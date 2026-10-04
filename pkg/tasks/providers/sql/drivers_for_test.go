// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

// The framework links no database driver (ADR-031); the TEST binary links them,
// with their classifiers, through internal/alldrivers, the way pkg/accounts and
// pkg/outbox do.
import "github.com/jcsvwinston/nucleus/internal/alldrivers"

func init() { alldrivers.RegisterAll() }
