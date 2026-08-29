// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/router/interceptor"
)

// An interceptor declared in the file must reach the request path. This
// is the whole arc in one test: a package registers itself, the operator
// names it in nucleus.yml, and the framework wires it — no bootstrap code
// pasted into the application.
func TestLoadConfig_InterceptorListAndSubtree(t *testing.T) {
	registerNoop(t, "audit")
	registerNoop(t, "tenant-guard")
	path := writeConfig(t, `
http_interceptors: [audit, tenant-guard]
interceptors:
  audit:
    sink: stdout
  tenant-guard:
    header: X-Tenant
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.HTTPInterceptors) != 2 || cfg.HTTPInterceptors[0] != "audit" {
		t.Fatalf("declaration order must survive, got %v", cfg.HTTPInterceptors)
	}
	if got := cfg.InterceptorConfig["audit"]["sink"]; got != "stdout" {
		t.Errorf("audit subtree = %v; an interceptor must be able to read its own settings", got)
	}
	if got := cfg.InterceptorConfig["tenant-guard"]["header"]; got != "X-Tenant" {
		t.Errorf("tenant-guard subtree = %v", got)
	}
}

// The subtree of a REGISTERED interceptor is exempt from the schema, the
// same rule every other provider namespace follows — and it is exempt per
// registered name, never for the whole namespace.
func TestLoadConfig_InterceptorSubtreeExemptOnlyWhenRegistered(t *testing.T) {
	registerNoop(t, "audit")

	if _, err := LoadConfig(writeConfig(t, `
http_interceptors: [audit]
interceptors:
  audit:
    sink: stdout
`)); err != nil {
		t.Fatalf("a registered interceptor's subtree must be accepted: %v", err)
	}

	_, err := LoadConfig(writeConfig(t, `
http_interceptors: [audit]
interceptors:
  audit:
    sink: stdout
  audti:
    sink: stdout
`))
	if err == nil {
		t.Fatal("a subtree under an unregistered name must still be an unknown key")
	}
	if !strings.Contains(err.Error(), "audti") {
		t.Errorf("the error must name the typo, got: %v", err)
	}
}

func registerNoop(t *testing.T, name string) {
	t.Helper()
	if err := interceptor.Register(name, func(interceptor.Config) (interceptor.Interceptor, error) {
		return func(next http.Handler) http.Handler { return next }, nil
	}); err != nil {
		t.Fatalf("Register(%q): %v", name, err)
	}
	t.Cleanup(func() { interceptor.Unregister(name) })
}
