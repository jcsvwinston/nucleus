// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// Widget is the model the data helpers are measured against: a string, a
// number, a time and a boolean beside the base fields.
type Widget struct {
	model.BaseModel
	Name      string    `db:"column:name" json:"name"`
	Size      int       `db:"column:size" json:"size"`
	Active    bool      `db:"column:active" json:"active"`
	ShippedAt time.Time `db:"column:shipped_at" json:"shipped_at"`
}

func widgetApp(t *testing.T, dbs map[string]app.DatabaseConfig) nucleus.App {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.JWTSecret = strings.Repeat("nucleustest-secret-", 3)
	cfg.Databases = dbs
	m := nucleus.Module[struct{}]{
		Name:   "widgets",
		Models: []any{&Widget{}},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/count", func(c *nucleus.Context) error {
				var n int
				if err := c.Context.Request.Context().Err(); err != nil {
					return err
				}
				return c.JSON(http.StatusOK, map[string]int{"count": n})
			})
		},
	}.Build()
	return nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{m.Name(): m},
		Options: []app.Option{app.WithOpenAuthz()},
	}
}

func countWidgets(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM widgets").Scan(&n); err != nil {
		t.Fatalf("count widgets: %v", err)
	}
	return n
}

func TestMakePersistsARecordWithDefaults(t *testing.T) {
	srv := StartApp(t, widgetApp(t, TempSQLite(t)))
	if err := srv.Runtime().AutoMigrate(&Widget{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	w := Make[Widget](srv)
	if w.ID == 0 {
		t.Fatal("Make did not fill the primary key")
	}
	if !strings.HasPrefix(w.Name, "name-") || w.Size == 0 || w.ShippedAt.IsZero() {
		t.Fatalf("defaults not filled: %+v", w)
	}
	if w.Active {
		t.Fatal("a boolean should stay false unless the test sets it")
	}
	g := Make[Widget](srv, func(w *Widget) { w.Name = "gear"; w.Active = true })
	if g.Name != "gear" || !g.Active || g.ID == w.ID {
		t.Fatalf("override not applied or key reused: %+v", g)
	}
	if n := countWidgets(t, srv.DB()); n != 2 {
		t.Fatalf("rows in the table: %d, want 2", n)
	}
	many := MakeN[Widget](srv, 3)
	seen := map[string]bool{}
	for _, m := range many {
		if seen[m.Name] {
			t.Fatalf("MakeN reused the default %q", m.Name)
		}
		seen[m.Name] = true
	}
	if n := countWidgets(t, srv.DB()); n != 5 {
		t.Fatalf("rows in the table: %d, want 5", n)
	}
}

func TestMakeRefusesAnUnregisteredModel(t *testing.T) {
	srv := StartApp(t, widgetApp(t, TempSQLite(t)))
	type Gadget struct {
		model.BaseModel
		Name string
	}
	rec := &recordingTB{TB: t}
	shadow := *srv
	shadow.tb = rec
	func() {
		defer func() { _ = recover() }()
		_ = Make[Gadget](&shadow)
	}()
	if !strings.Contains(rec.fatal, `no model named "Gadget"`) {
		t.Fatalf("Make on an unregistered model said: %q", rec.fatal)
	}
}

// recordingTB captures Fatalf (and stops the caller with a panic the test
// recovers) so a test can assert on what the kit says when it refuses.
type recordingTB struct {
	testing.TB
	fatal string
}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.fatal = fmt.Sprintf(format, args...)
	panic("recorded")
}

func TestTransactionalRollsTheWholeTestBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	url := "sqlite://" + path

	// Inside: the application migrates and writes; everything it sees is
	// there.
	t.Run("inside", func(t *testing.T) {
		dbs := Transactional(t, map[string]app.DatabaseConfig{"default": {URL: url}})
		if dbs["default"].Driver == "" {
			t.Fatal("Transactional did not name a driver")
		}
		srv := StartApp(t, widgetApp(t, dbs))
		if err := srv.Runtime().AutoMigrate(&Widget{}); err != nil {
			t.Fatalf("automigrate inside the transaction: %v", err)
		}
		MakeN[Widget](srv, 3)
		if n := countWidgets(t, srv.DB()); n != 3 {
			t.Fatalf("inside the transaction the application counts %d, want 3", n)
		}
		// The application's own transaction is a savepoint that commits
		// and rolls back inside the test's.
		tx, err := srv.DB().Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec("INSERT INTO widgets (name, size, active, shipped_at, created_at, updated_at) VALUES ('rolled', 1, 0, ?, ?, ?)", time.Now(), time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n := countWidgets(t, srv.DB()); n != 3 {
			t.Fatalf("after the savepoint rolled back the application counts %d, want 3", n)
		}
	})

	// Outside, after the subtest's cleanup rolled the transaction back: the
	// file is as it was — not even the table.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var n int
	err = raw.QueryRow("SELECT COUNT(*) FROM widgets").Scan(&n)
	if err == nil {
		t.Fatalf("the table survived the rollback with %d rows", n)
	}
}

// TestTransactionalOnTheMatrixDatabase runs the same shape against the
// engine the CI matrix hands the process (NUCLEUS_SQL_MATRIX_URL), where
// "one connection" means something SQLite does not exercise. The table is
// created outside the transactional scope: on MySQL a CREATE TABLE commits
// the transaction it runs in, so what Transactional promises on every engine
// is the ROWS, and the schema only where DDL is transactional (PostgreSQL,
// SQLite, SQL Server). Skipped without the variable.
func TestTransactionalOnTheMatrixDatabase(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if url == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL not set; database matrix lane only")
	}
	if strings.HasPrefix(url, "sqlite") {
		t.Skip("the SQLite shape is TestTransactionalRollsTheWholeTestBack")
	}
	name, dsn, err := db.ResolveDriver(url)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open(name, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	table := fmt.Sprintf("nucleustest_tx_%d", time.Now().UnixNano())
	if _, err := raw.Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, name VARCHAR(64))"); err != nil {
		t.Fatalf("create the table outside the scope: %v", err)
	}
	defer func() { _, _ = raw.Exec("DROP TABLE " + table) }()
	placeholder := func(i int) string {
		if name == "pgx" {
			return fmt.Sprintf("$%d", i)
		}
		if name == "sqlserver" {
			return fmt.Sprintf("@p%d", i)
		}
		return "?"
	}

	t.Run("inside", func(t *testing.T) {
		dbs := Transactional(t, map[string]app.DatabaseConfig{"default": {URL: url}})
		srv := StartApp(t, widgetApp(t, dbs))
		for i := 0; i < 3; i++ {
			if _, err := srv.DB().Exec("INSERT INTO "+table+" (id, name) VALUES ("+placeholder(1)+", "+placeholder(2)+")", i, fmt.Sprintf("w-%d", i)); err != nil {
				t.Fatalf("insert: %v", err)
			}
		}
		var n int
		if err := srv.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 3 {
			t.Fatalf("inside: count=%d err=%v", n, err)
		}
		// The application's own transaction is a savepoint inside the test's.
		tx, err := srv.DB().Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec("INSERT INTO "+table+" (id, name) VALUES ("+placeholder(1)+", "+placeholder(2)+")", 9, "rolled"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if err := srv.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 3 {
			t.Fatalf("after the savepoint rolled back: count=%d err=%v", n, err)
		}
		// Another connection — the raw one — must not see the rows: they are
		// in the test's transaction, not in the database.
		if err := raw.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("a connection outside the scope sees %d rows (%v) while the scope is open", n, err)
		}
	})

	var n int
	if err := raw.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
		t.Fatalf("after the scope: %d rows survived the rollback (%v)", n, err)
	}
}
