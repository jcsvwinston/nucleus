// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func checkApp(t *testing.T) App {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.LogLevel = "error"
	cfg.Storage.Provider = "memory"
	cfg.StateDir = t.TempDir()
	cfg.Databases = map[string]app.DatabaseConfig{"default": {URL: "sqlite://" + t.TempDir() + "/check.db"}}
	return App{Config: cfg}
}

func verdict(checks []ModuleCheck, name string) (error, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c.Err, true
		}
	}
	return nil, false
}

// A panic in the module's own code is a failed check, not a crashed test:
// boot would take the process down, and the check has more to say.
func TestCheckModule_PanickingOnStartIsAFailedStart(t *testing.T) {
	spec := Module[struct{}]{
		Name: "boom",
		OnStart: func(context.Context, Runtime, struct{}) error {
			var m map[string]int
			m["x"] = 1
			return nil
		},
		Routes: func(r Router, _ struct{}) { r.Get("/", func(c *Context) error { return c.NoContent() }) },
	}.Build()
	checks := CheckModule(context.Background(), checkApp(t), spec)
	err, ok := verdict(checks, "start")
	if !ok || err == nil || !strings.Contains(err.Error(), "OnStart panicked") {
		t.Fatalf("start: %v (ran %v)", err, ok)
	}
	if _, ran := verdict(checks, "shutdown"); ran {
		t.Error("a module whose OnStart failed is never shut down; the shutdown check must be left out")
	}
	if err, _ := verdict(checks, "routes"); err != nil {
		t.Errorf("the routes register whatever OnStart did: %v", err)
	}
}

// An application that cannot be built says so once, and the checks that
// need it are left out rather than failed on the module's account.
func TestCheckModule_ApplicationThatCannotBeBuilt(t *testing.T) {
	a := checkApp(t)
	a.Config.LogLevel = "verbose"
	checks := CheckModule(context.Background(), a, Module[struct{}]{Name: "fine"}.Build())
	err, ok := verdict(checks, "application")
	if !ok || err == nil || !strings.Contains(err.Error(), "log_level") {
		t.Fatalf("application: %v (ran %v)", err, ok)
	}
	for _, live := range []string{"models", "start", "routes", "shutdown"} {
		if _, ran := verdict(checks, live); ran {
			t.Errorf("%s ran without an application", live)
		}
	}
	if err, _ := verdict(checks, "name"); err != nil {
		t.Errorf("the module's own checks still run: name %v", err)
	}
}

// An OnShutdown that ignores its context is named with the budget boot
// would have given it.
func TestCheckModule_OnShutdownThatNeverReturns(t *testing.T) {
	a := checkApp(t)
	a.Config.WriteTimeout = 100 * time.Millisecond
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	spec := Module[struct{}]{
		Name:       "stuck",
		OnShutdown: func(context.Context, Runtime, struct{}) error { <-release; return nil },
	}.Build()
	err, ok := verdict(CheckModule(context.Background(), a, spec), "shutdown")
	if !ok || err == nil || !strings.Contains(err.Error(), "did not return within 100ms") {
		t.Fatalf("shutdown: %v (ran %v)", err, ok)
	}
}

// The module-name rule follows from where the framework uses the name.
func TestCheckModuleName(t *testing.T) {
	for _, ok := range []string{"notes", "shop_v2", "a1"} {
		if err := checkModuleName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"Notes", "shop.v2", "my-mod", "a/b", "1st", "with space"} {
		if err := checkModuleName(bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
}

func TestCheckModulePrefix(t *testing.T) {
	for _, ok := range []string{"", "/api", "/api/v1"} {
		if err := checkModulePrefix("m", ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"api", "/api/", "/api//v1", "/t/{tenant}", "/a/../b"} {
		if err := checkModulePrefix("m", bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
}

type checkClock struct{ zone string }

// A module that provides a value starts under the check the way it starts
// at boot (the check's runtime carries the table Provide writes to), and a
// DependsOn that names no mounted module is the error boot returns.
func TestCheckModule_ProvideAndDependsOn(t *testing.T) {
	provider := Module[struct{}]{
		Name: "clock",
		OnStart: func(_ context.Context, rt Runtime, _ struct{}) error {
			return Provide(rt, checkClock{zone: "UTC"})
		},
	}.Build()
	checks := CheckModule(context.Background(), checkApp(t), provider)
	if err, ok := verdict(checks, "start"); !ok || err != nil {
		t.Fatalf("a providing module fails its start under the check: %v (ran %v)", err, ok)
	}
	if err, _ := verdict(checks, "depends-on"); err != nil {
		t.Fatalf("a module that declares nothing: depends-on %v", err)
	}

	consumer := Module[struct{}]{Name: "billing", DependsOn: []string{"clock"}}.Build()
	checks = CheckModule(context.Background(), checkApp(t), consumer)
	if err, _ := verdict(checks, "depends-on"); err == nil || !strings.Contains(err.Error(), "clock") {
		t.Fatalf("a DependsOn on a module the application does not mount: %v", err)
	}
	a := checkApp(t)
	a.Modules = map[string]ModuleSpec{"clock": provider}
	checks = CheckModule(context.Background(), a, consumer)
	if err, _ := verdict(checks, "depends-on"); err != nil {
		t.Fatalf("the provider is mounted: depends-on %v", err)
	}
}
