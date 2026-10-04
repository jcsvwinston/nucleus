// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import "context"

// Key identifies one request-scoped value of type T. Create each key once,
// at package level, with NewKey, and share the variable between the code
// that sets the value and the code that reads it:
//
//	var CurrentTenant = nucleus.NewKey[Tenant]("tenant")
//
// A middleware stores the value in the request's context with
// CurrentTenant.WithValue; a handler reads it with nucleus.Value(c,
// CurrentTenant) and gets a Tenant, with no type assertion and no string key
// that two packages could both choose.
//
// Two keys are the same key only if they are the same variable: two NewKey
// calls with the same name are distinct keys. The zero Key is not usable —
// it stores nothing and finds nothing.
type Key[T any] struct {
	id *keyIdentity
}

// keyIdentity is what makes each NewKey distinct: the context stores values
// under a pointer to it.
type keyIdentity struct {
	name string
}

// contextKey is the unexported type values are stored under in a
// context.Context, so no other package's key can collide with one of ours.
type contextKey struct {
	id *keyIdentity
}

// NewKey returns a new key for request-scoped values of type T. The name
// appears only in String, for logs and debugging.
func NewKey[T any](name string) Key[T] {
	return Key[T]{id: &keyIdentity{name: name}}
}

// String returns the name the key was created with.
func (k Key[T]) String() string {
	if k.id == nil {
		return ""
	}
	return k.id.name
}

// WithValue returns a copy of ctx that carries v under k — the form a
// middleware uses:
//
//	next.ServeHTTP(w, r.WithContext(CurrentTenant.WithValue(r.Context(), t)))
//
// With the zero Key it returns ctx unchanged.
func (k Key[T]) WithValue(ctx context.Context, v T) context.Context {
	if k.id == nil || ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{id: k.id}, v)
}

// FromContext returns the value stored under k in ctx, and whether there is
// one.
func (k Key[T]) FromContext(ctx context.Context) (T, bool) {
	var zero T
	if k.id == nil || ctx == nil {
		return zero, false
	}
	v, ok := ctx.Value(contextKey{id: k.id}).(T)
	if !ok {
		return zero, false
	}
	return v, true
}

// SetValue stores v under k for the rest of this request's handling: the
// rest of the handler chain, and anything handed c or c.Request, reads it
// with Value. It replaces the request on c with one whose context carries
// the value, so an http.Handler middleware that wraps the route holds its
// own request and does not see it — such a middleware sets values with
// Key.WithValue before calling the next handler, and the handler reads them
// here.
func SetValue[T any](c *Context, k Key[T], v T) {
	if c == nil || c.Context == nil || c.Context.Request == nil {
		return
	}
	c.Context.Request = c.Context.Request.WithContext(k.WithValue(c.Context.Request.Context(), v))
}

// Value returns the value stored under k for this request — by SetValue, or
// by a middleware through Key.WithValue — and whether there is one.
func Value[T any](c *Context, k Key[T]) (T, bool) {
	if c == nil || c.Context == nil || c.Context.Request == nil {
		var zero T
		return zero, false
	}
	return k.FromContext(c.Context.Request.Context())
}
