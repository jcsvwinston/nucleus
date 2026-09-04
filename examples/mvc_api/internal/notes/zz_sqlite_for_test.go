// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package notes

// The framework links no database driver: each ships as its own module
//. These tests open a SQLite database, so the test binary links
// the driver the way the application in main.go does — through the
// published module, which also registers the unique-violation classifier.
import _ "github.com/jcsvwinston/nucleus/drivers/sqlite"
