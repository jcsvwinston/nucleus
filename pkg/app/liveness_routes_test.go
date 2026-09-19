// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

// For the whole of v1.x the framework served only /healthz, so an application
// that wanted the two Kubernetes probes wrote them itself. If the framework
// registers its own in New, that application's registration is the SECOND for
// the same pattern and net/http's ServeMux panics: a minor upgrade would stop
// it booting. The probes are mounted at Run instead, for whichever path is
// still free.
func TestOwnLivezKeepsBooting(t *testing.T) {
	cfg := DefaultConfig()
	a, err := New(&cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if rv := recover(); rv != nil {
			t.Fatalf("an application that serves its own /livez no longer boots: %v", rv)
		}
	}()
	a.Router.Get("/livez", func(c *router.Context) error { c.Writer.WriteHeader(http.StatusOK); return nil })
	a.Router.Get("/readyz", func(c *router.Context) error { c.Writer.WriteHeader(http.StatusOK); return nil })

	// And mounting the defaults afterwards leaves both of them alone.
	a.mountDefaultProbes()

	seen := map[string]int{}
	_ = a.Router.Walk(func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route == "/livez" || route == "/readyz" {
			seen[route]++
		}
		return nil
	})
	for _, route := range []string{"/livez", "/readyz"} {
		if seen[route] != 1 {
			t.Errorf("%s is registered %d times; the application's own handler must be the only one", route, seen[route])
		}
	}
}

// And an application that does NOT define them still gets both, which is the
// point of shipping them at all.
func TestDefaultProbesAreMountedWhenTheApplicationHasNone(t *testing.T) {
	cfg := DefaultConfig()
	a, err := New(&cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.mountDefaultProbes()

	seen := map[string]bool{}
	_ = a.Router.Walk(func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		seen[route] = true
		return nil
	})
	for _, route := range []string{"/livez", "/readyz"} {
		if !seen[route] {
			t.Errorf("%s is not mounted, so an orchestrator has nothing to probe", route)
		}
	}
	// Twice is a no-op, because Run may follow a caller that already asked.
	a.mountDefaultProbes()
}

// Run mounts them: that is the path every application and the test kit take.
func TestRunMountsTheDefaultProbes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	a, err := New(&cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		mounted := false
		_ = a.Router.Walk(func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if route == "/livez" {
				mounted = true
			}
			return nil
		})
		if mounted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run did not mount /livez")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}
