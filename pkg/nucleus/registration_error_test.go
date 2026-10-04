// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A malformed declaration a registration helper refuses is a boot error
// naming the module — not a crash, and not a route registered anyway.
func TestRegistrationHelperRefusalIsABootError(t *testing.T) {
	bad := Module[struct{}]{
		Name: "notes",
		Routes: func(r Router, _ struct{}) {
			Versioned(r, APIVersion{Name: "v/1"}, func(g Router) {
				g.Get("/notes", func(c *Context) error { return c.NoContent() })
			})
		},
	}.Build()
	a := checkApp(t)
	a.Modules = map[string]ModuleSpec{"notes": bad}
	a.Config.Port = 0
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := RunContext(ctx, a)
	if err == nil || !strings.Contains(err.Error(), `module "notes"`) || !strings.Contains(err.Error(), "one path segment") {
		t.Fatalf("a malformed version: want a boot error naming the module, got %v", err)
	}
}

// A Router the framework did not build has no boot to fail: the helper
// panics there, as a programmer error.
func TestRegistrationHelperRefusalOnAForeignRouterPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic on a foreign Router")
		}
	}()
	Versioned(foreignRouter{}, APIVersion{Name: ""}, func(Router) {})
}

type foreignRouter struct{ Router }

// Handle with a method it does not register is a boot error naming the
// module, like a malformed version.
func TestHandleUnsupportedMethodIsABootError(t *testing.T) {
	bad := Module[struct{}]{
		Name: "notes",
		Routes: func(r Router, _ struct{}) {
			Handle(r, "TRACE", "/notes", func(*Context, struct{}) (struct{}, error) { return struct{}{}, nil })
		},
	}.Build()
	a := checkApp(t)
	a.Modules = map[string]ModuleSpec{"notes": bad}
	a.Config.Port = 0
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := RunContext(ctx, a); err == nil || !strings.Contains(err.Error(), `module "notes"`) || !strings.Contains(err.Error(), "unsupported method") {
		t.Fatalf("want a boot error naming the module, got %v", err)
	}
}
