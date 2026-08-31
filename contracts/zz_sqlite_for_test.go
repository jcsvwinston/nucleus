// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package contracts

// The framework links no database driver: each ships as its own module
// (ADR-031). These tests open SQLite, so the driver has to be linked into the
// test binary — the same blank import an application writes, except that it
// names the driver package directly rather than the nucleus module that wraps
// it, because a module in this repo cannot require a module that requires it.
import _ "modernc.org/sqlite"
