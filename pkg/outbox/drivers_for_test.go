// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package outbox

// The framework links no database driver (ADR-031), and this package's tests
// run against every engine the store claims to speak: SQLite in the unit
// tests, and PostgreSQL or MySQL in the live matrix lane. The TEST binary
// links them, with their classifiers, through internal/alldrivers.
//
// Without it, TestSQLMatrix_Outbox fails against both engines with the
// framework's own guidance ("import _ .../drivers/postgres") — which is the
// right error for an application and the wrong one for the test that exists
// to exercise the engine. pkg/accounts carries the same file for the same
// reason; this one was missing because until now no lane ran pkg/outbox
// against a real engine.
import "github.com/jcsvwinston/nucleus/internal/alldrivers"

func init() { alldrivers.RegisterAll() }
