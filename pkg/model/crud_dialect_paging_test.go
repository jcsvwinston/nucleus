package model

import (
	"context"
	"strings"
	"testing"
)

// capturePagingSQL runs FindAll (page 2, size 5) and FindByID under the given
// dialect and returns the observed select.list / select.one events. The
// backing store is SQLite, so non-SQLite grammars fail at execution — that is
// fine: the SQL observer records the statement exactly as it would reach the
// engine (post-rebind), which is the surface under test.
func capturePagingSQL(t *testing.T, dialect string) (list, one SQLQueryEvent) {
	t.Helper()
	sqlDB := setupTestDB(t)
	meta, err := ExtractMeta(&TestUser{})
	if err != nil {
		t.Fatalf("ExtractMeta: %v", err)
	}
	meta.Config = ModelConfig{PageSize: 25}

	crud := NewCRUD(sqlDB, meta, nil)
	crud.SetDialect(dialect)

	var gotList, gotOne bool
	crud.SetSQLQueryObserver(func(_ context.Context, ev SQLQueryEvent) {
		switch ev.Operation {
		case "select.list":
			list, gotList = ev, true
		case "select.one":
			one, gotOne = ev, true
		}
	})

	_, _ = crud.FindAll(context.Background(), QueryOpts{Page: 2, PageSize: 5})
	_, _ = crud.FindByID(context.Background(), 1)

	if !gotList || !gotOne {
		t.Fatalf("dialect %s: expected select.list and select.one events (got list=%v one=%v)", dialect, gotList, gotOne)
	}
	return list, one
}

// TestCRUDPagingSQL_DialectShapes pins the paging/single-row grammar each
// dialect receives. mssql learned OFFSET … FETCH and TOP 1 in NU5-4 after its
// first live run rejected LIMIT; Oracle went through the identical failure in
// NU8-1 (ORA-00933: no LIMIT clause in that grammar either) and pages with
// OFFSET … FETCH / FETCH FIRST. The argument order is part of the pin:
// OFFSET-first for mssql/oracle, limit-first for the LIMIT dialects.
func TestCRUDPagingSQL_DialectShapes(t *testing.T) {
	// FindAll runs with Page=2, PageSize=5 → offset 5, fetch 6 (PageSize+1
	// for HasMore detection).
	cases := []struct {
		dialect      string
		wantList     string
		wantListArgs []interface{}
		wantOne      string
		banned       []string
	}{
		{
			dialect:      "sqlite",
			wantList:     " LIMIT ? OFFSET ?",
			wantListArgs: []interface{}{6, 5},
			wantOne:      " LIMIT 1",
			banned:       []string{"FETCH", "TOP 1"},
		},
		{
			dialect:      "mysql",
			wantList:     " LIMIT ? OFFSET ?",
			wantListArgs: []interface{}{6, 5},
			wantOne:      " LIMIT 1",
			banned:       []string{"FETCH", "TOP 1"},
		},
		{
			dialect:      "postgres",
			wantList:     " LIMIT $1 OFFSET $2",
			wantListArgs: []interface{}{6, 5},
			wantOne:      " LIMIT 1",
			banned:       []string{"FETCH", "TOP 1"},
		},
		{
			dialect:      "mssql",
			wantList:     " OFFSET @p1 ROWS FETCH NEXT @p2 ROWS ONLY",
			wantListArgs: []interface{}{5, 6},
			wantOne:      "SELECT TOP 1 ",
			banned:       []string{"LIMIT"},
		},
		{
			dialect:      "oracle",
			wantList:     " OFFSET :1 ROWS FETCH NEXT :2 ROWS ONLY",
			wantListArgs: []interface{}{5, 6},
			wantOne:      " FETCH FIRST 1 ROWS ONLY",
			banned:       []string{"LIMIT", "TOP 1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.dialect, func(t *testing.T) {
			list, one := capturePagingSQL(t, tc.dialect)

			if !strings.Contains(list.Query, tc.wantList) {
				t.Errorf("select.list %q must contain %q", list.Query, tc.wantList)
			}
			if !strings.Contains(one.Query, tc.wantOne) {
				t.Errorf("select.one %q must contain %q", one.Query, tc.wantOne)
			}
			for _, ban := range tc.banned {
				if strings.Contains(list.Query, ban) {
					t.Errorf("select.list %q must not contain %q on %s", list.Query, ban, tc.dialect)
				}
				if strings.Contains(one.Query, ban) {
					t.Errorf("select.one %q must not contain %q on %s", one.Query, ban, tc.dialect)
				}
			}

			// The two paging arguments are the trailing ones (any WHERE args
			// precede them); their order is dialect-specific and load-bearing.
			if n := len(list.Args); n < 2 {
				t.Fatalf("select.list carried %d args, want at least 2", n)
			}
			tail := list.Args[len(list.Args)-2:]
			for i, want := range tc.wantListArgs {
				if tail[i] != want {
					t.Errorf("select.list paging args = %v, want %v (offset/fetch order per dialect)", tail, tc.wantListArgs)
					break
				}
			}
		})
	}
}
