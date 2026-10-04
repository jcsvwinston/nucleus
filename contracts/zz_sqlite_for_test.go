// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package contracts

// The framework links no database driver: each ships as its own module
// (ADR-031). These tests open databases — the live matrix in CI reaches real
// PostgreSQL, MySQL, SQL Server and Oracle — so the test binary links every
// engine and its classifier through internal/alldrivers, which nothing but
// the CLI and the test binaries imports.
import "github.com/jcsvwinston/nucleus/internal/alldrivers"

func init() { alldrivers.RegisterAll() }
