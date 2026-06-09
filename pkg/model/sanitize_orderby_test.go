package model

import "testing"

// TestSanitizeOrderBy_SharedAllowList exercises the exported SanitizeOrderBy
// that the CRUD layer and the admin API now both delegate to (audit LOW-B),
// including the multi-column support the admin gained from the consolidation.
func TestSanitizeOrderBy_SharedAllowList(t *testing.T) {
	type rec struct {
		ID    int    `db:"pk"`
		Email string `db:"column:email"`
		Name  string `db:"column:name"`
	}
	meta, err := ExtractMeta(&rec{})
	if err != nil {
		t.Fatalf("ExtractMeta: %v", err)
	}

	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"email", "email asc", false},
		{"email desc", "email desc", false},
		{"email asc, name desc", "email asc, name desc", false}, // multi-column
		{"id", "id asc", false},                                 // synthetic PK
		{"ssn", "", true},                                       // unknown column
		{"email sideways", "", true},                            // bad direction
		{"email, , name", "", true},                             // empty clause
		{"name; DROP TABLE recs", "", true},                     // injection attempt
	}
	for _, tc := range cases {
		got, err := SanitizeOrderBy(meta, tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("SanitizeOrderBy(%q): expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("SanitizeOrderBy(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("SanitizeOrderBy(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if _, err := SanitizeOrderBy(nil, "email"); err == nil {
		t.Error("expected a defensive error for nil meta, not a panic")
	}
}
