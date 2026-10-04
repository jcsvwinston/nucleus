// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

type tenant struct{ ID string }

func TestKey_WithValueAndFromContext(t *testing.T) {
	k := NewKey[tenant]("tenant")
	if k.String() != "tenant" {
		t.Fatalf("String = %q", k.String())
	}
	ctx := k.WithValue(context.Background(), tenant{ID: "acme"})
	got, ok := k.FromContext(ctx)
	if !ok || got.ID != "acme" {
		t.Fatalf("FromContext = %+v, %v", got, ok)
	}

	// Two keys with one name are two keys.
	other := NewKey[tenant]("tenant")
	if _, ok := other.FromContext(ctx); ok {
		t.Fatal("a second NewKey with the same name read the first key's value")
	}
	// A key of another type with the same name finds nothing either.
	if _, ok := NewKey[string]("tenant").FromContext(ctx); ok {
		t.Fatal("a string key read a tenant value")
	}

	// The zero Key stores nothing and finds nothing.
	var zero Key[tenant]
	if zctx := zero.WithValue(ctx, tenant{ID: "x"}); zctx != ctx {
		t.Fatal("zero key changed the context")
	}
	if _, ok := zero.FromContext(ctx); ok {
		t.Fatal("zero key found a value")
	}
	var none context.Context
	if _, ok := k.FromContext(none); ok {
		t.Fatal("nil context found a value")
	}
}

func TestSetValueAndValue_OnTheContext(t *testing.T) {
	k := NewKey[int]("attempts")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := &Context{Context: routerpkg.NewContext(httptest.NewRecorder(), req, nil)}
	if _, ok := Value(c, k); ok {
		t.Fatal("value before SetValue")
	}
	SetValue(c, k, 3)
	if n, ok := Value(c, k); !ok || n != 3 {
		t.Fatalf("Value = %d, %v", n, ok)
	}
	// The request on the context carries it, for code handed c.Request.
	if n, ok := k.FromContext(c.Request.Context()); !ok || n != 3 {
		t.Fatalf("FromContext(c.Request.Context()) = %d, %v", n, ok)
	}
	// Nil-safe.
	SetValue[int](nil, k, 1)
	if _, ok := Value[int](nil, k); ok {
		t.Fatal("nil context found a value")
	}
}

// Through a route: an http middleware sets the value with Key.WithValue, a
// first handler in the chain adds another with SetValue, and the last
// handler reads both typed.
func TestValues_ThroughMiddlewareAndHandlerChain(t *testing.T) {
	who := NewKey[tenant]("tenant")
	step := NewKey[string]("step")

	mux := routerpkg.NewMux()
	a := newRouterAdapterFromMux(mux, "")
	setTenant := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(who.WithValue(r.Context(), tenant{ID: r.Header.Get("X-Tenant")})))
		})
	}
	first := func(c *Context) error {
		SetValue(c, step, "first-ran")
		return c.Next()
	}
	last := func(c *Context) error {
		tn, ok := Value(c, who)
		if !ok {
			return c.String(http.StatusInternalServerError, "no tenant")
		}
		s, _ := Value(c, step)
		return c.String(http.StatusOK, tn.ID+"/"+s)
	}
	a.With(setTenant).Get("/who", first, last)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/who", nil)
	req.Header.Set("X-Tenant", "acme")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "acme/first-ran" {
		t.Fatalf("GET /who = %d %q", rec.Code, rec.Body.String())
	}
}
