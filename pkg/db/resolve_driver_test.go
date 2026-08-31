package db

import (
	"strings"
	"testing"
)

// AN-04: a typo in the URL scheme ("sqlit://app.db") used to fall through the
// scheme switch into the ".db suffix ⇒ SQLite file path" branch, so SQLite
// tried to open a file literally named "sqlit://app.db" and died at Ping with
// "unable to open database file: out of memory (14)" — a message with zero
// relation to the actual mistake. The suffix branch is for bare paths only; a
// string that carries a scheme separator must resolve through the scheme
// switch or fail naming the scheme.
func TestResolveDriverRejectsSchemeTypoWithClearError(t *testing.T) {
	cases := []string{
		"sqlit://app.db",
		"sqlite3://app.db",
		"postgres//app.sqlite", // mangled separator + sqlite suffix
	}
	for _, rawURL := range cases {
		_, _, err := resolveDriver(rawURL)
		if err == nil {
			t.Fatalf("resolveDriver(%q) = nil error; want unsupported-scheme error", rawURL)
		}
		msg := err.Error()
		if !strings.Contains(msg, rawURL) {
			t.Errorf("resolveDriver(%q) error does not name the URL: %s", rawURL, msg)
		}
		if !strings.Contains(msg, "sqlite://") {
			t.Errorf("resolveDriver(%q) error does not list the supported schemes: %s", rawURL, msg)
		}
	}
}

// The legitimate spellings the suffix branch exists for keep working.
func TestResolveDriverBarePathsStillResolveToSQLite(t *testing.T) {
	for _, rawURL := range []string{"app.db", "data/app.sqlite", ":memory:", "sqlite://app.db"} {
		driverName, _, err := resolveDriver(rawURL)
		if err != nil {
			t.Fatalf("resolveDriver(%q) error: %v", rawURL, err)
		}
		if driverName != "sqlite" {
			t.Fatalf("resolveDriver(%q) driver = %q; want sqlite", rawURL, driverName)
		}
	}
}
