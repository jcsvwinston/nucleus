package signals

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestBus_OnAndEmit(t *testing.T) {
	bus := NewBus(slog.Default())
	var called int
	bus.On(PreCreate, func(e Event) error {
		called++
		return nil
	})

	err := bus.Emit(Event{Signal: PreCreate, ModelName: "User", Ctx: context.Background()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 1 {
		t.Errorf("expected 1 call, got %d", called)
	}
}

func TestBus_EmitPropagatesError(t *testing.T) {
	bus := NewBus(slog.Default())
	bus.On(PreSave, func(e Event) error {
		return errors.New("blocked")
	})
	bus.On(PreSave, func(e Event) error {
		t.Error("second handler should not be called")
		return nil
	})

	err := bus.Emit(Event{Signal: PreSave, Ctx: context.Background()})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBus_EmitAsync(t *testing.T) {
	bus := NewBus(slog.Default())
	var count atomic.Int32
	bus.On(PostCreate, func(e Event) error {
		count.Add(1)
		return nil
	})

	bus.EmitAsync(Event{Signal: PostCreate, Ctx: context.Background()})
	time.Sleep(50 * time.Millisecond)
	if count.Load() != 1 {
		t.Errorf("expected 1, got %d", count.Load())
	}
}

func TestBus_EmitNoHandlers(t *testing.T) {
	bus := NewBus(slog.Default())
	err := bus.Emit(Event{Signal: PreDelete, Ctx: context.Background()})
	if err != nil {
		t.Fatalf("expected no error for unregistered signal, got: %v", err)
	}
}

func TestBus_Clear(t *testing.T) {
	bus := NewBus(slog.Default())
	bus.On(PreCreate, func(e Event) error { return errors.New("fail") })
	bus.Clear(PreCreate)

	err := bus.Emit(Event{Signal: PreCreate, Ctx: context.Background()})
	if err != nil {
		t.Fatal("expected no error after clear")
	}
}

func TestBus_ClearAll(t *testing.T) {
	bus := NewBus(slog.Default())
	bus.On(PreCreate, func(e Event) error { return errors.New("fail") })
	bus.On(PreDelete, func(e Event) error { return errors.New("fail") })
	bus.Clear()

	if err := bus.Emit(Event{Signal: PreCreate, Ctx: context.Background()}); err != nil {
		t.Fatal("expected no error after clear all")
	}
	if err := bus.Emit(Event{Signal: PreDelete, Ctx: context.Background()}); err != nil {
		t.Fatal("expected no error after clear all")
	}
}
