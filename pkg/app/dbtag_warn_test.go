// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/model"
	"github.com/jcsvwinston/nucleus/pkg/db"
)

// A db-tag directive the parser does not recognize is applied as nothing;
// the sweep AutoMigrate runs must say so out loud. This pins the WARN wiring
// end to end: ExtractMeta → UnknownDBTokens → warnUnknownDBTags → slog.
func TestWarnUnknownDBTags(t *testing.T) {
	type Post struct {
		ID       int64  `db:"pk"`
		AuthorID int64  `db:"author_id,fk=users.id"` // phantom doc syntax, never parsed
		Email    string `db:"not null unique"`       // missing semicolon; prefix-matched pre-NU6-4
		Title    string `db:"column:title"`
	}
	meta, err := model.ExtractMeta(&Post{})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	warnUnknownDBTags(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})), meta)

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("expected a WARN, got: %q", out)
	}
	for _, want := range []string{"model=Post", "field=AuthorID", "author_id,fk=users.id", "field=Email", "not null unique"} {
		if !strings.Contains(out, want) {
			t.Fatalf("WARN missing %q; got: %q", want, out)
		}
	}
	if strings.Contains(out, "field=Title") {
		t.Fatalf("valid field must not be warned about; got: %q", out)
	}

	// Clean model → silent.
	buf.Reset()
	type Clean struct {
		ID   int64  `db:"pk"`
		Name string `db:"column:name;index"`
	}
	clean, err := model.ExtractMeta(&Clean{})
	if err != nil {
		t.Fatal(err)
	}
	warnUnknownDBTags(slog.New(slog.NewTextHandler(&buf, nil)), clean)
	if buf.Len() != 0 {
		t.Fatalf("expected no WARN for a clean model, got: %q", buf.String())
	}
}

// AutoMigrate is where a model's tags are read at boot since the registry
// left (DEP-2026-024): the sweep runs there, per model, before the DDL.
func TestAutoMigrateWarnsAboutUnknownDBTags(t *testing.T) {
	type Note struct {
		ID    int64  `db:"pk"`
		Title string `db:"title,size=255"` // Quark's grammar
	}
	var buf bytes.Buffer
	a := &App{
		Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})),
		DBs:    map[string]*db.DB{},
	}
	// No database named "default": AutoMigrate stops there, after the sweep.
	if err := a.AutoMigrate(&Note{}); err == nil {
		t.Fatal("AutoMigrate with no database must fail")
	}
	out := buf.String()
	for _, want := range []string{"level=WARN", "model=Note", "field=Title", "title,size=255", "Quark"} {
		if !strings.Contains(out, want) {
			t.Fatalf("AutoMigrate's WARN missing %q; got: %q", want, out)
		}
	}
}
