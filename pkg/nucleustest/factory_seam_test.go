// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Gear is the model the generators write (NU-137): Quark's tags, no
// registration anywhere. The kit does not read that grammar — the Factory
// installed with UseFactory does (the quarkmake module, in production; a
// recording double here).
type Gear struct {
	ID    uint   `db:"id" pk:"true"`
	Name  string `db:"name"`
	Teeth int    `db:"teeth"`
}

// Sprocket speaks pkg/model's tag grammar without embedding BaseModel and
// without a registration: the registry is where those tags are parsed, so
// no Factory gets it.
type Sprocket struct {
	ID   uint   `db:"pk"`
	Name string `db:"column:name;required"`
}

// recordingFactory is a Factory that writes nothing: it records the calls
// Make made, in order, with the record as Make handed it over.
type recordingFactory struct {
	calls  []string
	seqs   []int64
	nextID uint
}

func (f *recordingFactory) Fill(record any, n int64) error {
	g, ok := record.(*Gear)
	if !ok {
		return fmt.Errorf("recordingFactory knows Gear only, got %T", record)
	}
	f.calls = append(f.calls, fmt.Sprintf("fill(%d)", n))
	f.seqs = append(f.seqs, n)
	if g.Name == "" {
		g.Name = fmt.Sprintf("name-%d", n)
	}
	if g.Teeth == 0 {
		g.Teeth = int(n)
	}
	return nil
}

func (f *recordingFactory) Persist(_ context.Context, record any) error {
	g := record.(*Gear)
	f.calls = append(f.calls, fmt.Sprintf("persist(%s,%d)", g.Name, g.Teeth))
	f.nextID++
	g.ID = f.nextID
	return nil
}

func TestMakeWithoutAFactoryNamesTheQuarkmakeModule(t *testing.T) {
	srv := StartApp(t, widgetApp(t, TempSQLite(t)))
	rec := &recordingTB{TB: t}
	shadow := *srv
	shadow.tb = rec
	func() {
		defer func() { _ = recover() }()
		_ = Make[Gear](&shadow)
	}()
	for _, want := range []string{
		`no model named "Gear" is registered and no Factory is installed`,
		"github.com/jcsvwinston/nucleus/pkg/nucleustest/quarkmake",
		"srv.UseFactory(quarkmake.New(client, quarkmake.Model[Gear]()))",
	} {
		if !strings.Contains(rec.fatal, want) {
			t.Errorf("Make on a Quark model without a Factory said %q; want it to contain %q", rec.fatal, want)
		}
	}
}

func TestMakeDelegatesAnUnregisteredModelToTheFactory(t *testing.T) {
	srv := StartApp(t, widgetApp(t, TempSQLite(t)))
	f := &recordingFactory{}
	srv.UseFactory(f)

	// Fill, then the overrides, then Persist: the test's value is what gets
	// written, and the Factory fills only what the test left zero.
	g := Make[Gear](srv, func(g *Gear) { g.Name = "big" })
	if g.ID == 0 || g.Name != "big" || g.Teeth == 0 {
		t.Fatalf("Make returned %+v: want the key from Persist, the override kept, the default filled", g)
	}
	if len(f.calls) != 2 || !strings.HasPrefix(f.calls[0], "fill(") || f.calls[1] != fmt.Sprintf("persist(big,%d)", g.Teeth) {
		t.Fatalf("calls = %v; want fill, then persist with the override applied", f.calls)
	}

	// MakeN draws a fresh sequence number per record, so defaults never
	// collide across records.
	f.calls, f.seqs = nil, nil
	many := MakeN[Gear](srv, 3)
	seen := map[int64]bool{}
	for _, n := range f.seqs {
		if seen[n] {
			t.Fatalf("MakeN reused the sequence number %d: %v", n, f.seqs)
		}
		seen[n] = true
	}
	names := map[string]bool{}
	for _, m := range many {
		if names[m.Name] {
			t.Fatalf("MakeN produced the default %q twice", m.Name)
		}
		names[m.Name] = true
	}
	if len(names) != 3 || len(f.seqs) != 3 {
		t.Fatalf("MakeN made %d records over %d sequence numbers, want 3 and 3", len(names), len(f.seqs))
	}

	// A registered model keeps the registry path: the Factory is not asked.
	if err := srv.Runtime().AutoMigrate(&Widget{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	f.calls = nil
	w := Make[Widget](srv)
	if w.ID == 0 || len(f.calls) != 0 {
		t.Fatalf("a registered model must go through the registry, not the Factory: id=%d, factory calls=%v", w.ID, f.calls)
	}

	// nil uninstalls.
	srv.UseFactory(nil)
	rec := &recordingTB{TB: t}
	shadow := *srv
	shadow.tb = rec
	func() {
		defer func() { _ = recover() }()
		_ = Make[Gear](&shadow)
	}()
	if !strings.Contains(rec.fatal, "no Factory is installed") {
		t.Fatalf("after UseFactory(nil), Make said %q", rec.fatal)
	}
}

func TestMakeRefusesAnUnregisteredPkgModelStructEvenWithAFactory(t *testing.T) {
	srv := StartApp(t, widgetApp(t, TempSQLite(t)))
	f := &recordingFactory{}
	srv.UseFactory(f)
	rec := &recordingTB{TB: t}
	shadow := *srv
	shadow.tb = rec
	func() {
		defer func() { _ = recover() }()
		_ = Make[Sprocket](&shadow)
	}()
	if !strings.Contains(rec.fatal, `no model named "Sprocket"`) || !strings.Contains(rec.fatal, "Quark model goes to the Factory") {
		t.Fatalf("Make on an unregistered pkg/model struct said: %q", rec.fatal)
	}
	if len(f.calls) != 0 {
		t.Fatalf("the Factory must not see a pkg/model struct, got %v", f.calls)
	}
}

func TestUsesModelGrammarTellsTheTwoGrammarsApart(t *testing.T) {
	for _, tc := range []struct {
		typ  reflect.Type
		want bool
	}{
		{reflect.TypeOf(Widget{}), true},   // embeds model.BaseModel
		{reflect.TypeOf(Sprocket{}), true}, // column:…;required
		{reflect.TypeOf(Gear{}), false},    // Quark: db:"<column>", pk:"true"
	} {
		if got := usesModelGrammar(tc.typ); got != tc.want {
			t.Errorf("usesModelGrammar(%s) = %v, want %v", tc.typ, got, tc.want)
		}
	}
}
