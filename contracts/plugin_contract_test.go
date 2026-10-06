// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package contracts

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
)

// maxPluginContractDeps is the ceiling on third-party packages that the
// authentication contract may drag in.
//
// Two packages, from ONE module: the configuration decoder and its
// internal error type. The number is the point of the package, not a
// detail of it — the contract was extracted from pkg/auth precisely
// because implementing two methods used to cost 115 third-party packages
// (session stores, JWT, Redis, Prometheus, OpenTelemetry, the gRPC
// gateway), every one of them inherited by whoever wrote a backend.
//
// Raising this ceiling is a decision about what every plugin author pays,
// so it belongs in a review rather than in whatever import made it
// necessary.
const maxPluginContractDeps = 2

func TestPluginContract_StaysLight(t *testing.T) {
	for _, pkg := range []string{"../pkg/auth/backend", "../pkg/auth/federated", "../pkg/router/interceptor", "../pkg/storage/provider", "../pkg/auth/sessionstore"} {
		t.Run(pkg, func(t *testing.T) { assertContractStaysLight(t, pkg) })
	}
}

func assertContractStaysLight(t *testing.T, pkg string) {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}

	var thirdParty []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// A third-party import path has a dot in its first segment (a
		// domain); standard library paths do not. Vendored copies inside
		// the toolchain and our own module do not count.
		first, _, ok := strings.Cut(line, "/")
		if !ok || !strings.Contains(first, ".") {
			continue
		}
		if strings.HasPrefix(line, "vendor/") || strings.Contains(line, "jcsvwinston/nucleus") {
			continue
		}
		thirdParty = append(thirdParty, line)
	}

	if len(thirdParty) > maxPluginContractDeps {
		t.Fatalf("the plugin contract links %d third-party packages, ceiling is %d:\n  %s\n\nEvery package here is a dependency inherited by everyone who writes an authentication backend. If the import is genuinely necessary, raise maxPluginContractDeps deliberately and say why.",
			len(thirdParty), maxPluginContractDeps, strings.Join(thirdParty, "\n  "))
	}
	t.Logf("plugin contract links %d third-party package(s): %v", len(thirdParty), thirdParty)
}

// The extraction must not have broken a single line of anyone's code. The
// names in pkg/auth are ALIASES, so the types are identical rather than
// merely similar — a value built through one name satisfies an interface
// declared with the other, and errors.Is matches across the boundary.
func TestPluginContract_AliasesKeepSourceCompatibility(t *testing.T) {
	// Identical types, not convertible ones: this assignment does not compile
	// if the alias were a definition.
	var viaOldName *auth.User = &backend.User{ID: "1", Username: "ana"}
	var viaNewName *backend.User = &auth.User{ID: "1", Username: "ana"}
	if viaOldName.ID != viaNewName.ID {
		t.Fatal("the two names must denote the same type")
	}

	if auth.ErrInvalidCredentials != backend.ErrInvalidCredentials {
		t.Error("the sentinel errors must be the same value, or errors.Is stops matching across the boundary")
	}
	if auth.ErrBackendUnavailable != backend.ErrBackendUnavailable {
		t.Error("the sentinel errors must be the same value")
	}

	// A backend registered through either door lands in the same registry.
	name := "aliascontracttest"
	if err := auth.RegisterBackend(name, func(backend.Config) (backend.Backend, error) { return nil, nil }); err != nil {
		t.Fatalf("register through the old name: %v", err)
	}
	defer backend.Unregister(name)
	if _, ok := backend.Lookup(name); !ok {
		t.Error("a backend registered through auth.RegisterBackend must be visible to backend.Lookup — one registry, two doors")
	}
}
