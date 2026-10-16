// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authz

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DEP-2026-003 pins what a policy file in the legacy 3-column form does
// today, measured on 2026-10-16 with casbin v2.135.0: it does NOT load.
// The notice, written at v0.7.0, described rows that loaded and went inert;
// the deny-override model has four fields and the dependency now refuses a
// three-field row at load time, so the inert phase ended with that upgrade.
// What v1 adds is the way out in the error: authz.New names the migrator
// and the notice. At v2.0.0 the notice closes as `removed` and nothing
// else changes (D2, 2026-10-16); the 2.0 PR keeps this test as is.
func TestLegacyThreeColumnRows_RefusedWithTheMigratorNamed(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "rbac.csv")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Run("three columns are refused at load, naming the migrator", func(t *testing.T) {
		_, err := New(slog.Default(), write(t, "p, alice, /data, read\n"))
		if err == nil {
			t.Fatal("New loaded a 3-column policy file; casbin v2.135.0 refuses it, and this test pins that the refusal names the way out")
		}
		for _, want := range []string{"invalid policy rule size", "MigrateCSVPolicyFile", "DEP-2026-003"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %s", err, want)
			}
		}
	})
	t.Run("four columns grant", func(t *testing.T) {
		enf, err := New(slog.Default(), write(t, "p, alice, /data, read, allow\n"))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if !enf.Can("alice", "/data", "read") {
			t.Fatal("a 4-column allow row must grant")
		}
	})
}
