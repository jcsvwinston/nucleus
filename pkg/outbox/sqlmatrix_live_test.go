// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	nucleusdb "github.com/jcsvwinston/nucleus/pkg/db"
)

// TestSQLMatrix_Outbox opens the outbox against a real engine.
//
// It exists because the store declares three dialects and its unit tests
// exercise one, and the difference between them is not cosmetic: NU-85 was a
// CREATE INDEX IF NOT EXISTS — valid on SQLite and Postgres, a SYNTAX ERROR on
// MySQL — emitted from inside NewStore, so an application with the outbox
// enabled could not open a MySQL database at all. Nothing caught it because
// the outbox's tests are SQLite and this lane did not run pkg/outbox.
func TestSQLMatrix_Outbox(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping the live outbox test")
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
	table := fmt.Sprintf("outbox_live_%d", time.Now().UnixNano())
	cfg := Config{TableName: table, Flavor: flavor}

	// The schema, indexes included. This is the call that used to fail on
	// MySQL before the message ever got anywhere.
	store, err := NewStore(db, cfg)
	if err != nil {
		t.Fatalf("open the outbox against %s: %v", flavor, err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + table) })

	// Idempotent: opening twice is what a second replica does.
	if _, err := NewStore(db, cfg); err != nil {
		t.Fatalf("second open against %s: %v", flavor, err)
	}

	ctx := context.Background()
	msg, err := store.Enqueue(ctx, Entry{Topic: "live", Payload: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatalf("enqueue on %s: %v", flavor, err)
	}

	// A message abandoned by a dead owner: the engine has to agree that an
	// expired lease makes it claimable again (NU-84). Timestamps are bound
	// from Go in UTC, never taken from the engine's clock, which is the
	// convention the rest of the tree follows.
	if _, err := db.Exec(rebind(flavor, fmt.Sprintf(
		`UPDATE %s SET status = ?, lease_owner = ?, lease_until = ?, attempts = 1 WHERE id = ?`, table)),
		string(StatusProcessing), "dead-replica", time.Now().UTC().Add(-time.Hour), msg.ID); err != nil {
		t.Fatalf("abandon the message on %s: %v", flavor, err)
	}

	delivered := 0
	d, err := NewDispatcher(store, func(context.Context, Message) error {
		delivered++
		return nil
	}, DefaultDispatcherConfig())
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.RunOnce(ctx)
	if err != nil {
		t.Fatalf("dispatch pass on %s: %v", flavor, err)
	}
	if res.Delivered != 1 || delivered != 1 {
		t.Fatalf("on %s: delivered=%d (handler ran %d times), want the abandoned message recovered",
			flavor, res.Delivered, delivered)
	}

	snap := InspectRuntime(db, cfg)
	if snap.Delivered != 1 || snap.Processing != 0 {
		t.Fatalf("on %s: snapshot delivered=%d processing=%d", flavor, snap.Delivered, snap.Processing)
	}
}

// rebind turns `?` placeholders into the engine's own, for the statements this
// test writes by hand.
func rebind(flavor Flavor, query string) string {
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
