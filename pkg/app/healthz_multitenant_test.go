// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/storage"
	"github.com/jcsvwinston/nucleus/pkg/storage/provider"
)

// NU-111: in a multi-tenant application the storage probe of /healthz and
// /readyz listed its sentinel through the application's TenantStore, and a
// probe request carries no tenant — an orchestrator does not send
// X-Tenant-ID. By default the first probe left the WARN that exists for
// background jobs writing tenant data into the shared key space; with
// multitenant.require_tenant_storage: true the probe failed with
// ErrNoTenantInContext and /healthz answered 503 with the application
// healthy, which a load balancer reads as "take this instance out".
//
// The probe now checks the backing store, below the tenant policy; the
// policy itself still applies to every tenant-less operation the
// application makes.

// sharedKeySpaceWarn is the WARN the tenant policy logs the first time a
// tenant-less operation degrades to the shared key space.
const sharedKeySpaceWarn = "degraded to the SHARED (unprefixed) key space"

// probeMarker separates, in the captured log, what the probes wrote from
// what the application's own tenant-less operation wrote afterwards.
const probeMarker = "--- NU-111: probes done ---"

// multiTenantStorageConfig is the tutorial's multi-tenant configuration
// (header resolver, one shared database) with an in-process store.
func multiTenantStorageConfig(storageProvider string, strict bool) *Config {
	cfg := testAppConfig()
	cfg.LogLevel = "warn"
	cfg.LogFormat = "text"
	cfg.Storage.Provider = storageProvider
	cfg.MultiTenant = MultiTenantConfig{
		Enabled:              true,
		Resolver:             "header",
		Header:               "X-Tenant-ID",
		RequireIsolatedDB:    false,
		RequireTenantStorage: strict,
	}
	return cfg
}

// errBrokenBackend is what the broken backend answers to every List.
var errBrokenBackend = errors.New("nu111 test backend: connection refused")

// brokenStore is a backend that is really down: every List fails.
type brokenStore struct{ storage.Store }

func (brokenStore) List(context.Context, storage.ListOptions) (storage.ListResult, error) {
	return storage.ListResult{}, errBrokenBackend
}

// registerBrokenStorage registers, for the duration of the test, a storage
// provider whose backend is down.
func registerBrokenStorage(t *testing.T) string {
	t.Helper()
	name := "nu111-broken-" + strings.ToLower(strings.NewReplacer("/", "-", " ", "-").Replace(t.Name()))
	if err := storage.RegisterProvider(name, func(storage.Config) (storage.Store, error) {
		return brokenStore{Store: storage.NewMemoryStore()}, nil
	}); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	t.Cleanup(func() { provider.Unregister(name) })
	return name
}

// probeStorageCheck asks path the way an orchestrator does — no tenant
// header — and returns the status code and the storage entry of the body.
func probeStorageCheck(t *testing.T, a *App, path string) (int, HealthzCheck) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var body struct {
		Checks []HealthzCheck `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("GET %s: body is not JSON (%d): %s", path, rec.Code, rec.Body.String())
	}
	for _, ch := range body.Checks {
		if ch.Name == "storage" {
			return rec.Code, ch
		}
	}
	t.Fatalf("GET %s: no storage check in %s", path, rec.Body.String())
	return 0, HealthzCheck{}
}

// newMultiTenantApp builds the application and mounts /readyz the way Run
// does.
func newMultiTenantApp(t *testing.T, cfg *Config) *App {
	t.Helper()
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.mountDefaultProbes()
	return a
}

// Default mode: the probes are healthy and say nothing; the first
// tenant-less operation of the application itself still gets the WARN.
func TestHealthz_MultiTenantStorage_DefaultModeProbeDoesNotWarn(t *testing.T) {
	resetDrainingForTest()
	cfg := multiTenantStorageConfig("memory", false)

	type answer struct {
		path  string
		code  int
		check HealthzCheck
	}
	var answers []answer
	out := captureStdout(t, func() {
		a := newMultiTenantApp(t, cfg)
		defer func() { _ = a.Shutdown(context.Background()) }()
		for _, path := range []string{"/healthz", "/readyz", "/healthz"} {
			code, check := probeStorageCheck(t, a, path)
			answers = append(answers, answer{path, code, check})
		}
		fmt.Println(probeMarker)
		// A real tenant-less operation: the policy must still see it.
		if _, err := a.Storage.List(context.Background(), storage.ListOptions{Limit: 1}); err != nil {
			t.Errorf("tenant-less List in default mode: %v", err)
		}
	})

	for _, ans := range answers {
		if ans.code != http.StatusOK || ans.check.Status != "healthy" {
			t.Errorf("GET %s = %d, storage %q (%s); want 200 and healthy", ans.path, ans.code, ans.check.Status, ans.check.Message)
		}
	}
	probes, after, found := strings.Cut(out, probeMarker)
	if !found {
		t.Fatalf("marker missing from the captured log:\n%s", out)
	}
	if lines := linesContaining(probes, sharedKeySpaceWarn); len(lines) != 0 {
		t.Errorf("a storage probe tripped the tenant policy:\n%s", strings.Join(lines, "\n"))
	}
	if lines := linesContaining(after, sharedKeySpaceWarn); len(lines) != 1 {
		t.Errorf("the application's own tenant-less operation must still warn once, got %d lines:\n%s", len(lines), after)
	}
}

// Strict mode: a healthy store answers 200; the application's own
// tenant-less operations are still rejected.
func TestHealthz_MultiTenantStorage_StrictModeHealthyStoreIs200(t *testing.T) {
	resetDrainingForTest()
	cfg := multiTenantStorageConfig("memory", true)

	captureStdout(t, func() {
		a := newMultiTenantApp(t, cfg)
		defer func() { _ = a.Shutdown(context.Background()) }()

		for _, path := range []string{"/healthz", "/readyz"} {
			code, check := probeStorageCheck(t, a, path)
			if code != http.StatusOK || check.Status != "healthy" {
				t.Errorf("GET %s = %d, storage %q (%s); want 200 and healthy — the application is healthy", path, code, check.Status, check.Message)
			}
		}

		ctx := context.Background()
		if _, err := a.Storage.List(ctx, storage.ListOptions{Limit: 1}); !errors.Is(err, storage.ErrNoTenantInContext) {
			t.Errorf("tenant-less List in strict mode err = %v; want ErrNoTenantInContext", err)
		}
		if _, err := a.Storage.Put(ctx, "uploads/a.txt", strings.NewReader("x"), storage.PutOptions{}); !errors.Is(err, storage.ErrNoTenantInContext) {
			t.Errorf("tenant-less Put in strict mode err = %v; want ErrNoTenantInContext", err)
		}
	})
}

// Both modes: a backend that is really down still fails the probes, with
// the backend's own error — not the tenant policy's.
func TestHealthz_MultiTenantStorage_BrokenBackendIsStill503(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprintf("require_tenant_storage=%t", strict), func(t *testing.T) {
			resetDrainingForTest()
			cfg := multiTenantStorageConfig(registerBrokenStorage(t), strict)

			captureStdout(t, func() {
				a := newMultiTenantApp(t, cfg)
				defer func() { _ = a.Shutdown(context.Background()) }()

				for _, path := range []string{"/healthz", "/readyz"} {
					code, check := probeStorageCheck(t, a, path)
					if code != http.StatusServiceUnavailable || check.Status != "unhealthy" {
						t.Errorf("GET %s = %d, storage %q; want 503 and unhealthy — the backend is down", path, code, check.Status)
					}
					if !strings.Contains(check.Message, errBrokenBackend.Error()) {
						t.Errorf("GET %s storage message = %q; want the backend's error %q", path, check.Message, errBrokenBackend)
					}
				}
			})
		})
	}
}
