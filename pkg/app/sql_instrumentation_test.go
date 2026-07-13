package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// sqlSnapshot is a detached copy of the fields an event carries — the bus
// event is pooled and Released immediately, so tests must not retain it.
type sqlSnapshot struct {
	Operation string
	Query     string
}

// drainSQL subscribes to the app's observability bus for SQL events and
// returns a receive function that waits up to d for the next event, handing
// back a detached copy (the pooled event is Released before returning).
func drainSQL(t *testing.T, a *App) (next func(d time.Duration) *sqlSnapshot, cancel func()) {
	t.Helper()
	sub, cancelSub := a.Observability.Subscribe(
		observability.Filter{Kinds: []observability.EventKind{observability.KindSQLStatement}},
		nil,
	)
	next = func(d time.Duration) *sqlSnapshot {
		select {
		case ev := <-sub.Ch():
			sqlEv, _ := ev.(*observability.SQLStatementEvent)
			if sqlEv == nil {
				ev.Release()
				return nil
			}
			snap := &sqlSnapshot{Operation: sqlEv.Operation, Query: sqlEv.Query}
			sqlEv.Release()
			return snap
		case <-time.After(d):
			return nil
		}
	}
	return next, cancelSub
}

// TestSQLDriverInstrumentation_DirectQueryReachesBus proves the opt-in wiring:
// with sql_driver_instrumentation on, a direct db.ExecContext (bypassing
// model.CRUD) surfaces on the observability bus, and a statement marked as
// already model-observed does not (de-dup).
func TestSQLDriverInstrumentation_DirectQueryReachesBus(t *testing.T) {
	cfg := testAppConfig()
	cfg.SQLDriverInstrumentation = true

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	defer a.Shutdown(context.Background())

	next, cancel := drainSQL(t, a)
	defer cancel()

	sqlDB, err := a.DB.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}

	// A direct exec — the kind of statement model.CRUD never sees — must
	// reach the bus. Drain the window and look for our unique statement
	// (other setup statements may share the feed).
	if _, err := sqlDB.ExecContext(context.Background(), "CREATE TABLE direct_marker_tbl (id INTEGER)"); err != nil {
		t.Fatalf("direct exec: %v", err)
	}
	found := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ev := next(2 * time.Second)
		if ev == nil {
			break
		}
		if ev.Operation == "create" && strings.Contains(ev.Query, "direct_marker_tbl") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("direct CREATE statement did not reach the observability bus")
	}

	// A statement carrying the model-observed marker (what CRUD stamps) must
	// NOT be re-emitted by the driver layer.
	crudCtx := observe.CtxWithModelObserved(context.Background())
	if _, err := sqlDB.ExecContext(crudCtx, "INSERT INTO direct_marker_tbl (id) VALUES (777)"); err != nil {
		t.Fatalf("marked exec: %v", err)
	}
	deadline = time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		ev := next(400 * time.Millisecond)
		if ev == nil {
			break
		}
		if strings.Contains(ev.Query, "777") || (strings.Contains(ev.Query, "direct_marker_tbl") && ev.Operation == "insert") {
			t.Errorf("model-observed statement was re-emitted by the driver layer: %+v", ev)
		}
	}
}

// TestSQLDriverInstrumentation_OffByDefault proves the default posture: with
// the key unset, a direct statement does NOT reach the bus (only CRUD would).
func TestSQLDriverInstrumentation_OffByDefault(t *testing.T) {
	cfg := testAppConfig() // SQLDriverInstrumentation defaults to false

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	defer a.Shutdown(context.Background())

	next, cancel := drainSQL(t, a)
	defer cancel()

	sqlDB, err := a.DB.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}

	if _, err := sqlDB.ExecContext(context.Background(), "CREATE TABLE off_t (id INTEGER)"); err != nil {
		t.Fatalf("direct exec: %v", err)
	}
	if ev := next(300 * time.Millisecond); ev != nil {
		t.Errorf("direct statement reached the bus with instrumentation off: %+v", ev)
	}
}
