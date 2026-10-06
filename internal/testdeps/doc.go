// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package testdeps is the root of a module that holds what the framework's
// tests need and the framework's go.mod must not list.
//
// A module's go.mod lists every module its own tests import, and an
// application that requires the framework inherits that list in its build
// graph. Four database engines and miniredis were in it for the tests alone
// (NU-106, A12 N1). What needs them lives here instead:
//
//   - cmd/miniredis, the Redis server internal/testredis starts for the
//     framework's tests, one process per server;
//   - dbclassify, which holds the framework's engine-free classifiers to each
//     engine's real error type;
//   - drivergraph, which asserts each driver module links its own engine and
//     no other (NU-8).
//
// The module is never published: it sits under internal/, so no code outside
// this repository can import it, and its go.mod points at the framework in
// this tree. Its path, under the framework's, is what lets these tests reach
// the framework's internal packages. CI runs it in the test lane, and
// `make test` runs it locally.
package testdeps
