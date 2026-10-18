// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package nodriver is the consumer DEP-2026-019 describes: a test package
// that imports nucleustest and nothing that registers a database driver. It
// lives under testdata so `go test ./...` never builds it into a binary that
// does link one; TestNucleustestLinksNoDriver runs it in its own process.
package nodriver_test

import (
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

func TestTempSQLiteWithoutDriver(t *testing.T) {
	if slices.Contains(sql.Drivers(), "sqlite") {
		t.Fatalf("sql.Drivers() = %v: this binary links SQLite, so it measures nothing; something imported a driver", sql.Drivers())
	}

	// The configuration is the same with or without a driver.
	dbs := nucleustest.TempSQLite(t)
	url := dbs["default"].URL
	if !strings.HasPrefix(url, "sqlite://") || !strings.HasSuffix(url, "nucleustest.db") {
		t.Fatalf("TempSQLite default URL = %q, want sqlite://<temp dir>/nucleustest.db", url)
	}

	// Opening it is what fails, from pkg/db, naming the import to add.
	_, err := db.New(db.Config{DatabaseURL: url}, nil)
	if err == nil {
		t.Fatal("db.New opened a SQLite database with no \"sqlite\" driver registered")
	}
	for _, want := range []string{"not imported yet", `import _ "github.com/jcsvwinston/nucleus/drivers/sqlite"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not say %q:\n%v", want, err)
		}
	}
}
