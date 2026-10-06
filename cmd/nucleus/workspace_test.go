// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"testing"
)

// TestMain hands the commands under test an environment with no workspace.
// This module is built through one — it requires the framework at a release
// (ADR-038) — and `make test` passes it in GOWORK. Every project these tests
// generate is a module of its own, and a workspace handed down to the go
// commands they and the CLI run there (go test, go list) would claim it too.
func TestMain(m *testing.M) {
	if err := os.Setenv("GOWORK", "off"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
