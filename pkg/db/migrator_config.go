// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
)

// MigratorConfig is the input of NewMigratorFromConfig: where the migration
// files are, which database they apply to, and whether the ledger is scoped
// to a module.
type MigratorConfig struct {
	// DB is the database the migrations apply to. Up, Down, Steps, Status
	// and Drift need it; Create, which only writes files, does not.
	DB *DB

	// Dir is the directory the <id>.up.sql / <id>.down.sql files are read
	// from (and Create writes to). Exactly one of Dir and FS is set.
	Dir string

	// FS reads the migration files from the root of an fs.FS instead — an
	// embed.FS, through fs.Sub for a nested layout. Same flat naming as Dir.
	// Create is disk-only and returns an error on an FS-backed Migrator.
	FS fs.FS

	// Module, when set, scopes the ledger to a module: applied migrations
	// are recorded as "<Module>/<id>", so two modules' files with the same
	// name cannot collide on a shared database (ADR-010 §16). Empty records
	// the bare id, which is what `nucleus migrate` does for the
	// application's own migrations. It must not contain '/' (the namespace
	// separator) or NUL.
	Module string
}

// NewMigratorFromConfig creates a Migrator from cfg and returns an error for
// input it cannot work with: neither Dir nor FS set, both set, or a Module
// name with '/' or NUL in it. A nil logger logs nothing.
//
// It covers what NewMigrator, NewModuleMigrator and NewModuleFSMigrator do,
// with one difference: the last two panic on a bad module name, and this
// returns the error.
func NewMigratorFromConfig(cfg MigratorConfig, logger *slog.Logger) (*Migrator, error) {
	switch {
	case cfg.Dir == "" && cfg.FS == nil:
		return nil, errors.New("db.NewMigratorFromConfig: set Dir (a directory of migration files) or FS (an fs.FS holding them)")
	case cfg.Dir != "" && cfg.FS != nil:
		return nil, fmt.Errorf("db.NewMigratorFromConfig: Dir (%q) and FS are both set — a Migrator reads one source", cfg.Dir)
	}
	if strings.ContainsAny(cfg.Module, "/\x00") {
		return nil, fmt.Errorf("db.NewMigratorFromConfig: Module %q must not contain '/' or NUL", cfg.Module)
	}
	return &Migrator{
		db:             cfg.DB,
		migrationsPath: cfg.Dir,
		fsys:           cfg.FS,
		moduleName:     cfg.Module,
		logger:         logger,
	}, nil
}
