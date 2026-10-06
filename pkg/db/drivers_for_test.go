// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package db

// The framework links no database driver (ADR-031). The unit tests run on
// SQLite, linked with its classifier through internal/testsqlite. The live DB
// matrix in CI connects to real PostgreSQL, MySQL, SQL Server and Oracle, and
// links those engines on top through their driver modules with
// scripts/ci/test_with_engines.sh — so the root go.mod requires no engine but
// SQLite (NU-106).
import "github.com/jcsvwinston/nucleus/internal/testsqlite"

func init() { testsqlite.Register() }
