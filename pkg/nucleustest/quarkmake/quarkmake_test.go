// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package quarkmake_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest/quarkmake"
	"github.com/jcsvwinston/quark"
	_ "github.com/jcsvwinston/quark/drivers/sqlite"
)

// The Factory satisfies the kit's seam without this module importing the
// kit outside its tests: Go interfaces are structural, and keeping the
// import out of the package is what lets it build against the framework
// release it pins.
var _ nucleustest.Factory = (*quarkmake.Factory)(nil)

// Gear is the model the generators write: Quark's tags, a pinned table,
// no registration anywhere. Note is not a column (db:"-"); Owner is a
// relation, not a column.
type Gear struct {
	ID        uint       `db:"id" pk:"true" json:"id"`
	Name      string     `db:"name" quark:"not_null" json:"name"`
	Teeth     int        `db:"teeth" json:"teeth"`
	Note      string     `db:"-" json:"-"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt time.Time  `db:"updated_at" json:"updated_at"`
	DeletedAt *time.Time `db:"deleted_at" json:"-"`
}

// TableName pins the table, as the generated model does.
func (Gear) TableName() string { return "gear_parts" }

// Cog has no TableName — Quark's default naming applies (cogs) — and no pk
// tag: the key is the column named id. The option after the comma is
// Quark's sizing grammar, not part of the column name.
type Cog struct {
	ID    int64  `db:"id"`
	Label string `db:"label,size=64"`
}

// gearApp is an application with one route that reads a gear back through
// Quark — the way a handler of the application would.
func gearApp(t *testing.T, client **quark.Client) nucleus.App {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.JWTSecret = strings.Repeat("quarkmake-secret-", 3)
	cfg.Databases = nucleustest.TempSQLite(t)
	m := nucleus.Module[struct{}]{
		Name: "gears",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/gears/{id}", func(c *nucleus.Context) error {
				id, err := strconv.Atoi(c.Param("id"))
				if err != nil {
					return c.JSON(http.StatusBadRequest, map[string]string{"error": "id"})
				}
				g, err := quark.For[Gear](c.Context.Request.Context(), *client).Find(id)
				if err != nil {
					return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
				}
				return c.JSON(http.StatusOK, g)
			})
		},
	}.Build()
	return nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{m.Name(): m},
		Options: []app.Option{app.WithOpenAuthz()},
	}
}

// start boots the application, opens Quark on its database, creates the
// tables through Quark and installs the Factory — the recipe the docs give.
func start(t *testing.T) (*nucleustest.Server, *quark.Client) {
	t.Helper()
	var client *quark.Client
	srv := nucleustest.StartApp(t, gearApp(t, &client))
	c, err := quark.NewWithDB("sqlite", srv.DB())
	if err != nil {
		t.Fatalf("quark.NewWithDB: %v", err)
	}
	client = c
	if err := client.Migrate(context.Background(), &Gear{}, &Cog{}); err != nil {
		t.Fatalf("quark migrate: %v", err)
	}
	srv.UseFactory(quarkmake.New(client, quarkmake.Model[Gear](), quarkmake.Model[Cog]()))
	return srv, client
}

func TestMakePersistsAQuarkModelThroughQuark(t *testing.T) {
	srv, client := start(t)

	g := nucleustest.Make[Gear](srv)
	if g.ID == 0 {
		t.Fatal("Make did not fill the primary key of the Quark model")
	}
	if !strings.HasPrefix(g.Name, "name-") || g.Teeth == 0 {
		t.Fatalf("defaults not filled from Quark's metadata: %+v", g)
	}
	if g.CreatedAt.IsZero() || g.UpdatedAt.IsZero() {
		t.Fatalf("created_at/updated_at must be stamped by Quark's Create: %+v", g)
	}
	if g.DeletedAt != nil {
		t.Fatalf("deleted_at must stay nil, got %v", g.DeletedAt)
	}
	if g.Note != "" {
		t.Fatalf("a db:\"-\" field is not a column and gets no default, got %q", g.Note)
	}

	// What a handler reads back through Quark is what Make wrote.
	var got Gear
	resp := srv.Get("/gears/" + strconv.FormatUint(uint64(g.ID), 10))
	if resp.Status != http.StatusOK {
		t.Fatalf("GET /gears/%d: %d %s", g.ID, resp.Status, resp.String())
	}
	resp.JSON(t, &got)
	if got.ID != g.ID || got.Name != g.Name || got.Teeth != g.Teeth {
		t.Fatalf("the handler read %+v, Make wrote %+v", got, g)
	}

	// Overrides win over defaults; the sequence never reuses a key.
	big := nucleustest.Make[Gear](srv, func(g *Gear) { g.Name = "big"; g.Teeth = 99 })
	if big.Name != "big" || big.Teeth != 99 || big.ID == g.ID {
		t.Fatalf("override not applied or key reused: %+v", big)
	}

	// MakeN: distinct defaults, distinct keys, all persisted — counted
	// through Quark, soft-delete scope included.
	many := nucleustest.MakeN[Gear](srv, 3)
	seen := map[string]bool{}
	ids := map[uint]bool{}
	for _, m := range many {
		if seen[m.Name] || ids[m.ID] {
			t.Fatalf("MakeN reused the default %q or the key %d", m.Name, m.ID)
		}
		seen[m.Name], ids[m.ID] = true, true
	}
	n, err := quark.For[Gear](context.Background(), client).Count()
	if err != nil || n != 5 {
		t.Fatalf("rows in gear_parts through Quark: %d (%v), want 5", n, err)
	}
}

func TestMakeUsesQuarkDefaultTableAndIDFallback(t *testing.T) {
	srv, client := start(t)
	c := nucleustest.Make[Cog](srv)
	if c.ID == 0 {
		t.Fatal("Make did not fill the id key of a model without a pk tag")
	}
	if !strings.HasPrefix(c.Label, "label-") {
		t.Fatalf("the column option after the comma is not part of the default's name: %+v", c)
	}
	got, err := quark.For[Cog](context.Background(), client).Find(c.ID)
	if err != nil || got.Label != c.Label {
		t.Fatalf("the row must land in cogs, Quark's default table for Cog: %+v, %v", got, err)
	}
}

func TestFactoryNamesTheModelItWasNotGiven(t *testing.T) {
	srv, client := start(t)
	type Sprocket struct {
		ID   uint   `db:"id" pk:"true"`
		Name string `db:"name"`
	}
	f := quarkmake.New(client, quarkmake.Model[Gear]())
	err := f.Fill(&Sprocket{}, 1)
	if err == nil || !strings.Contains(err.Error(), "quarkmake.Model[Sprocket]()") {
		t.Fatalf("Fill on a model the Factory was not given: %v", err)
	}
	if err := f.Persist(context.Background(), &Sprocket{}); err == nil || !strings.Contains(err.Error(), "not one of the models") {
		t.Fatalf("Persist on a model the Factory was not given: %v", err)
	}
	if err := f.Fill(Gear{}, 1); err == nil || !strings.Contains(err.Error(), "pointer to a struct") {
		t.Fatalf("Fill on a value, not a pointer: %v", err)
	}
	_ = srv
}

// Bad input to New is reported as an error from the first use — Make fails
// the test with it — never as a panic (the repository's rule for every
// constructor).
func TestNewReportsBadInputAsAnErrorNotAPanic(t *testing.T) {
	_, client := start(t)
	noClient := quarkmake.New(nil, quarkmake.Model[Gear]())
	if err := noClient.Fill(&Gear{}, 1); err == nil || !strings.Contains(err.Error(), "nil ClientProvider") {
		t.Fatalf("New(nil) must surface from Fill: %v", err)
	}
	zeroSpec := quarkmake.New(client, quarkmake.Model[Gear](), quarkmake.Spec{})
	if err := zeroSpec.Persist(context.Background(), &Gear{}); err == nil || !strings.Contains(err.Error(), "zero Spec at position 2") {
		t.Fatalf("New with a zero Spec must surface from Persist: %v", err)
	}
}
