// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

func TestRegistersTheSQLDriver(t *testing.T) {
	if !slices.Contains(sql.Drivers(), "pgx") {
		t.Errorf("importing this module must register the \"pgx\" driver; registered: %v", sql.Drivers())
	}
}

// pgError stands in for any PostgreSQL driver error: the SQLSTATE reaches
// pkg/db through this method, which is why this module registers no
// classifier.
type pgError struct{ code string }

func (e *pgError) Error() string    { return "SQLSTATE " + e.code }
func (e *pgError) SQLState() string { return e.code }

// This module deliberately registers NO classifier, and that is worth a test
// rather than a comment: if someone "fixes" the apparent omission by adding
// one, PostgreSQL errors would be claimed twice, and the added classifier —
// necessarily typed to one driver — would silently stop covering lib/pq.
func TestClassificationNeedsNoRegistration(t *testing.T) {
	if driver.HasEngine("pgx") {
		t.Error("this module must not register a classifier: pkg/db reads the SQLSTATE through the SQLState() method, which works for every PostgreSQL driver")
	}
	if !db.IsUniqueViolation(&pgError{code: "23505"}) {
		t.Error("23505 must classify as a unique violation without any registration")
	}
	if db.IsUniqueViolation(&pgError{code: "23503"}) {
		t.Error("23503 is a foreign-key violation and must not classify as unique")
	}
}
