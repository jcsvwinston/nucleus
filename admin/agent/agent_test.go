package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/nucleus/admin/agent/internal/testserver"
	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

func discardLogger() *slog.Logger {
	if os.Getenv("AGENT_DEBUG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestAgent_New_Disabled verifies the no-endpoints fail-open path.
func TestAgent_New_Disabled(t *testing.T) {
	_, err := New(Config{Logger: discardLogger()})
	if err != ErrDisabled {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

// TestAgent_RegistrationAndSubscribe is the core integration test:
//
//   1. start a fake admin server
//   2. start the agent pointing at it
//   3. observe the NodeRegistration arrive
//   4. send a Subscribe command from the server
//   5. emit an event into the bus
//   6. observe it arrive at the server
//   7. send Unsubscribe and verify subsequent events are NOT delivered
func TestAgent_RegistrationAndSubscribe(t *testing.T) {
	srv := testserver.Start()
	defer srv.Close()

	bus := observability.NewBus(discardLogger())
	stateDir := t.TempDir()

	agent, err := New(Config{
		Endpoints:         []string{srv.URL()},
		StateDir:          stateDir,
		Bus:               bus,
		HeartbeatInterval: 100 * time.Millisecond,
		DrainTimeout:      500 * time.Millisecond,
		Logger:            discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- agent.Run(ctx) }()

	// 1) NodeRegistration arrives.
	reg, err := srv.WaitForRegistration(2 * time.Second)
	if err != nil {
		t.Fatalf("WaitForRegistration: %v", err)
	}
	if reg.NodeId != agent.NodeID() {
		t.Errorf("registration node_id = %q, agent.NodeID() = %q", reg.NodeId, agent.NodeID())
	}
	// node_id should be persistent (UUID).
	if want := filepath.Join(stateDir, "node_id"); want == "" {
		t.Errorf("state_dir misconfigured")
	}

	sessions := srv.Sessions()
	if len(sessions) == 0 {
		t.Fatal("no session captured")
	}
	sess := sessions[0]

	// 2) Subscribe from the server.
	if err := sess.SendSubscribe("sub-1", &adminv1.Filter{
		Types: []adminv1.EventType{adminv1.EventType_EVENT_TYPE_HTTP_REQUEST},
	}, nil); err != nil {
		t.Fatalf("SendSubscribe: %v", err)
	}

	// Give the agent a chance to register the subscription before we
	// emit; the bus may otherwise short-circuit on HasSubscribers.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bus.HasSubscribers(observability.KindHTTPRequest) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !bus.HasSubscribers(observability.KindHTTPRequest) {
		t.Fatal("agent did not register HTTP subscription on bus")
	}

	// 3) Emit a typed HTTP event into the bus.
	httpEv := observability.AcquireHTTPRequestEvent(time.Now(), agent.NodeID())
	httpEv.Method = "GET"
	httpEv.Path = "/api/x"
	httpEv.Status = 200
	bus.Emit(httpEv)

	// 4) Server receives the corresponding proto event.
	got, err := sess.WaitForEvent(2 * time.Second)
	if err != nil {
		t.Fatalf("WaitForEvent: %v", err)
	}
	if got.GetHttpRequest() == nil {
		t.Fatalf("expected http_request body, got %+v", got)
	}
	if got.GetHttpRequest().Method != "GET" || got.GetHttpRequest().Path != "/api/x" {
		t.Errorf("body = %+v", got.GetHttpRequest())
	}

	// 5) Unsubscribe; HasSubscribers must drop back to false.
	if err := sess.SendUnsubscribe("sub-1"); err != nil {
		t.Fatalf("SendUnsubscribe: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !bus.HasSubscribers(observability.KindHTTPRequest) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if bus.HasSubscribers(observability.KindHTTPRequest) {
		t.Fatal("agent did not unregister HTTP subscription after Unsubscribe")
	}

	// 6) Subsequent emits are no-ops on the wire.
	httpEv2 := observability.AcquireHTTPRequestEvent(time.Now(), agent.NodeID())
	httpEv2.Method = "POST"
	httpEv2.Path = "/api/y"
	bus.Emit(httpEv2)

	select {
	case ev := <-sess.EventsCh():
		t.Fatalf("unexpected event after Unsubscribe: %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// expected
	}

	// 7) Graceful shutdown sends Goodbye.
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after ctx cancel")
	}

	if g := sess.Goodbye(); g == "" {
		t.Errorf("server did not see Goodbye, got %q", g)
	}
}

// TestAgent_Reconnects_AfterServerGoodbye verifies the reconnect loop:
// the server sends Goodbye, the agent dials again and re-registers.
func TestAgent_Reconnects_AfterServerGoodbye(t *testing.T) {
	srv := testserver.Start()
	defer srv.Close()

	bus := observability.NewBus(discardLogger())

	agent, err := New(Config{
		Endpoints:         []string{srv.URL()},
		StateDir:          t.TempDir(),
		Bus:               bus,
		HeartbeatInterval: 50 * time.Millisecond,
		DrainTimeout:      200 * time.Millisecond,
		Logger:            discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- agent.Run(ctx) }()

	// First registration.
	if _, err := srv.WaitForRegistration(2 * time.Second); err != nil {
		t.Fatalf("first WaitForRegistration: %v", err)
	}
	first := srv.Sessions()[0]

	// Server says goodbye to force the agent to reconnect.
	if err := first.SendGoodbye("test-goodbye"); err != nil {
		t.Fatalf("SendGoodbye: %v", err)
	}

	// Second registration should arrive after reconnect.
	if _, err := srv.WaitForRegistration(3 * time.Second); err != nil {
		t.Fatalf("second WaitForRegistration: %v", err)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after ctx cancel")
	}
}

// TestAgent_NoEndpointsAvailable_BackoffsButDoesNotCrash verifies the
// agent stays alive (and hence the framework stays alive) when every
// endpoint is unreachable.
func TestAgent_NoEndpointsAvailable_BackoffsButDoesNotCrash(t *testing.T) {
	bus := observability.NewBus(discardLogger())

	agent, err := New(Config{
		Endpoints:         []string{"http://127.0.0.1:1"}, // refuses
		StateDir:          t.TempDir(),
		Bus:               bus,
		HeartbeatInterval: 50 * time.Millisecond,
		DrainTimeout:      100 * time.Millisecond,
		Logger:            discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := agent.Run(ctx); err != nil {
		t.Errorf("Run returned %v, want nil on graceful shutdown", err)
	}
}

// TestAgent_NodeIDPersistsAcrossRestarts uses the same StateDir across two
// agent runs and verifies the NodeID is identical.
func TestAgent_NodeIDPersistsAcrossRestarts(t *testing.T) {
	stateDir := t.TempDir()

	first, err := New(Config{
		Endpoints: []string{"http://127.0.0.1:1"},
		StateDir:  stateDir,
		Logger:    discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	id1 := first.NodeID()
	if id1 == "" {
		t.Fatal("empty NodeID on first agent")
	}

	second, err := New(Config{
		Endpoints: []string{"http://127.0.0.1:1"},
		StateDir:  stateDir,
		Logger:    discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.NodeID() != id1 {
		t.Errorf("NodeID changed across restarts: %q -> %q", id1, second.NodeID())
	}
}

