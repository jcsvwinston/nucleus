// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// A1 auth findings of the 2026-09-03 maturity audit: NU-9 (flash data has
// a life cycle), NU-29 (issuer and audience are verified), NU-42 (an empty
// cached string is a hit, not a miss).
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// flashClient drives a SessionManager through its middleware like a
// browser would: one cookie jar, one request per step.
type flashClient struct {
	t      *testing.T
	sm     *SessionManager
	cookie *http.Cookie
}

func (c *flashClient) do(fn func(ctx context.Context)) {
	c.t.Helper()
	h := c.sm.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fn(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == c.sm.scs.Cookie.Name {
			c.cookie = ck
		}
	}
}

func TestFlash_LivesExactlyOneRequest(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	c := &flashClient{t: t, sm: sm}

	c.do(func(ctx context.Context) { sm.Flash(ctx, "msg", "saved") })
	c.do(func(ctx context.Context) {
		if got := sm.GetFlash(ctx, "msg"); got != "saved" {
			t.Fatalf("request 2: GetFlash = %q, want saved", got)
		}
	})
	c.do(func(ctx context.Context) {
		if got := sm.GetFlash(ctx, "msg"); got != "" {
			t.Fatalf("request 3: GetFlash = %q, want empty (the flash had its one request)", got)
		}
	})
}

func TestFlash_NowIsThisRequestOnly(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	c := &flashClient{t: t, sm: sm}
	c.do(func(ctx context.Context) {
		sm.Now(ctx, "n", "here")
		if got := sm.GetFlash(ctx, "n"); got != "here" {
			t.Fatalf("Now not readable in its own request: %q", got)
		}
	})
	c.do(func(ctx context.Context) {
		if got := sm.GetFlash(ctx, "n"); got != "" {
			t.Fatalf("Now leaked into the next request: %q", got)
		}
	})
}

func TestFlash_ReflashAndKeepBuyOneMoreRequest(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	c := &flashClient{t: t, sm: sm}
	c.do(func(ctx context.Context) { sm.Flash(ctx, "a", "1"); sm.Flash(ctx, "b", "2"); sm.FlashInt(ctx, "n", 7) })
	c.do(func(ctx context.Context) { sm.Keep(ctx, []string{"a"}) })
	c.do(func(ctx context.Context) {
		if sm.GetFlash(ctx, "a") != "1" || sm.GetFlash(ctx, "b") != "" {
			t.Fatalf("Keep: a=%q b=%q, want a=1 and b gone", sm.GetFlash(ctx, "a"), sm.GetFlash(ctx, "b"))
		}
		sm.Reflash(ctx)
	})
	c.do(func(ctx context.Context) {
		if sm.GetFlash(ctx, "a") != "1" {
			t.Fatalf("Reflash did not carry a over")
		}
	})
	c.do(func(ctx context.Context) {
		if sm.GetFlash(ctx, "a") != "" || sm.GetFlashInt(ctx, "n") != 0 {
			t.Fatalf("flash data survived past its extra request")
		}
	})
}

func TestJWT_IssuerAndAudienceAreVerified(t *testing.T) {
	secret := strings.Repeat("s", 32)
	ours := NewJWTManager(secret, time.Hour, "https://app.example")
	other := NewJWTManager(secret, time.Hour, "https://other.example")

	tok, err := ours.Generate("u1", "alice", "user")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := ours.Validate(tok); err != nil {
		t.Fatalf("own token rejected: %v", err)
	}
	foreign, _ := other.Generate("u1", "alice", "user")
	if _, err := ours.Validate(foreign); err == nil {
		t.Fatalf("a token minted with another issuer validated (same secret): iss is not checked")
	}

	ours.SetAudience("api.example")
	tok, _ = ours.Generate("u1", "alice", "user")
	claims, err := ours.Validate(tok)
	if err != nil {
		t.Fatalf("token with audience rejected: %v", err)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "api.example" {
		t.Fatalf("aud not stamped: %v", claims.Audience)
	}
	// A token for another audience, same issuer and secret, is refused.
	noAud := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{UserID: "u1", RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "https://app.example", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), Audience: jwt.ClaimStrings{"someone-else"},
	}})
	signed, _ := noAud.SignedString([]byte(secret))
	if _, err := ours.Validate(signed); err == nil {
		t.Fatalf("token for another audience validated")
	}
}

func TestSessionCache_EmptyStringIsAHit(t *testing.T) {
	sm := NewSessionManager(SessionConfig{})
	c := &flashClient{t: t, sm: sm}
	cache := NewSessionCache(sm)
	c.do(func(ctx context.Context) {
		cache.Put(ctx, "empty", "", 0)
		if _, ok := cache.Get(ctx, "empty"); !ok {
			t.Fatalf("an empty string stored in the cache reads as a miss")
		}
		if _, ok := cache.Get(ctx, "absent"); ok {
			t.Fatalf("an absent key reads as a hit")
		}
	})
}
