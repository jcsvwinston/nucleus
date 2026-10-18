package signals

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type userEvent struct {
	Name string
}

var (
	userCreated = Define[userEvent]("user.created")
	userSaved   = Define[userEvent]("user.saved")
	userDeleted = Define[userEvent]("user.deleted")
)

func TestBus_SubscribeAndPublish(t *testing.T) {
	bus := NewBus(slog.Default())
	var called int
	Subscribe(bus, userCreated, func(context.Context, userEvent) error {
		called++
		return nil
	})

	err := Publish(context.Background(), bus, userCreated, userEvent{Name: "User"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 1 {
		t.Errorf("expected 1 call, got %d", called)
	}
}

func TestBus_PublishPropagatesError(t *testing.T) {
	bus := NewBus(slog.Default())
	Subscribe(bus, userSaved, func(context.Context, userEvent) error {
		return errors.New("blocked")
	})
	Subscribe(bus, userSaved, func(context.Context, userEvent) error {
		t.Error("second handler should not be called")
		return nil
	})

	err := Publish(context.Background(), bus, userSaved, userEvent{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBus_PublishAsync(t *testing.T) {
	bus := NewBus(slog.Default())
	var count atomic.Int32
	Subscribe(bus, userCreated, func(context.Context, userEvent) error {
		count.Add(1)
		return nil
	})

	PublishAsync(context.Background(), bus, userCreated, userEvent{})
	time.Sleep(50 * time.Millisecond)
	if count.Load() != 1 {
		t.Errorf("expected 1, got %d", count.Load())
	}
}

func TestBus_PublishNoHandlers(t *testing.T) {
	bus := NewBus(slog.Default())
	err := Publish(context.Background(), bus, userDeleted, userEvent{})
	if err != nil {
		t.Fatalf("expected no error for a topic nobody subscribed to, got: %v", err)
	}
}

// Clear takes topic names: the string Define was given, which is what
// Topic.String returns.
func TestBus_Clear(t *testing.T) {
	bus := NewBus(slog.Default())
	Subscribe(bus, userCreated, func(context.Context, userEvent) error { return errors.New("fail") })
	bus.Clear(userCreated.String())

	err := Publish(context.Background(), bus, userCreated, userEvent{})
	if err != nil {
		t.Fatal("expected no error after clear")
	}
}

func TestBus_ClearAll(t *testing.T) {
	bus := NewBus(slog.Default())
	Subscribe(bus, userCreated, func(context.Context, userEvent) error { return errors.New("fail") })
	Subscribe(bus, userDeleted, func(context.Context, userEvent) error { return errors.New("fail") })
	bus.Clear()

	if err := Publish(context.Background(), bus, userCreated, userEvent{}); err != nil {
		t.Fatal("expected no error after clear all")
	}
	if err := Publish(context.Background(), bus, userDeleted, userEvent{}); err != nil {
		t.Fatal("expected no error after clear all")
	}
}
