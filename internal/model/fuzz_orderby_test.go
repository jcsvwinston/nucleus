package model

import (
	"strings"
	"testing"
)

// fuzzOrderRec is the model the order-by allow-list is built from: a string
// column, a snake_case column, an explicit column override and the synthetic
// primary key.
type fuzzOrderRec struct {
	ID        int64  `db:"pk"`
	Email     string `db:"column:email"`
	Name      string
	CreatedAt string `db:"column:created_at"`
}

// FuzzSanitizeOrderBy fuzzes an ORDER BY expression. This is the one string a
// caller hands the CRUD layer that ends up in the SQL text rather than in a
// parameter: ListOptions.OrderBy comes from a query parameter in the admin
// API and in any handler that lets a user sort a table, and SanitizeOrderBy is
// the injection barrier — ADR-011 is explicit that the barrier is the
// allow-list, not quoting.
//
// The properties are what "allow-list" has to mean, checked against the output
// rather than against a list of known-bad inputs:
//
//   - Every clause that survives is one of the model's own columns followed by
//     exactly "asc" or "desc". Nothing the caller wrote reaches the clause: not
//     a comment, not a second statement, not a parenthesised subquery, not a
//     column of some other table. A hand-written deny-list can be walked
//     around; this cannot.
//   - The clause count is preserved. A malformed clause is a hard error, never
//     a silently dropped one, so an injection attempt fails loud instead of
//     degrading to the default ordering.
//   - The result is a fixed point: sanitising it again returns it unchanged.
//     That is the round trip this function has, and it is what lets a caller
//     store a sanitised clause and re-validate it later.
//   - An error yields the empty string, so a caller that ignores the error
//     cannot end up splicing a half-validated clause into a query.
//
// Seeds: every case from TestSanitizeOrderBy_SharedAllowList and
// TestCRUD_SanitizeOrderBy_RejectsInjection — the injections that were tried
// (statement terminators, comment markers, a subquery, `1=1`, `email/**/desc`)
// and the empty-clause shapes that used to be dropped silently. The injections
// are committed as named corpus files in testdata/fuzz/FuzzSanitizeOrderBy.
func FuzzSanitizeOrderBy(f *testing.F) {
	meta, err := ExtractMeta(&fuzzOrderRec{})
	if err != nil {
		f.Fatalf("ExtractMeta: %v", err)
	}
	allowed := map[string]bool{"id": true, "email": true, "name": true, "created_at": true}

	seeds := []string{
		"",
		"   ",
		"email",
		"email desc",
		"email ASC",
		"email asc, name desc",
		"name desc, email asc",
		"created_at desc",
		"id",
		"ssn",
		"secret",
		"email sideways",
		"email asc desc",
		"email, , name",
		"email,",
		",email",
		"email,,id",
		"name, secret",
		"1=1",
		"email/**/desc",
		"name; DROP TABLE recs",
		"id; DROP TABLE test_users --",
		"email; DELETE FROM test_users",
		"(SELECT password FROM admins)",
		"EMAIL DESC",
		"email\tdesc",
		"email\ndesc",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		got, err := SanitizeOrderBy(meta, raw)
		if err != nil {
			if got != "" {
				t.Fatalf("SanitizeOrderBy(%q) returned both an error and a clause %q", raw, got)
			}
			return
		}
		if strings.TrimSpace(raw) == "" {
			if got != "" {
				t.Fatalf("SanitizeOrderBy(%q) invented a clause %q from blank input", raw, got)
			}
			return
		}

		clauses := strings.Split(got, ", ")
		if n := len(strings.Split(raw, ",")); len(clauses) != n {
			t.Fatalf("SanitizeOrderBy(%q) = %q: %d clauses out of %d in; a clause must be rejected, never dropped",
				raw, got, len(clauses), n)
		}
		for _, clause := range clauses {
			col, dir, ok := strings.Cut(clause, " ")
			if !ok {
				t.Fatalf("SanitizeOrderBy(%q) = %q: clause %q is not \"<column> <direction>\"", raw, got, clause)
			}
			if !allowed[col] {
				t.Fatalf("SanitizeOrderBy(%q) = %q: %q is not a column of the model; caller input reached the SQL text",
					raw, got, col)
			}
			if dir != "asc" && dir != "desc" {
				t.Fatalf("SanitizeOrderBy(%q) = %q: %q is not a canonical direction", raw, got, dir)
			}
		}

		again, err := SanitizeOrderBy(meta, got)
		if err != nil {
			t.Fatalf("SanitizeOrderBy rejected its own output %q (from %q): %v", got, raw, err)
		}
		if again != got {
			t.Fatalf("SanitizeOrderBy is not a fixed point: %q -> %q -> %q", raw, got, again)
		}
	})
}
