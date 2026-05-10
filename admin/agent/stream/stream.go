// Package stream owns the bidi stream lifecycle between the agent and an
// admin server: the registration handshake, heartbeats, command dispatch
// (Subscribe / Unsubscribe / SnapshotRequest / Goodbye), event egress
// from the in-process bus, and graceful drain on shutdown.
//
// Stream is intentionally an inner detail of the agent. The agent's top-
// level New/Run constructs streams in a loop; each Stream represents one
// bidi attempt. A stream's lifetime ends when:
//
//   - the server sends Goodbye, or
//   - the underlying transport closes, or
//   - the parent context is cancelled.
//
// On any of these the stream returns from Run with a typed error class.
// The agent inspects the error to decide whether to reconnect (with
// backoff) or exit.
package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/nucleus/admin/agent/buffer"
	"github.com/jcsvwinston/nucleus/admin/agent/convert"
	"github.com/jcsvwinston/nucleus/admin/agent/metrics"
	"github.com/jcsvwinston/nucleus/admin/agent/sampler"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// ErrServerGoodbye is returned by Run when the admin server initiated a
// graceful shutdown of this stream. The agent reconnects.
var ErrServerGoodbye = errors.New("admin agent: server sent Goodbye")

// ErrTransport indicates the underlying HTTP/2 stream broke. The agent
// reconnects with backoff.
var ErrTransport = errors.New("admin agent: transport closed")

// DataStudioDispatcher executes a Data Studio request and returns the
// matching response. Concrete implementation lives in
// admin/agent/datastudio.Handler; the interface lets stream.go stay
// independent of pkg/model imports (which would otherwise create a
// dependency loop with pkg/app).
type DataStudioDispatcher interface {
	Dispatch(ctx context.Context, req *adminv1.DataStudioRequest) *adminv1.DataStudioResponse
	RegisteredModels() []string
}

// Config bundles the dependencies a Stream needs.
type Config struct {
	NodeID       string
	Version      string
	Labels       map[string]string
	StartedAt    time.Time
	Bus          *observability.Bus
	Buffer       *buffer.PerKind
	Metrics      *metrics.Metrics
	Logger       *slog.Logger
	Heartbeat    time.Duration
	DrainTimeout time.Duration

	// DataStudio, when non-nil, enables the agent-side Data Studio
	// dispatcher. The stream forwards DataStudioRequest commands to it
	// and ships the resulting DataStudioResponse back over the bidi
	// stream. Models reported by RegisteredModels() are included in
	// NodeRegistration so the admin server can route requests to the
	// right agent.
	DataStudio DataStudioDispatcher
}

func (c Config) withDefaults() Config {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Heartbeat <= 0 {
		c.Heartbeat = 10 * time.Second
	}
	if c.DrainTimeout <= 0 {
		c.DrainTimeout = 2 * time.Second
	}
	if c.StartedAt.IsZero() {
		c.StartedAt = time.Now().UTC()
	}
	return c
}

// Stream owns one bidi connection. Construct via New, drive via Run.
type Stream struct {
	cfg    Config
	client adminv1connect.AgentServiceClient

	// activeSubs tracks subscriptions the admin server has opened against
	// this agent. Each entry corresponds to one Subscribe command and one
	// observability.Subscription.
	subsMu     sync.Mutex
	activeSubs map[string]*activeSub

	// stream-lifetime context, cancelled when Run returns.
	streamCtx    context.Context
	streamCancel context.CancelFunc

	bidiStream *connect.BidiStreamForClient[adminv1.Frame, adminv1.Frame]

	// frameSendCh serializes all frames sent to the server. Anything that
	// wants to send (events, heartbeats, snapshot responses, goodbye)
	// pushes through this channel; the lone send goroutine drains it.
	frameSendCh chan *adminv1.Frame

	closed atomic.Bool
}

type activeSub struct {
	id      string
	sub     *observability.Subscription
	sampler *sampler.Sampler
	cancel  func()
}

// New constructs a Stream wrapping the given AgentService client. Run
// must be called to drive it; the constructor performs no network IO.
func New(client adminv1connect.AgentServiceClient, cfg Config) *Stream {
	cfg = cfg.withDefaults()
	return &Stream{
		cfg:         cfg,
		client:      client,
		activeSubs:  make(map[string]*activeSub),
		frameSendCh: make(chan *adminv1.Frame, 64),
	}
}

// Run opens the bidi stream, sends NodeRegistration, and runs the
// receive/send/heartbeat loops. It returns when the stream is gone — see
// ErrServerGoodbye / ErrTransport — or when ctx is cancelled.
func (s *Stream) Run(ctx context.Context) error {
	s.streamCtx, s.streamCancel = context.WithCancel(ctx)
	defer s.streamCancel()

	// Open the bidi stream. The connect-go client returns a half-stream
	// pair we can read from and write to in separate goroutines.
	s.bidiStream = s.client.Stream(s.streamCtx)

	// Spawn the loops BEFORE writing anything to the stream. Connect-RPC
	// bidi streams require both directions to be actively used; if the
	// client writes a frame before the response side is being read, some
	// HTTP/2 implementations close the request side prematurely. Registering
	// via the channel + sendLoop guarantees both sides are live by the
	// time the registration frame is flushed.
	errCh := make(chan error, 3)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); errCh <- s.recvLoop() }()
	go func() { defer wg.Done(); errCh <- s.sendLoop() }()
	go func() { defer wg.Done(); errCh <- s.heartbeatLoop() }()

	// Register; the server uses this to populate its node registry
	// before any events arrive. The frame travels through frameSendCh
	// so it is serialized with subsequent events/heartbeats.
	s.queueFrame(s.buildRegistration())

	// Replay the pre-disconnect ring buffer immediately after.
	s.replayBuffer()
	s.cfg.Metrics.Connected.WithLabelValues(s.endpointLabel()).Set(1)

	// Wait for the first one to surface a result. Cancel the stream
	// context to bring the others down, then collect.
	first := <-errCh
	s.streamCancel()

	// Cancel any outstanding subscriptions so the bus stops fanning out
	// to dead channels.
	s.cancelAllSubscriptions()

	// Drain the remaining two errors so wg.Wait below doesn't deadlock.
	for i := 0; i < 2; i++ {
		<-errCh
	}
	wg.Wait()

	// Close the stream halves explicitly. errors here are best-effort.
	_ = s.bidiStream.CloseRequest()
	_ = s.bidiStream.CloseResponse()

	s.cfg.Metrics.Connected.WithLabelValues(s.endpointLabel()).Set(0)
	return first
}

func (s *Stream) endpointLabel() string {
	// Best-effort: we don't have a direct handle to the endpoint here.
	// The agent layer tracks it via the connection.Result; this label is
	// purely informational. Empty is fine.
	return ""
}

func (s *Stream) buildRegistration() *adminv1.Frame {
	var models []string
	if s.cfg.DataStudio != nil {
		models = s.cfg.DataStudio.RegisteredModels()
	}
	return &adminv1.Frame{
		Body: &adminv1.Frame_Registration{
			Registration: &adminv1.NodeRegistration{
				NodeId:           s.cfg.NodeID,
				Version:          s.cfg.Version,
				Labels:           cloneLabels(s.cfg.Labels),
				StartedAt:        timestamppb.New(s.cfg.StartedAt),
				RegisteredModels: models,
			},
		},
	}
}

func (s *Stream) replayBuffer() {
	if s.cfg.Buffer == nil {
		return
	}
	queued := s.cfg.Buffer.DrainAll()
	for _, ev := range queued {
		s.queueFrame(&adminv1.Frame{Body: &adminv1.Frame_Event{Event: ev}})
	}
}

// recvLoop reads server-to-agent frames and dispatches them. It returns
// when the stream is closed or ctx is cancelled.
func (s *Stream) recvLoop() error {
	for {
		frame, err := s.bidiStream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ErrServerGoodbye
			}
			if connect.CodeOf(err) == connect.CodeCanceled || s.streamCtx.Err() != nil {
				return s.streamCtx.Err()
			}
			s.cfg.Metrics.StreamErrorsTotal.WithLabelValues("recv").Inc()
			return fmt.Errorf("%w: %v", ErrTransport, err)
		}

		switch body := frame.GetBody().(type) {
		case *adminv1.Frame_Command:
			s.handleCommand(body.Command)
		case *adminv1.Frame_Goodbye:
			s.cfg.Logger.Info("admin server requested stream goodbye",
				"reason", body.Goodbye.GetReason())
			return ErrServerGoodbye
		default:
			// Frames the agent doesn't handle (Heartbeat from server,
			// Registration echoed, etc.) are tolerated for forward compat.
			s.cfg.Logger.Debug("admin agent ignoring unrecognized server frame",
				"frame", fmt.Sprintf("%T", body))
		}
	}
}

// sendLoop drains frameSendCh and writes to the stream serially. This is
// the only writer to bidiStream.Send.
func (s *Stream) sendLoop() error {
	for {
		select {
		case <-s.streamCtx.Done():
			return s.streamCtx.Err()
		case frame := <-s.frameSendCh:
			if frame == nil {
				continue
			}
			if err := s.bidiStream.Send(frame); err != nil {
				s.cfg.Metrics.StreamErrorsTotal.WithLabelValues("send").Inc()
				return fmt.Errorf("%w: %v", ErrTransport, err)
			}
		}
	}
}

// heartbeatLoop fires Heartbeats at cfg.Heartbeat. The first heartbeat
// goes out one full interval after stream open, NOT immediately, to avoid
// noise during reconnection bursts.
func (s *Stream) heartbeatLoop() error {
	timer := time.NewTimer(s.cfg.Heartbeat)
	defer timer.Stop()
	for {
		select {
		case <-s.streamCtx.Done():
			return s.streamCtx.Err()
		case <-timer.C:
			s.queueFrame(s.buildHeartbeat())
			s.cfg.Metrics.HeartbeatsSent.Inc()
			timer.Reset(s.cfg.Heartbeat)
		}
	}
}

func (s *Stream) buildHeartbeat() *adminv1.Frame {
	stats := func(k observability.EventKind) (uint64, uint64) {
		if s.cfg.Bus == nil {
			return 0, 0
		}
		x := s.cfg.Bus.Stats(k)
		return x.Emitted, x.Dropped
	}
	var emitted, dropped uint64
	for _, k := range []observability.EventKind{
		observability.KindHTTPRequest,
		observability.KindSQLStatement,
		observability.KindSessionChange,
		observability.KindCustom,
	} {
		e, d := stats(k)
		emitted += e
		dropped += d
	}

	s.subsMu.Lock()
	active := uint32(len(s.activeSubs))
	s.subsMu.Unlock()

	return &adminv1.Frame{
		Body: &adminv1.Frame_Heartbeat{
			Heartbeat: &adminv1.Heartbeat{
				Timestamp:           timestamppb.Now(),
				ActiveSubscriptions: active,
				EventsEmittedTotal:  emitted,
				EventsDroppedTotal:  dropped,
			},
		},
	}
}

// handleCommand dispatches an incoming Command from the admin server.
func (s *Stream) handleCommand(cmd *adminv1.Command) {
	if cmd == nil {
		return
	}
	switch body := cmd.GetBody().(type) {
	case *adminv1.Command_Subscribe:
		s.handleSubscribe(body.Subscribe)
	case *adminv1.Command_Unsubscribe:
		s.handleUnsubscribe(body.Unsubscribe)
	case *adminv1.Command_SnapshotRequest:
		// Snapshot routing is the admin server's job in Phase 4. The
		// agent has no snapshot providers wired in this phase; respond
		// with an empty error response so the server can surface a
		// "snapshot not available" diagnostic to the operator.
		s.queueFrame(&adminv1.Frame{
			Body: &adminv1.Frame_SnapshotResponse{
				SnapshotResponse: &adminv1.SnapshotResponse{
					RequestId: body.SnapshotRequest.GetRequestId(),
					Type:      body.SnapshotRequest.GetType(),
					Error:     "agent: snapshot providers not implemented",
				},
			},
		})
	case *adminv1.Command_Goodbye:
		// Server-initiated graceful close handled by recvLoop's Goodbye
		// frame as well; this branch covers the alternative shape.
		s.cfg.Logger.Info("admin server graceful goodbye via Command",
			"reason", body.Goodbye.GetReason())
	case *adminv1.Command_DataStudio:
		s.handleDataStudio(body.DataStudio)
	}
}

// handleDataStudio runs a Data Studio request on the agent and ships
// the response back over the bidi stream. Each request runs in its
// own goroutine so a slow handler does not block the recvLoop.
func (s *Stream) handleDataStudio(req *adminv1.DataStudioRequest) {
	if req == nil {
		return
	}
	if s.cfg.DataStudio == nil {
		s.queueFrame(&adminv1.Frame{
			Body: &adminv1.Frame_DataStudioResponse{
				DataStudioResponse: &adminv1.DataStudioResponse{
					RequestId: req.GetRequestId(),
					Error:     "admin agent: data studio is not enabled on this node",
				},
			},
		})
		return
	}
	go func() {
		resp := s.cfg.DataStudio.Dispatch(s.streamCtx, req)
		if resp == nil {
			resp = &adminv1.DataStudioResponse{
				RequestId: req.GetRequestId(),
				Error:     "admin agent: data studio dispatcher returned nil",
			}
		}
		s.queueFrame(&adminv1.Frame{
			Body: &adminv1.Frame_DataStudioResponse{DataStudioResponse: resp},
		})
	}()
}

func (s *Stream) handleSubscribe(in *adminv1.Subscribe) {
	if in == nil {
		return
	}
	id := in.GetSubscriptionId()
	if id == "" {
		s.cfg.Logger.Warn("admin server Subscribe missing subscription_id; ignoring")
		return
	}

	// Cancel a previous sub with the same id (server replays Subscribe to
	// update filters).
	s.cancelSubscription(id)

	wantFilter := convert.FilterFromProto(in.GetFilter())
	smp := sampler.New(in.GetFilter(), in.GetSamplingRate())

	if s.cfg.Bus == nil {
		return
	}
	sub, cancel := s.cfg.Bus.Subscribe(wantFilter, &observability.SubscribeOptions{ChannelSize: 256})

	a := &activeSub{
		id:      id,
		sub:     sub,
		sampler: smp,
		cancel:  cancel,
	}
	s.subsMu.Lock()
	s.activeSubs[id] = a
	count := len(s.activeSubs)
	s.subsMu.Unlock()
	s.cfg.Metrics.ActiveSubscriptions.Set(float64(count))

	go s.drainSubscription(a)
}

func (s *Stream) drainSubscription(a *activeSub) {
	if a == nil || a.sub == nil {
		return
	}
	for {
		select {
		case <-s.streamCtx.Done():
			return
		case ev, ok := <-a.sub.Ch():
			if !ok {
				return
			}
			s.processEvent(a, ev)
		}
	}
}

// processEvent takes one event from the bus, applies sampling+filter,
// converts to proto, and queues for send. Each event is Released exactly
// once regardless of outcome.
func (s *Stream) processEvent(a *activeSub, ev observability.Event) {
	defer ev.Release()
	if a == nil || ev == nil {
		return
	}

	kindLabel := ev.Kind().String()

	switch a.sampler.Decide(ev) {
	case sampler.DropSampled:
		s.cfg.Metrics.EventsDroppedTotal.WithLabelValues(kindLabel, "sampled").Inc()
		return
	case sampler.DropFiltered:
		s.cfg.Metrics.EventsDroppedTotal.WithLabelValues(kindLabel, "filtered").Inc()
		return
	}

	pb := convert.EventToProto(ev)
	if pb == nil {
		s.cfg.Metrics.EventsDroppedTotal.WithLabelValues(kindLabel, "unknown_kind").Inc()
		return
	}
	frame := &adminv1.Frame{Body: &adminv1.Frame_Event{Event: pb}}
	if !s.tryQueueFrame(frame) {
		// Send buffer full; route to the per-kind ring buffer for replay
		// on the next reconnect.
		if s.cfg.Buffer != nil {
			if buf := s.cfg.Buffer.For(ev.Kind()); buf != nil {
				if buf.Push(pb) {
					s.cfg.Metrics.EventsDroppedTotal.WithLabelValues(kindLabel, "buffer_full").Inc()
				}
			}
		}
		s.cfg.Metrics.EventsDroppedTotal.WithLabelValues(kindLabel, "send_full").Inc()
		return
	}
	s.cfg.Metrics.EventsEmittedTotal.WithLabelValues(kindLabel).Inc()
}

func (s *Stream) handleUnsubscribe(in *adminv1.Unsubscribe) {
	if in == nil {
		return
	}
	s.cancelSubscription(in.GetSubscriptionId())
}

func (s *Stream) cancelSubscription(id string) {
	if id == "" {
		return
	}
	s.subsMu.Lock()
	a, ok := s.activeSubs[id]
	if ok {
		delete(s.activeSubs, id)
	}
	count := len(s.activeSubs)
	s.subsMu.Unlock()

	if ok && a != nil {
		a.cancel()
	}
	s.cfg.Metrics.ActiveSubscriptions.Set(float64(count))
}

func (s *Stream) cancelAllSubscriptions() {
	s.subsMu.Lock()
	subs := s.activeSubs
	s.activeSubs = make(map[string]*activeSub)
	s.subsMu.Unlock()

	for _, a := range subs {
		if a == nil {
			continue
		}
		a.cancel()
	}
	s.cfg.Metrics.ActiveSubscriptions.Set(0)
}

func (s *Stream) queueFrame(f *adminv1.Frame) {
	if f == nil {
		return
	}
	select {
	case s.frameSendCh <- f:
	case <-s.streamCtx.Done():
	}
}

func (s *Stream) tryQueueFrame(f *adminv1.Frame) bool {
	if f == nil {
		return true
	}
	select {
	case s.frameSendCh <- f:
		return true
	default:
		return false
	}
}

// Goodbye queues a Goodbye frame and best-effort waits for it to flush.
// Used during graceful shutdown.
func (s *Stream) Goodbye(reason string) {
	s.queueFrame(&adminv1.Frame{
		Body: &adminv1.Frame_Goodbye{
			Goodbye: &adminv1.Goodbye{Reason: reason},
		},
	})
}

func cloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
