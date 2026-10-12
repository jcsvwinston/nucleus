// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cache

// The framework links no database driver: each ships as its own module
// (ADR-031). These tests open SQLite databases, so the test binary links
// SQLite and its classifier through internal/testsqlite. The lanes that run
// them against PostgreSQL, MySQL, SQL Server and Oracle link those engines on
// top with scripts/ci/test_with_engines.sh, which is why the root go.mod
// requires no engine but SQLite (NU-106).
import "github.com/jcsvwinston/nucleus/internal/testsqlite"

func init() { testsqlite.Register() }
