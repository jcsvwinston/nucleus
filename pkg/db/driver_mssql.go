//go:build mssql

// Package db: MSSQL driver registration.
// Activate with: go build -tags mssql
package db

import _ "github.com/microsoft/go-mssqldb"
