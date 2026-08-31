// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cache

// The framework links no database driver: each ships as its own module
// (ADR-031). These tests open databases — the live matrix in CI reaches real
// PostgreSQL, MySQL, SQL Server and Oracle — so the test binary links them the
// way an application would, through the shared predicates, so this file and
// the drivers/ modules cannot drift apart.
import "github.com/jcsvwinston/nucleus/internal/dbclassify"

func init() { dbclassify.RegisterAll() }
