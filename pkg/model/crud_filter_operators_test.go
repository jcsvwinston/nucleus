package model

// Filters with operators, and a total a pager can divide.
//
// QueryOpts.Filters is column → value: equality was the whole filter
// language, so "views greater than 100" and "title contains pop" had nowhere
// to go. And a FILTERED list never carried a total — the answer was -1 with
// IsEstimated true — so no caller could say how many pages there were
// (OR-45 of the 2026-09 maturity audit).
//
// Both are additions: Where sits beside Filters and ExactTotal is opt-in, so
// a caller that sets neither behaves exactly as it did.

import (
	"context"
	"testing"
)

type Widget struct {
	ID    int64  `db:"pk"`
	Name  string `db:"column:name" admin:"search"`
	Views int    `db:"column:views"`
	Tag   string `db:"column:tag"`
}

func widgetCRUD(t *testing.T) (*CRUD, context.Context) {
	t.Helper()
	sqlDB := setupTestDB(t)
	ctx := context.Background()
	for _, stmt := range []string{
		`CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT, views INTEGER, tag TEXT)`,
		`INSERT INTO widgets (name, views, tag) VALUES
			('cheap popular', 1, 'a'),
			('popular thing', 500, 'b'),
			('quiet thing', 50, 'b'),
			('100% sure', 7, NULL)`,
	} {
		if _, err := sqlDB.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	meta, err := ExtractMeta(&Widget{})
	if err != nil {
		t.Fatalf("ExtractMeta: %v", err)
	}
	return NewCRUD(sqlDB, meta, nil), ctx
}

func widgetNames(t *testing.T, res *PaginatedResult) []string {
	t.Helper()
	items, ok := res.Items.([]Widget)
	if !ok {
		t.Fatalf("items are %T, want []Widget", res.Items)
	}
	out := make([]string, 0, len(items))
	for _, w := range items {
		out = append(out, w.Name)
	}
	return out
}

func TestCRUD_WhereOperators(t *testing.T) {
	crud, ctx := widgetCRUD(t)

	cases := []struct {
		name   string
		filter Filter
		want   []string
	}{
		{"greater than", Filter{Column: "views", Op: OpGreater, Value: "100"}, []string{"popular thing"}},
		{"greater or equal", Filter{Column: "views", Op: OpGreaterEqual, Value: "50"}, []string{"popular thing", "quiet thing"}},
		{"less than", Filter{Column: "views", Op: OpLess, Value: "10"}, []string{"cheap popular", "100% sure"}},
		{"not equal", Filter{Column: "tag", Op: OpNotEqual, Value: "b"}, []string{"cheap popular"}},
		{"contains", Filter{Column: "name", Op: OpContains, Value: "popular"}, []string{"cheap popular", "popular thing"}},
		{"starts with", Filter{Column: "name", Op: OpStartsWith, Value: "popular"}, []string{"popular thing"}},
		{"ends with", Filter{Column: "name", Op: OpEndsWith, Value: "thing"}, []string{"popular thing", "quiet thing"}},
		{"in", Filter{Column: "views", Op: OpIn, Values: []string{"1", "50"}}, []string{"cheap popular", "quiet thing"}},
		{"not in", Filter{Column: "views", Op: OpNotIn, Values: []string{"1", "50", "7"}}, []string{"popular thing"}},
		{"is null", Filter{Column: "tag", Op: OpIsNull, Value: "true"}, []string{"100% sure"}},
		{"is not null", Filter{Column: "tag", Op: OpIsNull, Value: "false"}, []string{"cheap popular", "popular thing", "quiet thing"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := crud.FindAll(ctx, QueryOpts{Where: []Filter{tc.filter}, OrderBy: "id asc"})
			if err != nil {
				t.Fatalf("FindAll: %v", err)
			}
			got := widgetNames(t, res)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// A % in the value is a character, not a wildcard: without the escape, "100%"
// would match every row whose name starts with 100 — and, worse, a bare "%"
// would match everything while looking like a filter.
func TestCRUD_WhereContainsEscapesWildcards(t *testing.T) {
	crud, ctx := widgetCRUD(t)

	res, err := crud.FindAll(ctx, QueryOpts{
		Where: []Filter{{Column: "name", Op: OpContains, Value: "100%"}},
	})
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if got := widgetNames(t, res); len(got) != 1 || got[0] != "100% sure" {
		t.Fatalf("got %v, want only the row whose name contains a literal 100%%", got)
	}

	res, err = crud.FindAll(ctx, QueryOpts{
		Where: []Filter{{Column: "name", Op: OpContains, Value: "%"}},
	})
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if got := widgetNames(t, res); len(got) != 1 {
		t.Fatalf("a bare %% matched %d rows: it is being treated as a wildcard", len(got))
	}
}

// An empty set matches nothing. Dropping the clause instead would answer
// every row — a filter that silently means "no filter" is the worst kind.
func TestCRUD_WhereEmptyInMatchesNothing(t *testing.T) {
	crud, ctx := widgetCRUD(t)
	res, err := crud.FindAll(ctx, QueryOpts{Where: []Filter{{Column: "views", Op: OpIn}}})
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if got := widgetNames(t, res); len(got) != 0 {
		t.Fatalf("an empty IN matched %v", got)
	}
}

// Where and Filters are ANDed, and both still work next to search.
func TestCRUD_WhereCombinesWithFiltersAndSearch(t *testing.T) {
	crud, ctx := widgetCRUD(t)
	res, err := crud.FindAll(ctx, QueryOpts{
		Filters: map[string]string{"tag": "b"},
		Where:   []Filter{{Column: "views", Op: OpGreaterEqual, Value: "100"}},
		Search:  "popular",
	})
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if got := widgetNames(t, res); len(got) != 1 || got[0] != "popular thing" {
		t.Fatalf("got %v, want the one row matching all three", got)
	}
}

// A column that does not resolve is dropped, exactly as an unknown key in
// Filters already was — the caller validates when it must not be dropped.
func TestCRUD_WhereUnknownColumnIsDropped(t *testing.T) {
	crud, ctx := widgetCRUD(t)
	res, err := crud.FindAll(ctx, QueryOpts{Where: []Filter{{Column: "nope; DROP TABLE widgets", Op: OpEqual, Value: "x"}}})
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if got := widgetNames(t, res); len(got) != 4 {
		t.Fatalf("got %d rows, want the unknown column dropped and everything listed", len(got))
	}
}

func TestParseFilterOp(t *testing.T) {
	for _, raw := range []string{"gt", "GTE", " contains ", "in", "isnull"} {
		if _, ok := ParseFilterOp(raw); !ok {
			t.Errorf("%q is a known operator and was refused", raw)
		}
	}
	for _, raw := range []string{"", "like", "regex", "drop"} {
		if _, ok := ParseFilterOp(raw); ok {
			t.Errorf("%q is not an operator and was accepted", raw)
		}
	}
}

// OR-45: a filtered list carries a total a pager can divide when the caller
// asks for one, and the total describes THE QUERY — not the table.
func TestCRUD_ExactTotalCountsTheFilteredQuery(t *testing.T) {
	crud, ctx := widgetCRUD(t)

	res, err := crud.FindAll(ctx, QueryOpts{
		PageSize:   2,
		Filters:    map[string]string{"tag": "b"},
		ExactTotal: true,
		OrderBy:    "id asc",
	})
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("total = %d, want the 2 rows the filter matches", res.Total)
	}
	if res.IsEstimated {
		t.Error("an exact total reported itself as an estimate")
	}
	if res.TotalPages != 1 {
		t.Errorf("total_pages = %d, want 1", res.TotalPages)
	}

	// And with an operator filter, which is the case that could not be
	// counted at all before.
	res, err = crud.FindAll(ctx, QueryOpts{
		PageSize:   1,
		Where:      []Filter{{Column: "views", Op: OpGreaterEqual, Value: "50"}},
		ExactTotal: true,
	})
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if res.Total != 2 || res.TotalPages != 2 {
		t.Fatalf("total/total_pages = %d/%d, want 2/2", res.Total, res.TotalPages)
	}
}

// Without ExactTotal nothing changes: an unfiltered list still answers the
// cheap estimate and a filtered one still says it does not know.
func TestCRUD_WithoutExactTotalTheAnswerIsUnchanged(t *testing.T) {
	crud, ctx := widgetCRUD(t)

	res, err := crud.FindAll(ctx, QueryOpts{Filters: map[string]string{"tag": "b"}})
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if res.Total != -1 || !res.IsEstimated {
		t.Fatalf("filtered total = %d (estimated %v), want the unchanged -1/true", res.Total, res.IsEstimated)
	}
}
