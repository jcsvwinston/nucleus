package model

import (
	"context"
	"testing"
)

// TestSanitizeOrderBy_AllowsKnownColumns confirms valid ORDER BY clauses survive
// validation and are rebuilt with a canonical direction.
func TestSanitizeOrderBy_AllowsKnownColumns(t *testing.T) {
	meta, err := ExtractMeta(&TestUser{})
	if err != nil {
		t.Fatalf("ExtractMeta: %v", err)
	}
	c := NewCRUD(nil, meta, nil) // sanitizeOrderBy needs only meta, not a live DB

	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"email", "email asc"},
		{"email desc", "email desc"},
		{"email ASC", "email asc"},
		{"name desc, email asc", "name desc, email asc"},
		{"created_at desc", "created_at desc"},
		{"id", "id asc"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := c.sanitizeOrderBy(tc.in)
			if err != nil {
				t.Errorf("sanitizeOrderBy(%q) unexpected error: %v", tc.in, err)
				return
			}
			if got != tc.want {
				t.Errorf("sanitizeOrderBy(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSanitizeOrderBy_RejectsInjectionAndUnknown is the core SEC guard: any
// input that is not a known column + asc/desc is rejected, so nothing
// attacker-controlled can reach the ORDER BY position of the query string.
func TestSanitizeOrderBy_RejectsInjectionAndUnknown(t *testing.T) {
	meta, err := ExtractMeta(&TestUser{})
	if err != nil {
		t.Fatalf("ExtractMeta: %v", err)
	}
	c := NewCRUD(nil, meta, nil)

	bad := []string{
		"id; DROP TABLE test_users --",
		"email; DELETE FROM test_users",
		"(SELECT password FROM admins)",
		"secret",         // unknown column
		"email sideways", // invalid direction
		"email asc desc", // too many tokens in one clause
		"1=1",            // not a column
		"email/**/desc",  // no whitespace → single unresolved token
		"name, secret",   // one good, one bad clause → whole input rejected
		"email,",         // trailing comma → empty clause, fail loud
		",email",         // leading comma → empty clause
		"email,,id",      // double comma → empty middle clause
	}
	for _, in := range bad {
		t.Run(in, func(t *testing.T) {
			if got, err := c.sanitizeOrderBy(in); err == nil {
				t.Errorf("sanitizeOrderBy(%q) accepted malicious/unknown input, returned %q", in, got)
			}
		})
	}
}

// TestFindAll_RejectsOrderByInjection is the end-to-end SEC guard: an injecting
// OrderBy is rejected before any SQL runs, and the table is left intact.
func TestFindAll_RejectsOrderByInjection(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, err := ExtractMeta(&TestUser{})
	if err != nil {
		t.Fatalf("ExtractMeta: %v", err)
	}
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(sqlDB, meta, nil)

	_, err = crud.FindAll(context.Background(), QueryOpts{
		Page: 1, PageSize: 10,
		OrderBy: "id; DROP TABLE test_users --",
	})
	if err == nil {
		t.Fatal("FindAll accepted an injecting OrderBy; expected a validation error")
	}

	// The injection must not have executed: the table is still queryable.
	if _, err := crud.FindAll(context.Background(), QueryOpts{Page: 1, PageSize: 10}); err != nil {
		t.Fatalf("test_users should be intact after a rejected injection: %v", err)
	}
}
