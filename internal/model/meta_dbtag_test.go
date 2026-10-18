package model

import (
	"strings"
	"testing"
)

// The `db:"-"` convention (encoding/json, sqlx) excludes the field from the
// persistence layer entirely. Before v1.3.2 the token was silently ignored —
// the documented "Ignore" behaviour simply did not happen.
func TestExtractMeta_DBTagDashExcludesField(t *testing.T) {
	type Draft struct {
		ID    int64  `db:"pk"`
		Title string `db:"column:title"`
		Cache string `db:"-"`
	}

	meta, err := ExtractMeta(&Draft{})
	if err != nil {
		t.Fatal(err)
	}

	var cols []string
	for _, f := range meta.Fields {
		if f.Name == "Cache" {
			t.Fatalf("field with db:\"-\" must not appear in meta.Fields; got column %q", f.Column)
		}
		cols = append(cols, f.Column)
	}
	if strings.Contains(strings.Join(cols, ","), "cache") {
		t.Fatalf("db:\"-\" field leaked into columns: %v", cols)
	}
	if len(meta.Fields) != 2 {
		t.Fatalf("expected 2 fields (ID, Title), got %d", len(meta.Fields))
	}
}

// Unrecognized db-tag directives must be recorded so App.Run can surface them
// as a boot WARN. The cases mirror the phantom syntax that the website taught
// for four audit rounds while the parser ignored it silently (NU5-1).
func TestExtractMeta_UnknownDBTokensRecorded(t *testing.T) {
	type Article struct {
		ID       int64  `db:"id,primary"`
		AuthorID int64  `db:"author_id,fk=users.id"`
		Title    string `db:"column:title"`
	}

	meta, err := ExtractMeta(&Article{})
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]FieldMeta{}
	for _, f := range meta.Fields {
		byName[f.Name] = f
	}

	// Note: ID still ends up as the PK — by the name convention, not because
	// the comma syntax parsed. The tag itself must be flagged as unknown.
	if got := byName["ID"].UnknownDBTokens; len(got) != 1 || got[0] != "id,primary" {
		t.Fatalf("expected [id,primary] recorded as unknown, got %v", got)
	}
	if got := byName["AuthorID"].UnknownDBTokens; len(got) != 1 || got[0] != "author_id,fk=users.id" {
		t.Fatalf("expected [author_id,fk=users.id] recorded as unknown, got %v", got)
	}
	if len(byName["Title"].UnknownDBTokens) != 0 {
		t.Fatalf("valid directives must not be flagged: %v", byName["Title"].UnknownDBTokens)
	}
}

// "not null" must match by strict equality. The old prefix match made
// `db:"not null unique"` (missing semicolon — the classic typo) mark
// required while silently dropping the unique index: exactly the false
// negative the UnknownDBTokens WARN promised to eliminate (NU6-4).
func TestExtractMeta_NotNullStrictEquality(t *testing.T) {
	type Account struct {
		ID    int64  `db:"pk"`
		Email string `db:"not null unique"` // typo: space instead of semicolon
		Name  string `db:"not null;unique"` // correct form
	}

	meta, err := ExtractMeta(&Account{})
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]FieldMeta{}
	for _, f := range meta.Fields {
		byName[f.Name] = f
	}

	// The typo'd tag must not half-apply: no silent required, no silently
	// dropped unique — the full token is recorded for the boot WARN.
	email := byName["Email"]
	if got := email.UnknownDBTokens; len(got) != 1 || got[0] != "not null unique" {
		t.Fatalf("expected the full token %q recorded as unknown, got %v", "not null unique", got)
	}
	if email.IsRequired {
		t.Fatal("a typo'd directive must not silently mark the field required")
	}
	if len(email.IndexRefs) != 0 {
		t.Fatalf("a typo'd directive must not create indexes, got %v", email.IndexRefs)
	}

	// The correct form applies both directives and warns about nothing.
	name := byName["Name"]
	if !name.IsRequired {
		t.Fatal("db:\"not null;unique\" must mark the field required")
	}
	if len(name.IndexRefs) != 1 || !name.IndexRefs[0].Unique {
		t.Fatalf("db:\"not null;unique\" must create a unique index, got %v", name.IndexRefs)
	}
	if len(name.UnknownDBTokens) != 0 {
		t.Fatalf("valid directives must not be flagged: %v", name.UnknownDBTokens)
	}

	// The exact token still parses.
	type Strict struct {
		ID   int64  `db:"pk"`
		Slug string `db:"not null"`
	}
	strictMeta, err := ExtractMeta(&Strict{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range strictMeta.Fields {
		if f.Name != "Slug" {
			continue
		}
		if !f.IsRequired {
			t.Fatal("db:\"not null\" alone must still mark the field required")
		}
		if len(f.UnknownDBTokens) != 0 {
			t.Fatalf("db:\"not null\" alone must not be flagged: %v", f.UnknownDBTokens)
		}
	}
}
