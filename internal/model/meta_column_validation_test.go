package model

import "testing"

// TestParseDBTag_RejectsInvalidColumnName guards audit LOW-A: a `column:`
// storage tag whose value is not identifier-like must be rejected at
// ExtractMeta time, since the column name is interpolated into DDL and
// queries (ADR-011 allow-list barrier).
func TestParseDBTag_RejectsInvalidColumnName(t *testing.T) {
	type badHyphen struct {
		ID   int    `db:"pk"`
		Name string `db:"column:bad-name"`
	}
	if _, err := ExtractMeta(&badHyphen{}); err == nil {
		t.Error("expected ExtractMeta to reject a hyphenated column name")
	}

	type badSpace struct {
		ID    int    `db:"pk"`
		Email string `db:"column:foo bar"`
	}
	if _, err := ExtractMeta(&badSpace{}); err == nil {
		t.Error("expected ExtractMeta to reject a column name with a space")
	}

	type badInjection struct {
		ID   int    `db:"pk"`
		Evil string `db:"column:x)"`
	}
	if _, err := ExtractMeta(&badInjection{}); err == nil {
		t.Error("expected ExtractMeta to reject a column name with a paren")
	}

	// A valid identifier-like column (letters, digits, _, .) still passes.
	type okModel struct {
		ID   int    `db:"pk"`
		Name string `db:"column:full_name"`
	}
	if _, err := ExtractMeta(&okModel{}); err != nil {
		t.Errorf("valid column name rejected: %v", err)
	}
}
