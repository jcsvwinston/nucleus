package model

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestSetDefaultSQLObserver_FiresAdditively verifies that BOTH the
// per-instance observer (legacy admin Panel hook) and the process-wide
// default observer (new observability bus) receive every event. This is
// the contract Phase 2 of the admin refactor relies on.
func TestSetDefaultSQLObserver_FiresAdditively(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}

	crud := NewCRUD(sqlDB, meta, nil)

	var perInstance atomic.Int32
	crud.SetSQLQueryObserver(func(ctx context.Context, _ SQLQueryEvent) {
		perInstance.Add(1)
	})

	var globalDef atomic.Int32
	SetDefaultSQLObserver(func(ctx context.Context, _ SQLQueryEvent) {
		globalDef.Add(1)
	})
	t.Cleanup(func() { SetDefaultSQLObserver(nil) })

	if err := crud.Create(context.Background(), &TestUser{
		Email: "obs@test.com", Name: "Obs", Role: "user", Active: true,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if perInstance.Load() == 0 {
		t.Fatal("per-instance observer did not fire")
	}
	if globalDef.Load() == 0 {
		t.Fatal("default observer did not fire")
	}
	if perInstance.Load() != globalDef.Load() {
		t.Fatalf("observer counts diverged: per-instance=%d default=%d",
			perInstance.Load(), globalDef.Load())
	}
}

// TestSetDefaultSQLObserver_OnlyDefault verifies the default fires alone
// when no per-instance observer is set.
func TestSetDefaultSQLObserver_OnlyDefault(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}

	crud := NewCRUD(sqlDB, meta, nil)
	// No per-instance observer.

	var fired atomic.Int32
	SetDefaultSQLObserver(func(ctx context.Context, _ SQLQueryEvent) {
		fired.Add(1)
	})
	t.Cleanup(func() { SetDefaultSQLObserver(nil) })

	if err := crud.Create(context.Background(), &TestUser{
		Email: "default-only@test.com", Name: "Def", Role: "user", Active: true,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if fired.Load() == 0 {
		t.Fatal("default observer did not fire")
	}
}

// TestSetDefaultSQLObserver_Clear verifies that nil disables the default.
func TestSetDefaultSQLObserver_Clear(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}

	crud := NewCRUD(sqlDB, meta, nil)

	var fired atomic.Int32
	SetDefaultSQLObserver(func(ctx context.Context, _ SQLQueryEvent) {
		fired.Add(1)
	})
	SetDefaultSQLObserver(nil)
	t.Cleanup(func() { SetDefaultSQLObserver(nil) })

	if err := crud.Create(context.Background(), &TestUser{
		Email: "cleared@test.com", Name: "C", Role: "user", Active: true,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if fired.Load() != 0 {
		t.Fatalf("default observer fired %d time(s) after clear", fired.Load())
	}
}
