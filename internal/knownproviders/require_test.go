// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package knownproviders

import (
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"
)

type nopDriver struct{}

func (nopDriver) Open(string) (driver.Conn, error) { return nil, nil }

// A driver the framework publishes as a module is reported as a module not
// imported, with the install recipe; one it does not know is a missing
// registration; one that is registered passes.
func TestRequireDBDriver_NamesTheModuleOrTheRegistration(t *testing.T) {
	err := RequireDBDriver("pgx")
	if err == nil {
		t.Fatal("pgx is not registered in this test binary and must be refused")
	}
	for _, want := range []string{"ships as its own module", "drivers/postgres", "import _", "linked right now"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}

	err = RequireDBDriver("cockroach")
	if err == nil || !strings.Contains(err.Error(), `no database/sql driver is registered under "cockroach"`) {
		t.Fatalf("a driver the framework does not publish is a missing registration, got %v", err)
	}

	sql.Register("knownproviders-require-probe", nopDriver{})
	if err := RequireDBDriver("knownproviders-require-probe"); err != nil {
		t.Fatalf("a registered driver must pass: %v", err)
	}
}
