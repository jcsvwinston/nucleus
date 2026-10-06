// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

// The framework links no database driver (ADR-031). The unit tests run on
// SQLite, linked through internal/testsqlite; the live matrix lane links
// PostgreSQL and MySQL on top with scripts/ci/test_with_engines.sh, the way
// pkg/accounts and pkg/outbox do (NU-106).
import "github.com/jcsvwinston/nucleus/internal/testsqlite"

func init() { testsqlite.Register() }
