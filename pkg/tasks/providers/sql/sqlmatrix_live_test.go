// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	nucleusdb "github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// TestSQLMatrix_JobsProvider opens the durable queue against a real engine and
// drives a full job life through it.
//
// It exists because of NU-85: the outbox shipped a CREATE INDEX IF NOT EXISTS
// that MySQL does not have, from inside its constructor, so an application
// with the outbox enabled could not open a MySQL database at all — and nothing
// caught it, because its tests were SQLite and no lane ran the package against
// a real engine. A queue whose DDL, claim and timestamp handling had only ever
// run on SQLite would be the same defect with a different name.
func TestSQLMatrix_JobsProvider(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping the live jobs provider test")
	}
	lower := strings.ToLower(rawURL)
	var flavor Flavor
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		flavor = FlavorPostgres
	case strings.HasPrefix(lower, "mysql://"):
		flavor = FlavorMySQL
	default:
		t.Skipf("NUCLEUS_SQL_MATRIX_URL=%q is not a required SQL matrix profile", rawURL)
	}

	db := openMatrixDB(t, rawURL)
	table := fmt.Sprintf("jobs_live_%d", time.Now().UnixNano())

	// The DDL, indexes included: the call that would have failed on MySQL.
	store, err := NewStore(db, Config{TableName: table, Flavor: flavor})
	if err != nil {
		t.Fatalf("open the queue against %s: %v", flavor, err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + table) })

	// Opening twice is what a second replica does.
	if _, err := NewStore(db, Config{TableName: table, Flavor: flavor}); err != nil {
		t.Fatalf("second open against %s: %v", flavor, err)
	}

	ctx := context.Background()
	m, err := NewManager(ManagerConfig{
		Store: store, Concurrency: 2, PollInterval: 50 * time.Millisecond,
		LeaseDuration: 2 * time.Second, ShutdownGrace: time.Second, Owner: "live-worker",
	}, quiet())
	if err != nil {
		t.Fatal(err)
	}
	ran := make(chan string, 4)
	if err := m.HandleFunc("live.work", func(_ context.Context, task tasks.Task) error {
		ran <- string(task.Payload())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	defer func() { stop(); _ = m.Close() }()
	go func() { _ = m.Run(runCtx) }()

	if _, err := m.EnqueueJSON("live.work", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("enqueue on %s: %v", flavor, err)
	}
	select {
	case <-ran:
	case <-time.After(20 * time.Second):
		t.Fatalf("on %s: the job never ran", flavor)
	}

	// The recovery path, which is what makes the queue durable: a job left
	// RUNNING by a dead owner, with an expired lease, has to be claimable
	// again. Timestamps are bound from Go in UTC — the engine's clock and time
	// zone never decide when a job is due.
	abandoned := fmt.Sprintf("abandoned-%d", time.Now().UnixNano())
	past := time.Now().UTC().Add(-time.Hour)
	if err := store.Enqueue(ctx, Job{
		ID: abandoned, Queue: "default", TaskType: "live.work", Payload: []byte(`{"k":"v"}`),
		MaxAttempts: 3, AvailableAt: past, CreatedAt: past,
	}); err != nil {
		t.Fatalf("enqueue the abandoned job on %s: %v", flavor, err)
	}
	if _, err := db.Exec(rebindFor(flavor, fmt.Sprintf(
		`UPDATE %s SET status = ?, lease_owner = ?, lease_until = ?, attempts = 1 WHERE id = ?`, table)),
		string(StatusRunning), "the-dead-replica", past, abandoned); err != nil {
		t.Fatalf("abandon the job on %s: %v", flavor, err)
	}
	select {
	case <-ran:
	case <-time.After(20 * time.Second):
		t.Fatalf("on %s: a job abandoned by a dead owner was never recovered", flavor)
	}

	// And the snapshot the panel reads answers from the table.
	//
	// The wait is not decoration: `ran` fires INSIDE the handler, and the row
	// is moved to done by the manager after the handler returns. Reading the
	// snapshot on the next line measures that gap, and on a fast engine it
	// reported one job of two — a green suite away from a red one by a few
	// milliseconds.
	var snap tasks.RuntimeSnapshot
	deadline := time.Now().Add(20 * time.Second)
	for {
		snap = NewInspector(store).InspectRuntime()
		if snap.Enabled && snap.TotalCompleted >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("on %s: snapshot enabled=%v completed=%d, want both jobs recorded done",
				flavor, snap.Enabled, snap.TotalCompleted)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func rebindFor(flavor Flavor, query string) string {
	if flavor != FlavorPostgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func openMatrixDB(t *testing.T, rawURL string) *sql.DB {
	t.Helper()
	database, err := nucleusdb.New(nucleusdb.Config{
		Engine:      nucleusdb.EngineSQL,
		DatabaseURL: rawURL,
	}, nil)
	if err != nil {
		t.Fatalf("open %q: %v", rawURL, err)
	}
	t.Cleanup(func() { _ = database.Close() })
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("sql handle: %v", err)
	}
	return sqlDB
}
