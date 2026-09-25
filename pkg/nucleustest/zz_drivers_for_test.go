// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

// The CI database matrix hands the tests a PostgreSQL or a MySQL URL; both
// drivers are already dependencies of the root module (pkg/db classifies
// their errors), and this registers them for the tests only.
import (
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)
