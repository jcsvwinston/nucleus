package app

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/model"
)

// A db-tag directive the parser does not recognize is applied as nothing; the
// boot sweep must say so out loud. This pins the WARN wiring end to end:
// Register → UnknownDBTokens → warnUnknownDBTags → slog.
func TestWarnUnknownDBTags(t *testing.T) {
	type Post struct {
		ID       int64  `db:"pk"`
		AuthorID int64  `db:"author_id,fk=users.id"` // phantom doc syntax, never parsed
		Email    string `db:"not null unique"`       // missing semicolon; prefix-matched pre-NU6-4
		Title    string `db:"column:title"`
	}

	reg := model.NewRegistry()
	if err := reg.Register(&Post{}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	a := &App{
		Models: reg,
		Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}

	warnUnknownDBTags(a)

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

	// Clean model → silent boot.
	buf.Reset()
	type Clean struct {
		ID   int64  `db:"pk"`
		Name string `db:"column:name;index"`
	}
	reg2 := model.NewRegistry()
	if err := reg2.Register(&Clean{}); err != nil {
		t.Fatal(err)
	}
	warnUnknownDBTags(&App{Models: reg2, Logger: slog.New(slog.NewTextHandler(&buf, nil))})
	if buf.Len() != 0 {
		t.Fatalf("expected no WARN for a clean model, got: %q", buf.String())
	}
}
