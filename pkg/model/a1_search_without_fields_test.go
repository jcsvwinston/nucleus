package model

import (
	"context"
	"errors"
	"reflect"
	"testing"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

// OR-43 (maturity audit 2026-09-03): a search term against a model with no
// searchable field was silently ignored — FindAll answered page one of
// everything, so an operator typing "alice" saw every row and no warning.
// Now it is a 400 that names the model and how to enable search; a model
// with a search field keeps working, and an empty term is still a plain
// listing.
func TestCRUD_FindAll_SearchWithoutSearchableFieldsIsBadRequest(t *testing.T) {
	type Opaque struct {
		ID   int64  `db:"pk"`
		Body string `db:"column:body"`
	}
	type Searchable struct {
		ID   int64  `db:"pk"`
		Body string `db:"column:body" admin:"search"`
	}

	sqlDB := setupTestDB(t)
	ctx := context.Background()
	for _, ddl := range []string{
		`CREATE TABLE opaques (id INTEGER PRIMARY KEY, body TEXT)`,
		`CREATE TABLE searchables (id INTEGER PRIMARY KEY, body TEXT)`,
		`INSERT INTO opaques (body) VALUES ('alice'), ('bob')`,
		`INSERT INTO searchables (body) VALUES ('alice'), ('bob')`,
	} {
		if _, err := sqlDB.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("%s: %v", ddl, err)
		}
	}

	opaqueMeta, err := ExtractMeta(&Opaque{})
	if err != nil {
		t.Fatalf("ExtractMeta(Opaque): %v", err)
	}
	opaque := NewCRUD(sqlDB, opaqueMeta, nil)

	_, err = opaque.FindAll(ctx, QueryOpts{Search: "alice"})
	var domErr *gferrors.DomainError
	if !errors.As(err, &domErr) || domErr.StatusCode != 400 {
		t.Fatalf("search on a model without searchable fields: got err=%v, want a 400 DomainError", err)
	}
	if got := domErr.Message; got == "" || !contains(got, "Opaque") || !contains(got, `admin:"search"`) {
		t.Fatalf("the error must name the model and the tag that enables search, got %q", got)
	}

	// An empty or blank term is not a search: the listing still works.
	res, err := opaque.FindAll(ctx, QueryOpts{Search: "   "})
	if err != nil || itemCount(res) != 2 {
		t.Fatalf("blank search must list everything: err=%v items=%d", err, itemCount(res))
	}

	searchMeta, err := ExtractMeta(&Searchable{})
	if err != nil {
		t.Fatalf("ExtractMeta(Searchable): %v", err)
	}
	res, err = NewCRUD(sqlDB, searchMeta, nil).FindAll(ctx, QueryOpts{Search: "ali"})
	if err != nil || itemCount(res) != 1 {
		t.Fatalf("search on a model with a search field must filter: err=%v items=%d", err, itemCount(res))
	}
}

func itemCount(res *PaginatedResult) int {
	if res == nil || res.Items == nil {
		return 0
	}
	return reflect.ValueOf(res.Items).Len()
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
