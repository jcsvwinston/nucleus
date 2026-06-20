package nucleus

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"
)

func quietBus() *observability.Bus {
	return observability.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// toSQLEvent must copy Args, not alias the bus event's backing array (which the
// bus reuses on Release).
func TestToSQLEvent_CopiesArgsAndFields(t *testing.T) {
	src := &observability.SQLStatementEvent{
		ModelName: "Widget",
		Operation: "read",
		Query:     "SELECT 1",
		Args:      []string{"a", "b"},
		Duration:  5 * time.Millisecond,
	}
	got, ok := toSQLEvent(src)
	if !ok {
		t.Fatal("toSQLEvent returned ok=false for a *SQLStatementEvent")
	}
	if got.Query != "SELECT 1" || got.Operation != "read" || got.ModelName != "Widget" {
		t.Errorf("field copy wrong: %+v", got)
	}
	if len(got.Args) != 2 || got.Args[0] != "a" {
		t.Fatalf("Args copy wrong: %v", got.Args)
	}
	// Independence: mutating the source must not affect the copy.
	src.Args[0] = "MUTATED"
	if got.Args[0] != "a" {
		t.Error("SQLEvent.Args aliases the source backing array; must be a copy")
	}
}

func TestToSQLEvent_WrongType(t *testing.T) {
	if _, ok := toSQLEvent(&observability.HTTPRequestEvent{}); ok {
		t.Error("toSQLEvent should return ok=false for a non-SQL event")
	}
}

func TestToHTTPEvent_WrongType(t *testing.T) {
	if _, ok := toHTTPEvent(&observability.SQLStatementEvent{}); ok {
		t.Error("toHTTPEvent should return ok=false for a non-HTTP event")
	}
}

// End-to-end through a real bus: emit a SQL event, receive the translated value.
func TestEventBus_SubscribeSQL_Integration(t *testing.T) {
	a := busAdapter{bus: quietBus()}
	ch, cancel := a.SubscribeSQL()
	t.Cleanup(cancel)

	e := observability.AcquireSQLStatementEvent(time.Now(), "node-1")
	e.Query = "SELECT 42"
	e.Operation = "read"
	e.Args = []string{"x"}
	a.bus.Emit(e)

	select {
	case got := <-ch:
		if got.Query != "SELECT 42" {
			t.Errorf("Query = %q, want SELECT 42", got.Query)
		}
		if got.NodeID != "node-1" {
			t.Errorf("NodeID = %q, want node-1", got.NodeID)
		}
		if len(got.Args) != 1 || got.Args[0] != "x" {
			t.Errorf("Args = %v, want [x]", got.Args)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the translated SQL event")
	}
}

func TestEventBus_SubscribeHTTP_Integration(t *testing.T) {
	a := busAdapter{bus: quietBus()}
	ch, cancel := a.SubscribeHTTP()
	t.Cleanup(cancel)

	e := observability.AcquireHTTPRequestEvent(time.Now(), "node-1")
	e.Method = "GET"
	e.Path = "/widgets"
	e.Status = 200
	a.bus.Emit(e)

	select {
	case got := <-ch:
		if got.Method != "GET" || got.Path != "/widgets" || got.Status != 200 {
			t.Errorf("HTTP event translated wrong: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the translated HTTP event")
	}
}

// cancel unsubscribes, stops the pump, and closes the output channel; calling it
// twice must not panic.
func TestEventBus_CancelClosesChannelIdempotent(t *testing.T) {
	a := busAdapter{bus: quietBus()}
	ch, cancel := a.SubscribeSQL()

	cancel()
	cancel() // idempotent — must not panic or double-close

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed after cancel — success
			}
			// a buffered event may arrive before the close; keep reading
		case <-deadline:
			t.Fatal("output channel was not closed after cancel")
		}
	}
}

func TestRuntimeObservability(t *testing.T) {
	core := newTestApp(t)
	if newRuntime(core, "").Observability() == nil {
		t.Fatal("Runtime.Observability() returned nil for a backed app (bus is always non-nil after app.New)")
	}
	if (runtime{}).Observability() != nil {
		t.Fatal("Runtime.Observability() on an unbacked runtime should be nil")
	}
}
