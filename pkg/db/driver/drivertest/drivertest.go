// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package drivertest is a conformance kit for database driver modules.
//
// A driver module registers a UniqueViolationFunc, and pkg/db consults every
// registered classifier in turn. That design has one sharp edge: a classifier
// that answers true for an error it did not produce answers for ANOTHER
// engine, and the bug shows up as a wrong HTTP status on a deployment the
// author never tests. The checks here pin the properties that make consulting
// classifiers in turn safe.
//
// A driver module runs it from its own test:
//
//	func TestConformance(t *testing.T) {
//	    drivertest.VerifyClassifier(t, drivertest.Case{
//	        Engine:    "mysql",
//	        Classify:  isUniqueViolation,
//	        Violation: &gomysql.MySQLError{Number: 1062},
//	        NotViolation: []error{&gomysql.MySQLError{Number: 1452}},
//	    })
//	}
//
// Like backendtest for authentication backends (ADR-027), this exists so the
// contract is verified by the same suite everywhere rather than re-derived,
// approximately, in each module.
package drivertest

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

// Case describes one driver's classifier and the errors it must and must not
// claim.
type Case struct {
	// Engine is the database/sql driver name the module registers under.
	Engine string

	// Classify is the function the module passes to
	// driver.RegisterUniqueViolation.
	Classify driver.UniqueViolationFunc

	// Violation is an error the driver produces for a unique or primary-key
	// constraint. Prefer one obtained from the real driver; fabricate only
	// when the driver's error type can be constructed honestly.
	Violation error

	// NotViolation lists errors from the SAME driver that are constraint
	// failures of another kind — a foreign key, a NOT NULL. These are the
	// cases that fail when a classifier is widened to "any constraint
	// error", which would make a caller blame the wrong field.
	NotViolation []error
}

// sqlStater mimics the shape every PostgreSQL driver exposes. pkg/db reads a
// PostgreSQL SQLSTATE through this method and does so BEFORE consulting the
// registry, so a classifier that claimed such an error would be shadowing an
// engine it knows nothing about.
type sqlStater struct{ code string }

func (e *sqlStater) Error() string    { return "SQLSTATE " + e.code }
func (e *sqlStater) SQLState() string { return e.code }

// VerifyClassifier runs the conformance checks against one driver's
// classifier.
func VerifyClassifier(t *testing.T, c Case) {
	t.Helper()

	if c.Engine == "" || c.Classify == nil || c.Violation == nil {
		t.Fatal("drivertest: Engine, Classify and Violation are required")
	}

	t.Run("recognises its own violation", func(t *testing.T) {
		if !c.Classify(c.Violation) {
			t.Errorf("classifier did not recognise %v", c.Violation)
		}
	})

	t.Run("recognises it through a wrap", func(t *testing.T) {
		// Callers wrap. A classifier that type-asserts instead of using
		// errors.As passes the check above and fails here, which is the
		// shape this whole kit exists to catch.
		if !c.Classify(fmt.Errorf("insert user: %w", c.Violation)) {
			t.Errorf("classifier did not recognise a WRAPPED %v — use errors.As, not a type assertion", c.Violation)
		}
	})

	t.Run("rejects other constraint failures", func(t *testing.T) {
		for _, err := range c.NotViolation {
			if c.Classify(err) {
				t.Errorf("classifier claimed %v as a unique violation; a caller acting on \"unique\" would point at the wrong field", err)
			}
		}
	})

	t.Run("claims nothing that is not its own", func(t *testing.T) {
		// Classifiers are consulted in turn, so one that answers for a
		// foreign error answers for another engine.
		for _, err := range []error{
			nil,
			errors.New("connection refused"),
			&sqlStater{code: "23505"},
			fmt.Errorf("wrapped: %w", &sqlStater{code: "23505"}),
		} {
			if c.Classify(err) {
				t.Errorf("classifier claimed a foreign error (%v); it must answer only for its own driver's error type", err)
			}
		}
	})

	t.Run("is registered under its engine", func(t *testing.T) {
		// The classifier being correct is worth nothing if the module's
		// init() never registered it — the failure mode then is
		// IsUniqueViolation quietly answering false.
		if !driver.HasEngine(c.Engine) {
			t.Errorf("engine %q has no registered classifier; registered: %v", c.Engine, driver.RegisteredEngines())
		}
	})
}
