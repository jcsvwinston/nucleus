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
