// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package accounts

// The framework links no database driver (ADR-031), and this package's
// tests run against every engine the store claims to speak: SQLite in the
// unit tests, and PostgreSQL or MySQL in the live matrix lane. The TEST
// binary links them through the same predicates the driver modules
// register, so this file and those modules cannot drift apart.
//
// Without it, TestSQLMatrix_AccountsStore fails against MySQL with the
// framework's own guidance ("import _ .../drivers/mysql") — which is the
// right error for an application and the wrong one for the test that
// exists to exercise the engine.
import "github.com/jcsvwinston/nucleus/internal/dbclassify"

func init() { dbclassify.RegisterAll() }
