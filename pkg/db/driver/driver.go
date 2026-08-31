// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package driver is the contract a database driver module implements to plug
// into pkg/db. It is a LEAF package: it imports nothing but the standard
// library, so a driver module can implement it without compiling the
// framework — the same reason pkg/storage/provider and pkg/auth/backend are
// leaves (ADR-025, ADR-026).
//
// A driver module does two things in its init(), and both matter:
//
//   - it registers the database/sql driver, so sql.Open finds it;
//   - it registers how that driver reports a unique-constraint violation.
//
// The second is the one that is easy to forget and expensive to omit. Without
// it, IsUniqueViolation does not fail — it answers false, and the branch that
// turns "that email is taken" into a 409 becomes dead code on that engine. A
// wrong answer is worse than a missing one, so a driver module that registers
// the driver without its classifier is a bug in the module, not a degraded
// mode of the framework.
package driver

import (
	"fmt"
	"sort"
	"sync"
)

// UniqueViolationFunc reports whether err — as produced by one specific
// driver — was caused by a unique or primary-key constraint.
//
// It must match on the CODE the driver reports, through errors.As, never on
// substrings of the message: PostgreSQL, MySQL, Oracle and SQL Server all
// translate their messages when the server runs in another language, so a
// substring check silently returns false on exactly the deployments where it
// matters. It must also walk the Unwrap chain, so a wrapped error still
// classifies.
//
// It must return false for an error that did not come from its own driver:
// classifiers are consulted in turn, and one that claims errors it does not
// recognise would answer for another engine.
type UniqueViolationFunc func(error) bool

var (
	mu          sync.RWMutex
	uniqueByEng = map[string]UniqueViolationFunc{}
)

// RegisterUniqueViolation records how the driver registered under engine —
// the database/sql driver name, e.g. "pgx", "mysql", "sqlite" — reports a
// unique-constraint violation.
//
// It returns an error rather than panicking on a duplicate so that a module
// linked twice through different paths fails loudly at init in the module's
// own code, where the import that caused it is visible.
func RegisterUniqueViolation(engine string, fn UniqueViolationFunc) error {
	if engine == "" {
		return fmt.Errorf("db/driver: engine name is required")
	}
	if fn == nil {
		return fmt.Errorf("db/driver: %s: classifier is required — registering the driver without one makes IsUniqueViolation answer false for this engine", engine)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := uniqueByEng[engine]; dup {
		return fmt.Errorf("db/driver: %s is already registered", engine)
	}
	uniqueByEng[engine] = fn
	return nil
}

// MustRegisterUniqueViolation is RegisterUniqueViolation for use in an
// init(), where there is no caller to hand an error back to.
func MustRegisterUniqueViolation(engine string, fn UniqueViolationFunc) {
	if err := RegisterUniqueViolation(engine, fn); err != nil {
		panic(err)
	}
}

// UniqueViolationFuncs returns the registered classifiers, in a stable order
// so that classification does not depend on map iteration.
func UniqueViolationFuncs() []UniqueViolationFunc {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]UniqueViolationFunc, 0, len(uniqueByEng))
	for _, e := range sortedKeysLocked() {
		out = append(out, uniqueByEng[e])
	}
	return out
}

// RegisteredEngines returns the engines that have a classifier, sorted. It is
// what an error message uses to tell an operator what IS linked in.
func RegisteredEngines() []string {
	mu.RLock()
	defer mu.RUnlock()
	return sortedKeysLocked()
}

// HasEngine reports whether engine registered a classifier.
func HasEngine(engine string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := uniqueByEng[engine]
	return ok
}

func sortedKeysLocked() []string {
	keys := make([]string, 0, len(uniqueByEng))
	for e := range uniqueByEng {
		keys = append(keys, e)
	}
	sort.Strings(keys)
	return keys
}
