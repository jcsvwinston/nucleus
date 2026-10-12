// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package main

// The framework links no database driver: each ships as its own module so an
// application pays only for the engine it uses (ADR-031). The CLI is the one
// place where linking every engine is right — it is a tool people install
// once and point at whatever database they have, and `nucleus migrate` has to
// work against the database in front of it without a rebuild.
//
// It links them the way an application does: by importing the five driver
// modules, each of which registers its database/sql driver and its
// unique-violation classifier. That is possible because this binary is a
// module of its own (cmd/nucleus/go.mod, A12 N1): the driver modules require
// the framework, and the framework does not require them, so neither the
// engines nor anything the CLI alone needs reaches the go.mod an application
// inherits.

import (
	_ "github.com/jcsvwinston/nucleus/drivers/mssql"
	_ "github.com/jcsvwinston/nucleus/drivers/mysql"
	_ "github.com/jcsvwinston/nucleus/drivers/oracle"
	_ "github.com/jcsvwinston/nucleus/drivers/postgres"
	_ "github.com/jcsvwinston/nucleus/drivers/sqlite"
)
