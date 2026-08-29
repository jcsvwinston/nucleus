// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package interceptor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func recording(tag string, log *[]string) Factory {
	return func(Config) (Interceptor, error) {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				*log = append(*log, "enter:"+tag)
				next.ServeHTTP(w, r)
				*log = append(*log, "exit:"+tag)
			})
		}, nil
	}
}

func register(t *testing.T, name string, f Factory) {
	t.Helper()
	if err := Register(name, f); err != nil {
		t.Fatalf("Register(%q): %v", name, err)
	}
	t.Cleanup(func() { Unregister(name) })
}

// Order is the behaviour, not a detail: the first name in the list must
// be outermost — it sees the request first and the response last.
func TestBuild_DeclarationOrderIsTheRequestOrder(t *testing.T) {
	var log []string
	register(t, "first", recording("first", &log))
	register(t, "second", recording("second", &log))

	chain, err := Build([]string{"first", "second"}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var h http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		log = append(log, "handler")
	})
	for i := len(chain) - 1; i >= 0; i-- {
		h = chain[i](h)
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := "enter:first enter:second handler exit:second exit:first"
	if got := strings.Join(log, " "); got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}

// A typo in a list of request interceptors must not resolve to "one fewer
// protection, quietly". It fails, naming what is registered.
func TestBuild_UnregisteredNameFailsNamingTheRegistered(t *testing.T) {
	var log []string
	register(t, "audit", recording("audit", &log))

	_, err := Build([]string{"audti"}, nil)
	if err == nil {
		t.Fatal("an unregistered interceptor must fail")
	}
	if !strings.Contains(err.Error(), "audit") {
		t.Errorf("the error must name what IS registered, got: %v", err)
	}
}

// A factory that cannot configure itself fails BOOT. A protection that is
// not there must not be discovered by an audit six months later.
func TestBuild_FactoryErrorFailsBoot(t *testing.T) {
	register(t, "broken", func(Config) (Interceptor, error) {
		return nil, fmt.Errorf("missing signing key")
	})

	_, err := Build([]string{"broken"}, nil)
	if err == nil {
		t.Fatal("a factory error must fail the build")
	}
	if !strings.Contains(err.Error(), "missing signing key") {
		t.Errorf("the cause must survive, got: %v", err)
	}
}

// A factory returning neither an interceptor nor an error would remove
// itself from the request path in silence.
func TestBuild_NilInterceptorWithoutErrorFails(t *testing.T) {
	register(t, "vanishing", func(Config) (Interceptor, error) { return nil, nil })

	if _, err := Build([]string{"vanishing"}, nil); err == nil {
		t.Fatal("a factory that returns no interceptor and no error must fail rather than silently drop out of the request path")
	}
}

// The same interceptor twice is almost always a copy-paste, and running
// it twice is rarely what anybody means.
func TestBuild_DuplicateNameIsAnError(t *testing.T) {
	var log []string
	register(t, "audit", recording("audit", &log))

	if _, err := Build([]string{"audit", "audit"}, nil); err == nil {
		t.Fatal("the same interceptor declared twice must fail")
	}
}

// Each interceptor is handed its own subtree, the same channel every
// other provider in this framework is configured through.
func TestBuild_HandsEachInterceptorItsOwnSubtree(t *testing.T) {
	var seen []Config
	capture := func(cfg Config) (Interceptor, error) {
		seen = append(seen, cfg)
		return func(next http.Handler) http.Handler { return next }, nil
	}
	register(t, "audit", capture)
	register(t, "tenant", capture)

	_, err := Build([]string{"audit", "tenant"}, map[string]map[string]any{
		"audit":  {"sink": "stdout"},
		"tenant": {"header": "X-Tenant"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("both factories must run, got %d", len(seen))
	}
	if seen[0].Name != "audit" || seen[0].ProviderConfig["sink"] != "stdout" {
		t.Errorf("audit got %+v", seen[0])
	}
	if seen[1].Name != "tenant" || seen[1].ProviderConfig["header"] != "X-Tenant" {
		t.Errorf("tenant got %+v", seen[1])
	}
}

// Two packages claiming one name would make the effective interceptor
// depend on import order — for something in the request path, that is a
// security control whose identity depends on an import block.
func TestRegister_DuplicateNameIsAnError(t *testing.T) {
	var log []string
	register(t, "audit", recording("audit", &log))

	if err := Register("audit", recording("other", &log)); err == nil {
		t.Fatal("registering a name twice must fail")
	}
}
