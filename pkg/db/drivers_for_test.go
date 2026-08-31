// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package db

// The framework links no database driver (ADR-031). The tests run against
// every engine — the live DB matrix in CI connects to real PostgreSQL, MySQL,
// SQL Server and Oracle — so the TEST binary links them all, through the same
// predicates the driver modules register, so this file and those modules
// cannot drift apart.
import "github.com/jcsvwinston/nucleus/internal/dbclassify"

func init() { dbclassify.RegisterAll() }
