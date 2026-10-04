// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package main

// The framework links no database driver: each ships as its own module so an
// application pays only for the engine it uses (ADR-031). The CLI is the one
// place where linking every engine is right — it is a tool people install
// once and point at whatever database they have, and `nucleus migrate` has to
// work against the database in front of it without a rebuild.
//
// It cannot import the drivers/ modules: those import the framework, and this
// binary lives in the framework's module, so the requirement would be
// circular. internal/alldrivers links the drivers directly and registers the
// root's copy of their classifiers; this binary and the test binaries are the
// only things that import it.

import "github.com/jcsvwinston/nucleus/internal/alldrivers"

func init() { alldrivers.RegisterAll() }
