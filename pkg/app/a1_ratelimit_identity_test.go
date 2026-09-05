// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// NU-4 / NU-28 (maturity audit 2026-09-03): the limiter used to sit in the
// router's default stack, outermost, before the bearer was decoded — so
// `user:` and `tenant:` keys were unreachable and rate_limit_by_role was
// inert. It now mounts after the identity middleware; these tests pin the
// keys it sees.
package app

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRateLimit_KeyedByAuthenticatedUser(t *testing.T) {
	cfg := testAppConfig()
	cfg.JWTSecret = "nu-4-rate-limit-test-secret-0123456789"
	cfg.RateLimitRequests = 1
	cfg.RateLimitWindow = time.Minute
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	if a.JWT == nil {
		t.Fatal("JWT manager missing")
	}
	alice, _ := a.JWT.Generate("u-alice", "alice", "user")
	bob, _ := a.JWT.Generate("u-bob", "bob", "user")

	// Same client IP for every request (httptest uses 192.0.2.1): with an
	// IP key the second request of ANY caller would be refused.
	if rec := bearerGet(t, a, "/api/mine", alice); rec.Code == http.StatusTooManyRequests {
		t.Fatalf("alice #1: 429 on the first request")
	}
	if rec := bearerGet(t, a, "/api/mine", bob); rec.Code == http.StatusTooManyRequests {
		t.Fatalf("bob #1 was refused: the limiter keys by IP, not by user")
	}
	if rec := bearerGet(t, a, "/api/mine", alice); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("alice #2: status %d, want 429 (her own bucket is spent)", rec.Code)
	}
	// Anonymous callers share the IP bucket, and it is a different bucket
	// from the users'.
	if rec := bearerGet(t, a, "/api/mine", ""); rec.Code == http.StatusTooManyRequests {
		t.Fatalf("anonymous #1: 429 although the IP bucket was untouched")
	}
	if rec := bearerGet(t, a, "/api/mine", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("anonymous #2: status %d, want 429", rec.Code)
	}
}

func TestToTimeoutSeconds_NegativeDisables(t *testing.T) {
	cases := map[time.Duration]int{-1: 0, -time.Second: 0, 0: 30, 500 * time.Millisecond: 1, 5 * time.Second: 5}
	for in, want := range cases {
		if got := toTimeoutSeconds(in); got != want {
			t.Errorf("toTimeoutSeconds(%v) = %d, want %d", in, got, want)
		}
	}
}
