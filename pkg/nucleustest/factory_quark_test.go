// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

// Gear is the model the generators write (NU-137): Quark's tags, a pinned
// table, no registration anywhere. Note is not a column (db:"-").
type Gear struct {
	ID        uint       `db:"id" pk:"true" json:"id"`
	Name      string     `db:"name" quark:"not_null" json:"name"`
	Teeth     int        `db:"teeth" json:"teeth"`
	Note      string     `db:"-"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt time.Time  `db:"updated_at" json:"updated_at"`
	DeletedAt *time.Time `db:"deleted_at" json:"-"`
}

// TableName pins the table, as the generated model does.
func (Gear) TableName() string { return "gear_parts" }

// Cog has no TableName — Quark's default naming applies — and no pk tag:
// the key is the column named id. The column option after the comma is
// Quark's sizing grammar, not part of the name.
type Cog struct {
	ID    int64  `db:"id"`
	Label string `db:"label,size=64"`
}

// Sprocket speaks pkg/model's tag grammar without embedding BaseModel and
// without a registration: the registry is where those tags are parsed.
type Sprocket struct {
	ID   uint   `db:"pk"`
	Name string `db:"column:name;required"`
}

func quarkTables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, ddl := range []string{
		`CREATE TABLE gear_parts (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, teeth INTEGER, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE cogs (id INTEGER PRIMARY KEY AUTOINCREMENT, label TEXT)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
}

func TestMakePersistsAQuarkModelFromItsTags(t *testing.T) {
	srv := StartApp(t, widgetApp(t, TempSQLite(t)))
	quarkTables(t, srv.DB())

	g := Make[Gear](srv)
	if g.ID == 0 {
		t.Fatal("Make did not fill the primary key of the Quark model")
	}
	if !strings.HasPrefix(g.Name, "name-") || g.Teeth == 0 {
		t.Fatalf("defaults not filled from the Quark tags: %+v", g)
	}
	if g.CreatedAt.IsZero() || g.UpdatedAt.IsZero() {
		t.Fatalf("created_at/updated_at not stamped: %+v", g)
	}
	if g.DeletedAt != nil {
		t.Fatalf("deleted_at must stay nil, got %v", g.DeletedAt)
	}
	if g.Note != "" {
		t.Fatalf("a db:\"-\" field is not a column and gets no default, got %q", g.Note)
	}

	// The row is in the pinned table, with the column values the struct
	// carries and a NULL soft-delete column.
	var name string
	var teeth int
	var deleted sql.NullTime
	if err := srv.DB().QueryRow("SELECT name, teeth, deleted_at FROM gear_parts WHERE id = ?", g.ID).Scan(&name, &teeth, &deleted); err != nil {
		t.Fatalf("read the row back: %v", err)
	}
	if name != g.Name || teeth != g.Teeth || deleted.Valid {
		t.Fatalf("row (%q, %d, deleted=%v) does not match the record %+v", name, teeth, deleted.Valid, g)
	}

	// Overrides win over defaults; the sequence never reuses a key.
	big := Make[Gear](srv, func(g *Gear) { g.Name = "big"; g.Teeth = 99 })
	if big.Name != "big" || big.Teeth != 99 || big.ID == g.ID {
		t.Fatalf("override not applied or key reused: %+v", big)
	}

	// MakeN: distinct defaults, distinct keys, all persisted.
	many := MakeN[Gear](srv, 3)
	seen := map[string]bool{}
	ids := map[uint]bool{}
	for _, m := range many {
		if seen[m.Name] || ids[m.ID] {
			t.Fatalf("MakeN reused the default %q or the key %d", m.Name, m.ID)
		}
		seen[m.Name], ids[m.ID] = true, true
	}
	var n int
	if err := srv.DB().QueryRow("SELECT COUNT(*) FROM gear_parts").Scan(&n); err != nil || n != 5 {
		t.Fatalf("rows in gear_parts: %d (%v), want 5", n, err)
	}
}

func TestMakeUsesQuarkDefaultTableNameAndIDFallback(t *testing.T) {
	srv := StartApp(t, widgetApp(t, TempSQLite(t)))
	quarkTables(t, srv.DB())

	c := Make[Cog](srv)
	if c.ID == 0 {
		t.Fatal("Make did not fill the id key of a model without a pk tag")
	}
	var label string
	if err := srv.DB().QueryRow("SELECT label FROM cogs WHERE id = ?", c.ID).Scan(&label); err != nil {
		t.Fatalf("the row must land in cogs, Quark's default table for Cog: %v", err)
	}
	if label != c.Label || !strings.HasPrefix(label, "label-") {
		t.Fatalf("label column %q does not match the record %+v", label, c)
	}
}

func TestMakeRefusesAnUnregisteredPkgModelStruct(t *testing.T) {
	srv := StartApp(t, widgetApp(t, TempSQLite(t)))
	rec := &recordingTB{TB: t}
	shadow := *srv
	shadow.tb = rec
	func() {
		defer func() { _ = recover() }()
		_ = Make[Sprocket](&shadow)
	}()
	if !strings.Contains(rec.fatal, `no model named "Sprocket"`) || !strings.Contains(rec.fatal, "Quark model needs no registration") {
		t.Fatalf("Make on an unregistered pkg/model struct said: %q", rec.fatal)
	}
}

func TestQuarkNamingMirrorsQuark(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Cog", "cogs"},
		{"UserProfile", "user_profiles"},
		{"Category", "categories"},
		{"Box", "boxes"},
		{"HTTPServer", "http_servers"},
		{"Day", "days"},
	} {
		if got := quarkSnakeCase(quarkPluralize(tc.in)); got != tc.want {
			t.Errorf("default table for %s = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := quarkColumn(" price , precision=18,scale=4"); got != "price" {
		t.Errorf("quarkColumn = %q, want price", got)
	}
}
