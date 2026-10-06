// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package accounts

// The framework links no database driver (ADR-031), and this package's
// tests run against every engine the store claims to speak: SQLite in the
// unit tests, linked here through internal/testsqlite, and PostgreSQL or
// MySQL in the live matrix lane, which links those engines on top through
// their driver modules with scripts/ci/test_with_engines.sh (NU-106).
//
// Run without that script, TestSQLMatrix_AccountsStore fails against MySQL
// with the framework's own guidance ("import _ .../drivers/mysql") — the
// right error for an application, and a loud one for a lane that forgot to
// link the engine it exists to exercise.
import "github.com/jcsvwinston/nucleus/internal/testsqlite"

func init() { testsqlite.Register() }
