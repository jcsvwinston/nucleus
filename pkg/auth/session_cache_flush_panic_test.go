// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-6 (external coverage demo, 2026-08-17): SessionCache.Flush slices
// every session key with key[:len(sessionCacheExpiryKey)] (14 bytes) after
// only checking len(key) > len(sessionCacheKeyPrefix) (7 bytes) — so any
// UNRELATED session key of 8–13 bytes ("user_id", "csrf_tok", "locale_x",
// real session data) makes Flush PANIC with a slice-bounds error.
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionCacheFlushSurvivesShortSessionKeys(t *testing.T) {
	sm := NewSessionManager(SessionConfig{Lifetime: time.Hour})
	cache := NewSessionCache(sm)

	deadline := time.Now().UTC().Add(time.Hour)
	payload, err := sm.SCS().Codec.Encode(deadline, map[string]interface{}{})
	if err != nil {
		t.Fatalf("encode session payload: %v", err)
	}
	token := "flush-panic-token"
	if err := sm.SCS().Store.Commit(token, payload, deadline); err != nil {
		t.Fatalf("commit seed payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sm.SCS().Cookie.Name, Value: token})

	handler := sm.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// Real-world session state: plain keys of 8–13 bytes, exactly the
		// range that passes the 7-byte guard and dies on the 14-byte slice.
		sm.SCS().Put(ctx, "user_id", int(42))       // 7 bytes — safe today, keep it honest
		sm.SCS().Put(ctx, "locale_x", "es")         // 8 bytes — panics on v1.8.1
		sm.SCS().Put(ctx, "csrf_token_a", "tok")    // 12 bytes — panics on v1.8.1
		cache.Put(ctx, "cached", "value", 0)

		// The panic: Flush must survive arbitrary session keys and remove
		// ONLY the cache-prefixed entries.
		cache.Flush(ctx)

		if _, ok := cache.Get(ctx, "cached"); ok {
			t.Error("cache entry survived Flush")
		}
		if sm.SCS().GetString(ctx, "locale_x") != "es" {
			t.Error("Flush removed an unrelated session key")
		}
		if got := sm.SCS().GetInt(ctx, "user_id"); got != 42 {
			t.Errorf("Flush touched user_id: got %d", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handler did not complete: HTTP %d", rec.Code)
	}
}
