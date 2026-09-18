// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

// NU-84: a message claimed by a process that never came back has to be
// deliverable again once its lease expires. It used to be unreachable for
// good — the claim asked for `pending` and nothing moved a row back — so a
// replica dying between the claim and the delivery stranded its messages.
func TestDispatcher_ReclaimsAnAbandonedMessage(t *testing.T) {
	db := openOutboxTestDB(t)
	cfg := Config{TableName: "recovery_outbox"}
	store, err := NewStore(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := store.Enqueue(context.Background(), Entry{Topic: "t", Payload: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}

	// The state a dead owner leaves behind: claimed, lease long expired.
	expired := time.Now().UTC().Add(-time.Hour)
	if _, err := db.Exec(
		`UPDATE recovery_outbox SET status = ?, lease_owner = ?, lease_until = ?, attempts = 1 WHERE id = ?`,
		string(StatusProcessing), "the-dead-replica", expired, msg.ID); err != nil {
		t.Fatal(err)
	}

	delivered := 0
	dcfg := DefaultDispatcherConfig()
	dcfg.LeaseOwner = "the-live-replica"
	d, err := NewDispatcher(store, func(context.Context, Message) error {
		delivered++
		return nil
	}, dcfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Delivered != 1 || delivered != 1 {
		t.Fatalf("delivered=%d (handler ran %d times), want the abandoned message to be delivered", res.Delivered, delivered)
	}
	snap := InspectRuntime(db, cfg)
	if snap.Delivered != 1 || snap.Processing != 0 {
		t.Fatalf("snapshot delivered=%d processing=%d, want the message to have left processing", snap.Delivered, snap.Processing)
	}
}

// A live lease is not stolen: only an EXPIRED one makes a message claimable
// again, or two replicas would deliver the same message at once.
func TestDispatcher_DoesNotStealALiveLease(t *testing.T) {
	db := openOutboxTestDB(t)
	cfg := Config{TableName: "live_lease_outbox"}
	store, err := NewStore(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := store.Enqueue(context.Background(), Entry{Topic: "t", Payload: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`UPDATE live_lease_outbox SET status = ?, lease_owner = ?, lease_until = ?, attempts = 1 WHERE id = ?`,
		string(StatusProcessing), "the-busy-replica", time.Now().UTC().Add(time.Hour), msg.ID); err != nil {
		t.Fatal(err)
	}

	d, err := NewDispatcher(store, func(context.Context, Message) error {
		t.Error("a message under a live lease was delivered by a second dispatcher")
		return nil
	}, DefaultDispatcherConfig())
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Attempted != 0 {
		t.Fatalf("attempted=%d, want the live lease left alone", res.Attempted)
	}
}

// The rescue has to be bounded. A message that takes its process down every
// time it is picked up would otherwise be reclaimed for ever, spending an
// attempt per rescue with nothing to fail it: the delivery never returns, so
// no code path reaches the MaxAttempts check.
func TestDispatcher_ReapsAnAbandonedMessageThatIsOutOfAttempts(t *testing.T) {
	db := openOutboxTestDB(t)
	cfg := Config{TableName: "reaped_outbox"}
	store, err := NewStore(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := store.Enqueue(context.Background(), Entry{Topic: "t", Payload: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	dcfg := DefaultDispatcherConfig()
	if _, err := db.Exec(
		`UPDATE reaped_outbox SET status = ?, lease_owner = ?, lease_until = ?, attempts = ? WHERE id = ?`,
		string(StatusProcessing), "the-poisoned-replica", time.Now().UTC().Add(-time.Hour),
		dcfg.MaxAttempts, msg.ID); err != nil {
		t.Fatal(err)
	}

	d, err := NewDispatcher(store, func(context.Context, Message) error {
		t.Error("a message that had spent its attempts was picked up again")
		return errors.New("unreachable")
	}, dcfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed != 1 {
		t.Fatalf("failed=%d, want the exhausted message retired", res.Failed)
	}
	snap := InspectRuntime(db, cfg)
	if snap.Failed != 1 || snap.Processing != 0 {
		t.Fatalf("snapshot failed=%d processing=%d, want it out of processing and marked failed", snap.Failed, snap.Processing)
	}

	// And it stays retired: a second pass does not resurrect it.
	res2, err := d.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res2.Attempted != 0 || res2.Failed != 0 {
		t.Fatalf("second pass attempted=%d failed=%d, want nothing left to do", res2.Attempted, res2.Failed)
	}
}

// A rescued message keeps its attempt history: the rescue spends one, so a
// message that keeps killing its owner converges on the dead letter instead of
// cycling for ever.
func TestDispatcher_RescueSpendsAnAttempt(t *testing.T) {
	db := openOutboxTestDB(t)
	cfg := Config{TableName: "attempts_outbox"}
	store, err := NewStore(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := store.Enqueue(context.Background(), Entry{Topic: "t", Payload: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`UPDATE attempts_outbox SET status = ?, lease_until = ?, attempts = 2 WHERE id = ?`,
		string(StatusProcessing), time.Now().UTC().Add(-time.Hour), msg.ID); err != nil {
		t.Fatal(err)
	}

	var seen Message
	d, err := NewDispatcher(store, func(_ context.Context, m Message) error {
		seen = m
		return nil
	}, DefaultDispatcherConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen.Attempts != 3 {
		t.Fatalf("attempts=%d on the rescued message, want 3 — the rescue has to cost one", seen.Attempts)
	}
}
