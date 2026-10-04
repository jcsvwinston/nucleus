// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package dbclassify recognises a unique-constraint violation from MySQL,
// SQL Server, Oracle and SQLite WITHOUT importing any of them.
//
// Each driver module under drivers/ carries its own predicate, typed to its
// own engine's error, and registers it next to the driver, so an application
// that imports drivers/sqlite links SQLite and no other engine (NU-8). This
// package is what the ROOT module keeps in their place, for two consumers that
// cannot import the driver modules — those require this module, so the
// requirement would be circular:
//
//   - internal/alldrivers, which the `nucleus` CLI and the framework's test
//     binaries link to reach every engine. Registering these predicates there
//     means the DB matrix lanes exercise them against real servers.
//   - the driver modules published up to v0.1.7, which import this package by
//     name. An application that upgrades the framework and not its driver
//     module compiles them against this copy, so the four functions keep their
//     names and signatures — and since this package imports no engine, those
//     applications also stop linking the four engines they never chose.
//
// Importing it costs nothing: it reaches the standard library only, and a test
// asserts that. It recognises an engine's error by the identity of its type —
// import path and name — and reads the code through the method or the exported
// field the driver documents, which is what errors.As against the typed error
// does, without the import. The tests check every predicate against the
// engine's real error type, so a driver that renames its type or its code
// field turns them red instead of turning a predicate into a silent false.
package dbclassify

import (
	"reflect"
)

// errorType names a driver's error type without importing it.
type errorType struct {
	pkg, name string
	pointer   bool // the driver returns *T, not T
}

var (
	mysqlError  = errorType{pkg: "github.com/go-sql-driver/mysql", name: "MySQLError", pointer: true}
	mssqlError  = errorType{pkg: "github.com/microsoft/go-mssqldb", name: "Error"}
	oracleError = errorType{pkg: "github.com/sijms/go-ora/v2/network", name: "OracleError", pointer: true}
	sqliteError = errorType{pkg: "modernc.org/sqlite", name: "Error", pointer: true}
)

// find returns the first error in err's tree whose dynamic type is t, walking
// the tree the way errors.As does: depth first, through Unwrap() error and
// Unwrap() []error. An As method is not consulted; none of these drivers'
// errors, nor the wrappers they return, has one.
func (t errorType) find(err error) (error, bool) {
	for err != nil {
		if t.is(err) {
			return err, true
		}
		switch u := err.(type) {
		case interface{ Unwrap() error }:
			err = u.Unwrap()
		case interface{ Unwrap() []error }:
			for _, e := range u.Unwrap() {
				if found, ok := t.find(e); ok {
					return found, true
				}
			}
			return nil, false
		default:
			return nil, false
		}
	}
	return nil, false
}

func (t errorType) is(err error) bool {
	v := reflect.ValueOf(err)
	typ := v.Type()
	if t.pointer {
		// A nil *T inside a non-nil error has no code to read; errors.As
		// would hand it over and the typed predicate would dereference it.
		if typ.Kind() != reflect.Pointer || v.IsNil() {
			return false
		}
		typ = typ.Elem()
	}
	return typ.Kind() == reflect.Struct && typ.PkgPath() == t.pkg && typ.Name() == t.name
}

// field reads an exported integer field of the struct behind err, and reports
// false when the driver's type no longer has it.
func field(err error, name string) (int64, bool) {
	v := reflect.Indirect(reflect.ValueOf(err))
	f := v.FieldByName(name)
	switch {
	case !f.IsValid():
		return 0, false
	case f.CanInt():
		return f.Int(), true
	case f.CanUint():
		return int64(f.Uint()), true
	}
	return 0, false
}

// MySQLUniqueViolation matches error 1062 (ER_DUP_ENTRY) on the CODE, not on
// the message: a server running with lc_messages set to another language
// answers the same rejection in that language, and a substring check would
// silently return false there.
func MySQLUniqueViolation(err error) bool {
	e, ok := mysqlError.find(err)
	if !ok {
		return false
	}
	n, ok := field(e, "Number")
	return ok && n == 1062
}

// MSSQLUniqueViolation matches 2627 (unique/primary-key CONSTRAINT) and 2601
// (duplicate row in a unique INDEX). Both exist because the engine raises a
// different number depending on how uniqueness was declared, and a caller
// cares about neither distinction.
//
// go-mssqldb returns its Error as a VALUE (the methods have value receivers),
// so a *Error is not the driver's error and does not match — the same rule
// errors.As applies to a value target. The number is read through
// SQLErrorNumber, which the driver documents for exactly this: checking an
// error without importing the package.
func MSSQLUniqueViolation(err error) bool {
	e, ok := mssqlError.find(err)
	if !ok {
		return false
	}
	num, ok := e.(interface{ SQLErrorNumber() int32 })
	if !ok {
		return false
	}
	n := num.SQLErrorNumber()
	return n == 2627 || n == 2601
}

// OracleUniqueViolation matches ORA-00001 ("unique constraint violated").
//
// go-ora/v2 returns a *network.OracleError; the walk follows Unwrap, so one
// that a caller wrapped still classifies.
func OracleUniqueViolation(err error) bool {
	e, ok := oracleError.find(err)
	if !ok {
		return false
	}
	n, ok := field(e, "ErrCode")
	return ok && n == 1
}

// SQLiteUniqueViolation matches SQLite's extended result codes: 2067 is
// SQLITE_CONSTRAINT_UNIQUE and 1555 SQLITE_CONSTRAINT_PRIMARYKEY. Both mean
// "that value is already taken"; the primary-key code is separate and would
// be missed by a check that only looked for the unique one.
func SQLiteUniqueViolation(err error) bool {
	e, ok := sqliteError.find(err)
	if !ok {
		return false
	}
	c, ok := e.(interface{ Code() int })
	if !ok {
		return false
	}
	code := c.Code()
	return code == 2067 || code == 1555
}
