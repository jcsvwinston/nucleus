package admin

import (
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observability"
)

// TestConsumeObservabilityFeedsLiveSQL pins the "bus becomes the single SQL
// feed" wiring: a SQL event emitted on the observability bus (as the framework
// default observer does for EVERY model.CRUD query across the app) lands in the
// live view's SQL buffer — not just the admin's own Data Studio queries.
func TestConsumeObservabilityFeedsLiveSQL(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	bus := observability.NewBus(nil)
	stop := panel.ConsumeObservability(bus)
	defer stop()

	if !panel.observConnected.Load() {
		t.Fatal("ConsumeObservability must mark the panel bus-connected")
	}

	ev := observability.AcquireSQLStatementEvent(time.Now().UTC(), "node-x")
	ev.ModelName = "Widget"
	ev.Operation = "SELECT"
	ev.Query = "SELECT * FROM widgets WHERE id = ?"
	ev.Args = []string{"7"}
	ev.RequestID = "req-9"
	bus.Emit(ev)

	// Delivery is asynchronous (the consumer goroutine drains the channel).
	var rows []liveSQLEvent
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rows = panel.live.sql.latest(10); len(rows) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 SQL event in the live buffer from the bus, got %d", len(rows))
	}
	if rows[0].ModelName != "Widget" || rows[0].Operation != "SELECT" {
		t.Fatalf("unexpected live SQL event: %+v", rows[0])
	}
	if rows[0].RequestID != "req-9" {
		t.Fatalf("request id not propagated from bus event: %+v", rows[0])
	}
}

// TestConsumeObservabilityNilBusIsNoop confirms the degrade path: a nil bus
// leaves the panel unconnected (so getCRUD keeps its per-CRUD observer fallback)
// and returns a no-op stop.
func TestConsumeObservabilityNilBusIsNoop(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	stop := panel.ConsumeObservability(nil)
	defer stop()

	if panel.observConnected.Load() {
		t.Fatal("ConsumeObservability(nil) must not mark the panel connected")
	}
}

// TestConsumeObservabilityStopIsIdempotent pins the leak/race fixes: stop can be
// called multiple times (and via Close) without panicking or double-closing,
// and the drain goroutine terminates (a permanent block would surface under the
// goroutine leak checker / -race over repeated runs).
func TestConsumeObservabilityStopIsIdempotent(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	bus := observability.NewBus(nil)
	stop := panel.ConsumeObservability(bus)

	stop()
	stop()                                           // second call must be a no-op (sync.Once)
	if err := panel.Close(t.Context()); err != nil { // Close also invokes the stop
		t.Fatalf("Close after stop: %v", err)
	}
}
