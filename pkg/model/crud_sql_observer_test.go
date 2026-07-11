package model

import (
	"context"
	"testing"
)

func TestCRUD_SQLQueryObserverReceivesEvents(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}

	crud := NewCRUD(sqlDB, meta, nil)

	events := make([]SQLQueryEvent, 0, 8)
	crud.SetSQLQueryObserver(func(ctx context.Context, event SQLQueryEvent) {
		events = append(events, event)
	})

	entity := &TestUser{
		Email:  "observer@test.com",
		Name:   "Observer",
		Role:   "user",
		Active: true,
	}
	if err := crud.Create(context.Background(), entity); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if _, err := crud.FindAll(context.Background(), QueryOpts{Page: 1, PageSize: 10}); err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}

	if err := crud.Delete(context.Background(), entity.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if len(events) == 0 {
		t.Fatalf("expected SQL observer events, got none")
	}

	ops := map[string]bool{}
	for _, event := range events {
		if event.ModelName != "TestUser" {
			t.Fatalf("expected model TestUser, got %q", event.ModelName)
		}
		if event.Query == "" {
			t.Fatalf("expected non-empty query in event: %#v", event)
		}
		ops[event.Operation] = true
	}

	if !ops["insert"] {
		t.Fatalf("expected insert operation event, got ops=%v", ops)
	}
	// select.count is no longer used due to estimation optimization
	// if !ops["select.count"] {
	// 	t.Fatalf("expected select.count operation event, got ops=%v", ops)
	// }
	if !ops["select.list"] {
		t.Fatalf("expected select.list operation event, got ops=%v", ops)
	}
	if !ops["soft_delete"] {
		t.Fatalf("expected soft_delete operation event, got ops=%v", ops)
	}
}

func TestCRUD_SQLQueryObserverDisabledWhenNil(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}
	crud := NewCRUD(sqlDB, meta, nil)

	called := false
	crud.SetSQLQueryObserver(func(ctx context.Context, event SQLQueryEvent) {
		called = true
	})
	crud.SetSQLQueryObserver(nil)

	if err := crud.Create(context.Background(), &TestUser{
		Email: "nil-observer@test.com",
		Name:  "No Observer",
	}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if called {
		t.Fatal("expected observer not to be called after SetSQLQueryObserver(nil)")
	}
}

// TestCRUD_SQLQueryObserverReportsRowsAffected pins the v1.1.0 additive
// contract: exec-style operations carry the driver-reported row count;
// SELECT paths report 0 ("not reported").
func TestCRUD_SQLQueryObserverReportsRowsAffected(t *testing.T) {
	sqlDB := setupTestDB(t)
	meta, _ := ExtractMeta(&TestUser{})
	meta.Config = ModelConfig{PageSize: 25}

	crud := NewCRUD(sqlDB, meta, nil)

	events := make([]SQLQueryEvent, 0, 8)
	crud.SetSQLQueryObserver(func(_ context.Context, event SQLQueryEvent) {
		events = append(events, event)
	})

	entity := &TestUser{Email: "rows@test.com", Name: "Rows", Role: "user", Active: true}
	if err := crud.Create(context.Background(), entity); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := crud.FindAll(context.Background(), QueryOpts{Page: 1, PageSize: 10}); err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}
	if err := crud.Delete(context.Background(), entity.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	byOp := map[string]SQLQueryEvent{}
	for _, ev := range events {
		byOp[ev.Operation] = ev
	}
	if got := byOp["insert"].RowsAffected; got != 1 {
		t.Fatalf("insert RowsAffected = %d, want 1", got)
	}
	if got := byOp["soft_delete"].RowsAffected; got != 1 {
		t.Fatalf("soft_delete RowsAffected = %d, want 1", got)
	}
	if got := byOp["select.list"].RowsAffected; got != 0 {
		t.Fatalf("select.list RowsAffected = %d, want 0 (not reported)", got)
	}
}
