// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package postgres links the PostgreSQL driver into a Nucleus application.
//
// Import it for its side effect, once, anywhere in the program:
//
//	import _ "github.com/jcsvwinston/nucleus/drivers/postgres"
//
// It registers pgx/v5 under the name "pgx", which is what pkg/db resolves a
// postgres:// or postgresql:// URL to.
//
// Unlike the other driver modules this one registers no classifier, and that
// is deliberate rather than an omission: pkg/db reads a PostgreSQL SQLSTATE
// through the `SQLState() string` method every PostgreSQL driver exposes, so
// unique-violation detection already works here — for lib/pq too — without
// naming a driver type.
package postgres

import _ "github.com/jackc/pgx/v5/stdlib"
