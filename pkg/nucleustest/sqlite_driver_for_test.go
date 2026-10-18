// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest_test

// The kit links no database driver (DEP-2026-019), so its own tests link
// one, the way a consumer's test package does. This is the bare driver, not
// drivers/sqlite or internal/testsqlite: those register SQLite's
// unique-violation classifier too, and TestKitSQLiteClassifiesUniqueViolations
// measures that the kit supplies the classifier when the driver came without
// one.
import _ "modernc.org/sqlite"
