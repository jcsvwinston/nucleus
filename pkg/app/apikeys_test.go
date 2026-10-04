// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth/apikeys"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// keyGet sends a request carrying an API key (none when key is "").
func keyGet(t *testing.T, a *App, path, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if key != "" {
		req.Header.Set(apikeys.HeaderName, key)
	}
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	return rec
}

// ownerRoute answers with the owner of the key that authenticated the
// request, or "anonymous".
func ownerRoute(a *App) {
	a.Router.Get("/api/owner", router.FromHTTP(func(w http.ResponseWriter, r *http.Request) {
		owner := "anonymous"
		if key, ok := apikeys.FromContext(r.Context()); ok {
			owner = key.OwnerID
		}
		_, _ = w.Write([]byte(owner))
	}))
}

func issue(t *testing.T, a *App, owner string) string {
	t.Helper()
	if a.apiKeys == nil {
		t.Fatal("WithAPIKeys opened no store")
	}
	_, presented, err := apikeys.Issue(context.Background(), a.apiKeys, apikeys.Key{Name: "test", OwnerID: owner})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return presented
}

// On the api starter's stack (WithoutDefaults) a valid key authenticates
// its owner, a forged one is refused, and no key passes through.
func TestWithAPIKeys_AuthenticatesWithoutDefaults(t *testing.T) {
	a, err := New(testAppConfig(), WithoutDefaults(), WithAPIKeys())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	ownerRoute(a)
	key := issue(t, a, "svc-billing")

	if rec := keyGet(t, a, "/api/owner", key); rec.Code != http.StatusOK || rec.Body.String() != "svc-billing" {
		t.Fatalf("a valid key: %d %q, want 200 svc-billing", rec.Code, rec.Body.String())
	}
	if rec := keyGet(t, a, "/api/owner", apikeys.Prefix+"_forged_forged"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a forged key: %d, want 401", rec.Code)
	}
	if rec := keyGet(t, a, "/api/owner", ""); rec.Code != http.StatusOK || rec.Body.String() != "anonymous" {
		t.Fatalf("no key: %d %q, want 200 anonymous", rec.Code, rec.Body.String())
	}
}

// Without the option nothing reads a key: the forged one is not refused.
func TestWithAPIKeys_OffByDefault(t *testing.T) {
	a, err := New(testAppConfig(), WithoutDefaults())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	ownerRoute(a)
	if rec := keyGet(t, a, "/api/owner", apikeys.Prefix+"_forged_forged"); rec.Code != http.StatusOK {
		t.Fatalf("without WithAPIKeys a key header answered %d", rec.Code)
	}
}

// On the default stack the key is read before the rate limiter, so two
// programs behind the same address get a bucket each — the reason the
// middleware sits next to the bearer decode and not after app.New.
func TestWithAPIKeys_TheLimiterKeysOnTheOwner(t *testing.T) {
	cfg := testAppConfig()
	cfg.RateLimitRequests = 1
	cfg.RateLimitWindow = time.Minute
	a, err := New(cfg, WithOpenAuthz(), WithAPIKeys())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	ownerRoute(a)
	one, two := issue(t, a, "svc-one"), issue(t, a, "svc-two")

	if rec := keyGet(t, a, "/api/owner", one); rec.Code != http.StatusOK {
		t.Fatalf("svc-one #1: %d", rec.Code)
	}
	if rec := keyGet(t, a, "/api/owner", two); rec.Code != http.StatusOK {
		t.Fatalf("svc-two #1 answered %d: the limiter keys on the address, not on the key's owner", rec.Code)
	}
	if rec := keyGet(t, a, "/api/owner", one); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("svc-one #2: %d, want 429 (its own bucket is spent)", rec.Code)
	}
}

func TestAPIKeyFlavor(t *testing.T) {
	for system, want := range map[string]apikeys.Flavor{"sqlite": apikeys.FlavorSQLite, "postgresql": apikeys.FlavorPostgres, "mysql": apikeys.FlavorMySQL} {
		if got, err := apiKeyFlavor(system); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", system, got, err, want)
		}
	}
	if _, err := apiKeyFlavor("mssql"); err == nil || !strings.Contains(err.Error(), "mssql") {
		t.Errorf("an engine the store cannot speak must be refused with its name: %v", err)
	}
}
