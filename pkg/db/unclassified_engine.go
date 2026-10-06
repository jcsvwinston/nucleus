// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"github.com/jcsvwinston/nucleus/pkg/db/driver"
)

// A missing classifier does not fail: IsUniqueViolation answers false, and
// the branch that turns "that email is taken" into a 409 never runs on that
// engine. Nothing else in the process notices, so the only place left to say
// so is the log, at the moment the database is opened — before the first
// duplicate key arrives under load and is answered as an internal error.

// hasClassifier is driver.HasEngine, held in a variable so a test can stand
// in for a registry that lacks an engine its own binary links.
var hasClassifier = driver.HasEngine

// warnedUnclassified holds the engines warnUnclassifiedEngine has already
// warned about. The gap is a property of the binary, not of one connection,
// so a process that opens several databases on one engine — two aliases, a
// test binary that boots an application per test — is told once.
var warnedUnclassified sync.Map

// classifierCovers reports whether IsUniqueViolation can recognise the errors
// of a database whose URL selects the database/sql driver engine — "pgx",
// "mysql", "sqlite", "sqlserver" or "oracle", the names classifiers are
// registered under — opened through driverName, the Config.DriverName that
// replaces it ("" when nothing does).
//
// PostgreSQL needs no classifier: IsUniqueViolation reads its SQLSTATE through
// the method every PostgreSQL driver exposes. Any other engine is covered by a
// classifier registered under its stock driver name, which is where the
// driver modules register it. That also covers a wrapping driver — one that
// instruments, or the test kit's per-test transactional driver — because a
// wrapper hands back the errors of the driver it wraps. A classifier
// registered under driverName itself covers a replacement driver whose
// importer classified it under the name it opens with.
func classifierCovers(engine, driverName string) bool {
	if engine == "pgx" {
		return true
	}
	if hasClassifier(engine) {
		return true
	}
	return driverName != "" && driverName != engine && hasClassifier(driverName)
}

// warnUnclassifiedEngine logs, once per engine and process, that
// IsUniqueViolation cannot recognise the errors of the database New has just
// opened. engine is the driver name the URL's scheme selects (resolveDriver)
// and driverName the Config.DriverName that replaced it, if any.
//
// A nil logger falls back to slog.Default: a caller that passed none did not
// ask for silence, and this line is the only signal the gap has.
func warnUnclassifiedEngine(logger *slog.Logger, engine, driverName string) {
	if engine == "" || classifierCovers(engine, driverName) {
		return
	}
	if _, warned := warnedUnclassified.LoadOrStore(engine, struct{}{}); warned {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	attrs := []any{"engine", engine}
	if driverName != "" && driverName != engine {
		attrs = append(attrs, "driver", driverName)
	}
	attrs = append(attrs,
		"fix", unclassifiedEngineFix(engine),
		"why", "db.IsUniqueViolation answers false for every duplicate key on this engine, so a branch that turns \"that value is taken\" into a 409 never runs and the request fails as an internal error",
	)
	logger.Warn("db: no unique-violation classifier is registered for this engine; db.IsUniqueViolation cannot recognise its errors", attrs...)
}

// unclassifiedEngineFix is the instruction the warning carries: the nucleus
// driver module for an engine this project publishes — it registers the
// driver and its classifier together — and, for any engine, the registration
// a caller who imports the driver directly can make instead.
func unclassifiedEngineFix(engine string) string {
	register := fmt.Sprintf("driver.RegisterUniqueViolation(%q, fn) from %s/pkg/db/driver, with fn matching the code the driver reports",
		engine, knownproviders.RepoModule)
	if p, ours := knownproviders.DBDriver(engine); ours {
		return fmt.Sprintf("import _ %q (or run `nucleus add %s`), which registers the driver together with its classifier; or, keeping the driver you import yourself, register one with %s",
			p.ImportPath(), p.Name, register)
	}
	return "register one for the driver you import with " + register
}
