package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/nucleus/admin/agent/buffer"
	"github.com/jcsvwinston/nucleus/admin/agent/connection"
	"github.com/jcsvwinston/nucleus/admin/agent/identity"
	"github.com/jcsvwinston/nucleus/admin/agent/metrics"
	"github.com/jcsvwinston/nucleus/admin/agent/stream"
)

// Config bundles every dependency the agent needs. The framework's
// app.New constructs this from nucleus.yml plus the in-process bus and
// passes it to New.
type Config struct {
	// Endpoints is the ordered list of admin server URLs (admin.endpoints
	// in nucleus.yml). At least one is required for the agent to start;
	// passing an empty list returns ErrDisabled from New.
	Endpoints []string

	// Token is the shared bearer token sent on every Connect-RPC call.
	// May be empty if mTLS is in use (Phase 6).
	Token string

	// StateDir is the path under which node_id is persisted (the new
	// top-level state_dir key in nucleus.yml). Empty means "use ephemeral
	// NodeID with WARN", consistent with decision 15.
	StateDir string

	// NodeIDOverride pins the NodeID instead of resolving from StateDir.
	// Empty means resolve via identity.Resolver.
	NodeIDOverride string

	// Version, Labels, StartedAt are forwarded to NodeRegistration.
	Version   string
	Labels    map[string]string
	StartedAt time.Time

	// Bus is the in-process observability bus the agent subscribes to. If
	// nil, the agent runs but no events flow.
	Bus *observability.Bus

	// HeartbeatInterval is the cadence between Heartbeat frames sent to
	// the admin server. Default 10s.
	HeartbeatInterval time.Duration

	// DrainTimeout is the maximum time spent flushing the ring buffer to
	// the stream during graceful shutdown. Default 2s.
	DrainTimeout time.Duration

	// HTTPBufferSize / SQLBufferSize / SessionBufferSize / CustomBufferSize
	// configure the per-kind drop-oldest ring buffers used during
	// microcortes (default 256 each).
	HTTPBufferSize    int
	SQLBufferSize     int
	SessionBufferSize int
	CustomBufferSize  int

	// MetricsAddr, when non-empty, starts a /metrics + /healthz HTTP
	// server on this address.
	MetricsAddr string

	// Logger receives WARN/INFO/DEBUG diagnostics. Pass nil for
	// slog.Default.
	Logger *slog.Logger
}

func (c Config) withDefaults() Config {
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 10 * time.Second
	}
	if c.DrainTimeout <= 0 {
		c.DrainTimeout = 2 * time.Second
	}
	if c.HTTPBufferSize <= 0 {
		c.HTTPBufferSize = 256
	}
	if c.SQLBufferSize <= 0 {
		c.SQLBufferSize = 256
	}
	if c.SessionBufferSize <= 0 {
		c.SessionBufferSize = 64
	}
	if c.CustomBufferSize <= 0 {
		c.CustomBufferSize = 64
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.StartedAt.IsZero() {
		c.StartedAt = time.Now().UTC()
	}
	return c
}

// ErrDisabled is returned by New when no admin endpoints are configured.
// Callers should treat this as "the agent is disabled" rather than as an
// error; it lets fail-open wiring in pkg/app skip the agent without
// noise.
var ErrDisabled = errors.New("admin agent: no endpoints configured (disabled)")

// Agent is the long-lived top-level type. Its Run method blocks until
// ctx is cancelled and returns nil on graceful shutdown.
type Agent struct {
	cfg    Config
	nodeID string

	bufs    *buffer.PerKind
	metrics *metrics.Metrics
	dialer  *connection.Dialer

	// connectedOnce is closed the first time Run successfully establishes
	// a stream to any admin endpoint. Used by Extension wrappers that
	// implement --require-admin (fail boot if no admin reachable within
	// a timeout). Subsequent disconnects/reconnects do NOT re-open it.
	connectedOnce     chan struct{}
	connectedOnceOnce sync.Once

	mu     sync.Mutex
	closed bool
}

// New constructs an Agent. Returns ErrDisabled when no endpoints are
// configured.
func New(cfg Config) (*Agent, error) {
	cfg = cfg.withDefaults()
	if len(cfg.Endpoints) == 0 {
		return nil, ErrDisabled
	}

	nodeID := cfg.NodeIDOverride
	if nodeID == "" {
		resolver := identity.New(cfg.StateDir, cfg.Logger)
		resolved := resolver.Resolve()
		nodeID = resolved.NodeID
		cfg.Logger.Info("admin agent NodeID resolved",
			"node_id", nodeID,
			"persistent", resolved.Persistent,
			"source", resolved.Source)
	}

	bufs := buffer.NewPerKind(map[observability.EventKind]int{
		observability.KindHTTPRequest:   cfg.HTTPBufferSize,
		observability.KindSQLStatement:  cfg.SQLBufferSize,
		observability.KindSessionChange: cfg.SessionBufferSize,
		observability.KindCustom:        cfg.CustomBufferSize,
	})

	m := metrics.New()

	dialer := connection.NewDialer(connection.Config{
		Endpoints: cfg.Endpoints,
		Token:     cfg.Token,
		Logger:    cfg.Logger,
	})

	return &Agent{
		cfg:           cfg,
		nodeID:        nodeID,
		bufs:          bufs,
		metrics:       m,
		dialer:        dialer,
		connectedOnce: make(chan struct{}),
	}, nil
}

// NodeID returns the resolved node identifier.
func (a *Agent) NodeID() string {
	if a == nil {
		return ""
	}
	return a.nodeID
}

// Metrics returns the agent's Prometheus metrics. Useful when the host
// app wants to expose them through its own /metrics rather than via the
// agent's stand-alone metrics server.
func (a *Agent) Metrics() *metrics.Metrics {
	if a == nil {
		return nil
	}
	return a.metrics
}

// Connected returns a channel that is closed the first time Run
// establishes a stream to any admin endpoint. Subsequent
// disconnects/reconnects do NOT re-open the channel.
//
// It is the integration point for the --require-admin path: callers
// that need the framework to fail boot when no admin is reachable
// select on this channel against a timeout, and abort if the timeout
// fires first.
func (a *Agent) Connected() <-chan struct{} {
	if a == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return a.connectedOnce
}

// Run drives the agent until ctx is cancelled. It blocks. On reconnect
// errors it sleeps according to the dialer backoff and retries. Returns
// nil on graceful shutdown.
func (a *Agent) Run(ctx context.Context) error {
	if a == nil {
		return errors.New("admin agent: nil agent")
	}

	// Optional metrics server.
	metricsSrv := metrics.NewServer(a.cfg.MetricsAddr, a.metrics, a.cfg.Logger)
	metricsErr := make(chan error, 1)
	go func() { metricsErr <- metricsSrv.Run(ctx) }()

	// Periodic ring-buffer gauge update.
	gaugeStop := make(chan struct{})
	go a.updateBufferGauges(ctx, gaugeStop)

	for {
		if err := ctx.Err(); err != nil {
			close(gaugeStop)
			<-metricsErr
			return nil
		}
		if err := a.runOnce(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				close(gaugeStop)
				<-metricsErr
				return nil
			}
			a.cfg.Logger.Debug("admin agent stream cycle ended", "error", err.Error())
			a.metrics.ReconnectsTotal.Inc()
			// Backoff before the next reconnect attempt.
			sleep := a.dialer.Backoff()
			select {
			case <-ctx.Done():
				close(gaugeStop)
				<-metricsErr
				return nil
			case <-time.After(sleep):
			}
			continue
		}
		// runOnce returned nil → ctx cancelled.
		close(gaugeStop)
		<-metricsErr
		return nil
	}
}

// runOnce performs one connect → register → run-stream cycle.
func (a *Agent) runOnce(ctx context.Context) error {
	res, err := a.dialer.Dial(ctx)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	a.metrics.Connected.WithLabelValues(res.Endpoint).Set(1)
	defer a.metrics.Connected.WithLabelValues(res.Endpoint).Set(0)

	a.connectedOnceOnce.Do(func() { close(a.connectedOnce) })

	a.cfg.Logger.Info("admin agent connected",
		"endpoint", res.Endpoint, "node_id", a.nodeID)

	st := stream.New(res.Client, stream.Config{
		NodeID:       a.nodeID,
		Version:      a.cfg.Version,
		Labels:       a.cfg.Labels,
		StartedAt:    a.cfg.StartedAt,
		Bus:          a.cfg.Bus,
		Buffer:       a.bufs,
		Metrics:      a.metrics,
		Logger:       a.cfg.Logger,
		Heartbeat:    a.cfg.HeartbeatInterval,
		DrainTimeout: a.cfg.DrainTimeout,
	})

	// streamLifeCtx is intentionally NOT a child of ctx. It is cancelled
	// only after the agent has had a chance to flush a Goodbye frame and
	// drain the send buffer. Tying it directly to ctx would propagate the
	// parent cancel into the underlying HTTP/2 stream and abort any
	// in-flight Send (including the Goodbye itself).
	streamLifeCtx, streamLifeCancel := context.WithCancel(context.Background())
	defer streamLifeCancel()

	streamDone := make(chan error, 1)
	go func() { streamDone <- st.Run(streamLifeCtx) }()

	select {
	case <-ctx.Done():
		// Graceful shutdown:
		//   1. queue the Goodbye (sendLoop is still alive)
		//   2. wait DrainTimeout for the stream to flush cleanly
		//   3. force-cancel and wait once more so we never block forever.
		st.Goodbye("agent shutting down")

		select {
		case <-streamDone:
		case <-time.After(a.cfg.DrainTimeout):
			streamLifeCancel()
			select {
			case <-streamDone:
			case <-time.After(a.cfg.DrainTimeout):
				a.cfg.Logger.Warn("admin agent stream did not finish drain within deadline; abandoning",
					"drain_timeout", a.cfg.DrainTimeout)
			}
		}
		return ctx.Err()
	case err := <-streamDone:
		return err
	}
}

func (a *Agent) updateBufferGauges(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			snap := a.bufs.LenSnapshot()
			for kind, n := range snap {
				a.metrics.BufferSize.WithLabelValues(kind.String()).Set(float64(n))
			}
		}
	}
}
