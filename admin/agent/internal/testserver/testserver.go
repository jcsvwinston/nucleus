// Package testserver provides an in-process fake admin server used in
// agent integration tests. It implements just enough of AgentService to
// observe what the agent sends, drive subscribe/unsubscribe scenarios,
// and exercise reconnect paths.
//
// The package is internal so it cannot be imported outside admin/agent;
// production code MUST NOT depend on it.
package testserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// Server is a fake admin server. Construct with Start, drop with Close.
// Use the methods on Server to observe registrations, drive subscribes,
// and watch incoming events.
type Server struct {
	httpSrv  *http.Server
	listener net.Listener
	url      string

	mu         sync.Mutex
	streams    []*StreamSession
	regCh      chan *adminv1.NodeRegistration
	heartbeats int
}

// StreamSession represents one bidi stream the agent has opened.
// Methods are goroutine-safe and may be called from the test goroutine.
type StreamSession struct {
	mu       sync.Mutex
	stream   *connect.BidiStream[adminv1.Frame, adminv1.Frame]
	closed   bool
	closedCh chan struct{}

	// observed
	registration *adminv1.NodeRegistration
	heartbeats   int
	events       []*adminv1.Event
	goodbye      string

	eventsCh chan *adminv1.Event
	regCh    chan *adminv1.NodeRegistration
}

// Start launches a new fake server on a fresh ephemeral port. The
// returned Server is ready to accept connections; agents should be
// pointed at Server.URL().
func Start() *Server {
	s := &Server{
		regCh: make(chan *adminv1.NodeRegistration, 16),
	}

	mux := http.NewServeMux()
	mux.Handle(adminv1connect.NewAgentServiceHandler(s))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// h2c so the agent (which uses an h2c-capable client) can talk to us
	// without TLS. The canonical recipe is net.Listen + http.Server with
	// h2c.NewHandler — httptest.NewServer's HTTP/1.1 surface does not
	// expose the underlying connection in a way that lets h2c upgrade.
	h2s := &http2.Server{}
	handler := h2c.NewHandler(mux, h2s)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Errorf("testserver: listen: %w", err))
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	s.httpSrv = srv
	s.listener = listener
	s.url = "http://" + listener.Addr().String()

	go func() {
		_ = srv.Serve(listener)
	}()

	return s
}

// Close shuts the server down. Safe to call from defer.
func (s *Server) Close() {
	if s == nil || s.httpSrv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.httpSrv.Shutdown(ctx)
}

// URL returns the URL agents should connect to (e.g. http://127.0.0.1:NNNN).
func (s *Server) URL() string { return s.url }

// Listener returns the bound TCP address (host:port). Useful for tests
// that want to break the connection by closing the listener manually.
func (s *Server) Addr() net.Addr {
	if s == nil || s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Sessions returns a snapshot of sessions opened against this server.
func (s *Server) Sessions() []*StreamSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*StreamSession, len(s.streams))
	copy(out, s.streams)
	return out
}

// WaitForRegistration blocks until any agent sends a NodeRegistration on
// any stream, or the deadline elapses.
func (s *Server) WaitForRegistration(d time.Duration) (*adminv1.NodeRegistration, error) {
	select {
	case reg := <-s.regCh:
		return reg, nil
	case <-time.After(d):
		return nil, errors.New("testserver: timeout waiting for registration")
	}
}

// Stream is the AgentService.Stream handler.
func (s *Server) Stream(ctx context.Context, stream *connect.BidiStream[adminv1.Frame, adminv1.Frame]) error {
	sess := &StreamSession{
		stream:   stream,
		closedCh: make(chan struct{}),
		eventsCh: make(chan *adminv1.Event, 64),
		regCh:    make(chan *adminv1.NodeRegistration, 1),
	}

	s.mu.Lock()
	s.streams = append(s.streams, sess)
	s.mu.Unlock()

	defer func() {
		sess.mu.Lock()
		if !sess.closed {
			sess.closed = true
			close(sess.closedCh)
		}
		sess.mu.Unlock()
	}()

	for {
		frame, err := stream.Receive()
		if err != nil {
			// Client closed or transport error.
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return nil
		}
		switch body := frame.GetBody().(type) {
		case *adminv1.Frame_Registration:
			sess.mu.Lock()
			sess.registration = body.Registration
			sess.mu.Unlock()
			select {
			case sess.regCh <- body.Registration:
			default:
			}
			select {
			case s.regCh <- body.Registration:
			default:
			}
		case *adminv1.Frame_Heartbeat:
			sess.mu.Lock()
			sess.heartbeats++
			sess.mu.Unlock()
			s.mu.Lock()
			s.heartbeats++
			s.mu.Unlock()
		case *adminv1.Frame_Event:
			sess.mu.Lock()
			sess.events = append(sess.events, body.Event)
			sess.mu.Unlock()
			select {
			case sess.eventsCh <- body.Event:
			default:
			}
		case *adminv1.Frame_Goodbye:
			sess.mu.Lock()
			sess.goodbye = body.Goodbye.GetReason()
			sess.mu.Unlock()
			return nil
		case *adminv1.Frame_SnapshotResponse:
			// Phase 4 wires this; the test server stores it for
			// inspection but otherwise ignores.
		}
	}
}

// SendSubscribe pushes a Subscribe command to the latest session.
func (sess *StreamSession) SendSubscribe(id string, filter *adminv1.Filter, rates map[string]float32) error {
	return sess.send(&adminv1.Frame{
		Body: &adminv1.Frame_Command{
			Command: &adminv1.Command{
				Body: &adminv1.Command_Subscribe{
					Subscribe: &adminv1.Subscribe{
						SubscriptionId: id,
						Filter:         filter,
						SamplingRate:   rates,
					},
				},
			},
		},
	})
}

// SendUnsubscribe pushes an Unsubscribe command.
func (sess *StreamSession) SendUnsubscribe(id string) error {
	return sess.send(&adminv1.Frame{
		Body: &adminv1.Frame_Command{
			Command: &adminv1.Command{
				Body: &adminv1.Command_Unsubscribe{
					Unsubscribe: &adminv1.Unsubscribe{
						SubscriptionId: id,
					},
				},
			},
		},
	})
}

// SendGoodbye pushes a server-initiated Goodbye and closes the session.
func (sess *StreamSession) SendGoodbye(reason string) error {
	return sess.send(&adminv1.Frame{
		Body: &adminv1.Frame_Goodbye{
			Goodbye: &adminv1.Goodbye{Reason: reason},
		},
	})
}

func (sess *StreamSession) send(frame *adminv1.Frame) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return errors.New("session closed")
	}
	return sess.stream.Send(frame)
}

// WaitForEvent blocks until the session receives an event from the agent
// or the timeout elapses.
func (sess *StreamSession) WaitForEvent(d time.Duration) (*adminv1.Event, error) {
	select {
	case ev := <-sess.eventsCh:
		return ev, nil
	case <-time.After(d):
		return nil, errors.New("testserver: timeout waiting for event")
	}
}

// EventsCh exposes the raw events channel. Tests use it for select-form
// checks like "no event arrives within X". Each item is owned by the
// caller; the testserver does not retain references after sending.
func (sess *StreamSession) EventsCh() <-chan *adminv1.Event {
	return sess.eventsCh
}

// WaitForRegistration blocks for this specific session.
func (sess *StreamSession) WaitForRegistration(d time.Duration) (*adminv1.NodeRegistration, error) {
	select {
	case reg := <-sess.regCh:
		return reg, nil
	case <-time.After(d):
		return nil, errors.New("testserver: timeout waiting for session registration")
	}
}

// EventCount returns the count of events received so far.
func (sess *StreamSession) EventCount() int {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return len(sess.events)
}

// HeartbeatCount returns the number of heartbeats received so far.
func (sess *StreamSession) HeartbeatCount() int {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.heartbeats
}

// Goodbye returns the reason string if the agent sent a Goodbye, or "".
func (sess *StreamSession) Goodbye() string {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.goodbye
}

// Closed returns a channel that is closed once the session has ended
// (either side closed the stream).
func (sess *StreamSession) Closed() <-chan struct{} {
	return sess.closedCh
}
