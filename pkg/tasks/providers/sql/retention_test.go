// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"testing"
	"time"
)

// Without retention the table only grows, in a system that is working
// perfectly: a queue doing a million jobs a day keeps a million rows a day,
// and the index the claim depends on gets slower every week.
func TestRetention_PurgesFinishedJobs(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Three finished jobs: two old, one recent.
	for i, finished := range []time.Time{now.Add(-48 * time.Hour), now.Add(-47 * time.Hour), now.Add(-time.Minute)} {
		id := "done-" + string(rune('a'+i))
		if err := store.Enqueue(ctx, Job{
			ID: id, Queue: "default", TaskType: "work", Payload: []byte(`{}`),
			MaxAttempts: 1, AvailableAt: now, CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(store.rebind(
			`UPDATE `+store.quotedTable()+` SET status = ?, finished_at = ? WHERE id = ?`),
			string(StatusDone), finished, id); err != nil {
			t.Fatal(err)
		}
	}
	// And one dead job, as old as they come.
	if err := store.Enqueue(ctx, Job{
		ID: "dead-1", Queue: "default", TaskType: "work", Payload: []byte(`{}`),
		MaxAttempts: 1, AvailableAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(store.rebind(
		`UPDATE `+store.quotedTable()+` SET status = ?, finished_at = ? WHERE id = ?`),
		string(StatusDead), now.Add(-72*time.Hour), "dead-1"); err != nil {
		t.Fatal(err)
	}

	purged, err := store.PurgeFinished(ctx, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if purged != 2 {
		t.Fatalf("purged %d, want the two finished jobs older than the retention", purged)
	}

	snap := NewInspector(store).InspectRuntime()
	if snap.TotalCompleted != 1 {
		t.Errorf("completed=%d, want the recent one kept", snap.TotalCompleted)
	}
	// The dead one survives whatever its age: it is the one somebody may still
	// want to requeue, and purge-archived is the deliberate act that removes it.
	if snap.TotalArchived != 1 {
		t.Errorf("archived=%d, want the dead job kept — retention must not be a back door that deletes it", snap.TotalArchived)
	}
}

// Retention off means off: an application that audits its own queue keeps
// everything.
func TestRetention_NegativeKeepsEverything(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := store.Enqueue(ctx, Job{
		ID: "old-1", Queue: "default", TaskType: "work", Payload: []byte(`{}`),
		MaxAttempts: 1, AvailableAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(store.rebind(
		`UPDATE `+store.quotedTable()+` SET status = ?, finished_at = ? WHERE id = ?`),
		string(StatusDone), now.Add(-100*time.Hour), "old-1"); err != nil {
		t.Fatal(err)
	}
	if purged, err := store.PurgeFinished(ctx, -1, now); err != nil || purged != 0 {
		t.Fatalf("purged=%d err=%v, want nothing removed", purged, err)
	}
}
