package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestStoreEnqueueAndSnapshot(t *testing.T) {
	db := openOutboxTestDB(t)
	store, err := NewStore(db, Config{Flavor: FlavorSQLite})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	msg, err := store.Enqueue(context.Background(), Entry{
		Topic:   "billing.invoice.created",
		Payload: map[string]any{"invoice_id": "inv_123"},
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if msg.Status != StatusPending {
		t.Fatalf("expected pending status, got %q", msg.Status)
	}

	snapshot := store.Snapshot(context.Background())
	if !snapshot.Enabled {
		t.Fatalf("expected enabled snapshot, got reason %q", snapshot.Reason)
	}
	if snapshot.Pending != 1 || snapshot.Total != 1 {
		t.Fatalf("unexpected snapshot counts: %#v", snapshot)
	}
}

func TestStoreEnqueueTxRollback(t *testing.T) {
	db := openOutboxTestDB(t)
	store, err := NewStore(db, Config{Flavor: FlavorSQLite})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := store.EnqueueTx(context.Background(), tx, Entry{
		Topic:   "billing.invoice.created",
		Payload: map[string]any{"invoice_id": "inv_rollback"},
	}); err != nil {
		t.Fatalf("enqueue tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	snapshot := store.Snapshot(context.Background())
	if snapshot.Total != 0 {
		t.Fatalf("expected rolled back entry to disappear, got %#v", snapshot)
	}
}

func TestDispatcherRunOnceDeliversAndMarksDelivered(t *testing.T) {
	db := openOutboxTestDB(t)
	store, err := NewStore(db, Config{Flavor: FlavorSQLite})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.Enqueue(context.Background(), Entry{
		Topic:   "emails.send",
		Payload: map[string]any{"to": "dev@example.com"},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	delivered := make([]string, 0, 1)
	dispatcher, err := NewDispatcher(store, func(ctx context.Context, msg Message) error {
		delivered = append(delivered, msg.Topic)
		return nil
	}, DispatcherConfig{
		LeaseOwner: "test-node",
		BatchSize:  4,
	})
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	result, err := dispatcher.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if result.Attempted != 1 || result.Delivered != 1 || result.Retried != 0 || result.Failed != 0 {
		t.Fatalf("unexpected dispatch result: %#v", result)
	}
	if len(delivered) != 1 || delivered[0] != "emails.send" {
		t.Fatalf("unexpected delivered topics: %#v", delivered)
	}

	snapshot := store.Snapshot(context.Background())
	if snapshot.Delivered != 1 || snapshot.Pending != 0 || snapshot.Processing != 0 {
		t.Fatalf("unexpected snapshot after delivery: %#v", snapshot)
	}
}

func TestDispatcherRetriesThenFails(t *testing.T) {
	db := openOutboxTestDB(t)
	store, err := NewStore(db, Config{Flavor: FlavorSQLite})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.Enqueue(context.Background(), Entry{
		Topic:   "emails.send",
		Payload: map[string]any{"to": "dev@example.com"},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	dispatcher, err := NewDispatcher(store, func(ctx context.Context, msg Message) error {
		return errors.New("smtp unavailable")
	}, DispatcherConfig{
		LeaseOwner:  "test-node",
		BatchSize:   1,
		MaxAttempts: 2,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	first, err := dispatcher.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if first.Attempted != 1 || first.Retried != 1 || first.Failed != 0 {
		t.Fatalf("unexpected first result: %#v", first)
	}

	time.Sleep(3 * time.Millisecond)

	second, err := dispatcher.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second.Attempted != 1 || second.Failed != 1 || second.Retried != 0 {
		t.Fatalf("unexpected second result: %#v", second)
	}

	snapshot := store.Snapshot(context.Background())
	if snapshot.Failed != 1 || snapshot.Pending != 0 || snapshot.Processing != 0 {
		t.Fatalf("unexpected snapshot after failure: %#v", snapshot)
	}
}

func openOutboxTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbName := fmt.Sprintf("file:outbox_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dbName)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}
