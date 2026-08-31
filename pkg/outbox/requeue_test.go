package outbox

import (
	"context"
	"testing"
)

// NF-8: a message that exhausted its retries stayed "failed" forever — no
// requeue path short of hand-written SQL. RequeueFailed is that path.
func TestRequeueFailedReturnsMessagesToPending(t *testing.T) {
	db := openOutboxTestDB(t)
	store, err := NewStore(db, Config{Flavor: FlavorSQLite})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := context.Background()

	var ids []string
	for i := 0; i < 3; i++ {
		msg, err := store.Enqueue(ctx, Entry{Topic: "t", Payload: map[string]any{"i": i}})
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		ids = append(ids, msg.ID)
	}
	// Fail two of them the way the dispatcher does (status + error + attempts).
	for _, id := range ids[:2] {
		if _, err := db.ExecContext(ctx,
			`UPDATE nucleus_outbox SET status = 'failed', attempts = 5, last_error = 'boom' WHERE id = ?`, id); err != nil {
			t.Fatalf("mark failed: %v", err)
		}
	}

	// Requeue ONE by id: the other failed message and the pending one stay put.
	n, err := store.RequeueFailed(ctx, ids[0])
	if err != nil {
		t.Fatalf("RequeueFailed(one): %v", err)
	}
	if n != 1 {
		t.Fatalf("RequeueFailed(one) = %d; want 1", n)
	}
	snap := store.Snapshot(ctx)
	if snap.Failed != 1 || snap.Pending != 2 {
		t.Fatalf("after one requeue: failed=%d pending=%d; want 1/2", snap.Failed, snap.Pending)
	}

	// The requeued message got its retry budget back and kept last_error.
	var attempts int
	var lastError string
	if err := db.QueryRowContext(ctx,
		`SELECT attempts, COALESCE(last_error, '') FROM nucleus_outbox WHERE id = ?`, ids[0]).Scan(&attempts, &lastError); err != nil {
		t.Fatalf("inspect requeued: %v", err)
	}
	if attempts != 0 {
		t.Fatalf("requeued attempts = %d; want 0", attempts)
	}
	if lastError != "boom" {
		t.Fatalf("requeued last_error = %q; want preserved 'boom'", lastError)
	}

	// Requeue ALL: only the remaining failed one moves.
	n, err = store.RequeueFailed(ctx)
	if err != nil {
		t.Fatalf("RequeueFailed(all): %v", err)
	}
	if n != 1 {
		t.Fatalf("RequeueFailed(all) = %d; want 1", n)
	}
	snap = store.Snapshot(ctx)
	if snap.Failed != 0 || snap.Pending != 3 {
		t.Fatalf("after full requeue: failed=%d pending=%d; want 0/3", snap.Failed, snap.Pending)
	}
}

// An id that is not failed must not be touched — a typo'd id cannot re-run a
// delivered message.
func TestRequeueFailedIgnoresNonFailedIDs(t *testing.T) {
	db := openOutboxTestDB(t)
	store, err := NewStore(db, Config{Flavor: FlavorSQLite})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := context.Background()
	msg, err := store.Enqueue(ctx, Entry{Topic: "t", Payload: map[string]any{}})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE nucleus_outbox SET status = 'delivered' WHERE id = ?`, msg.ID); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	n, err := store.RequeueFailed(ctx, msg.ID)
	if err != nil {
		t.Fatalf("RequeueFailed: %v", err)
	}
	if n != 0 {
		t.Fatalf("RequeueFailed on delivered id = %d; want 0", n)
	}
}
