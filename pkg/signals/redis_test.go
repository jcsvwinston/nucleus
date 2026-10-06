package signals

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/internal/testredis"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

func TestNewRedisRelayRequiresRedisURL(t *testing.T) {
	_, err := NewRedisRelay(RedisRelayConfig{}, nil)
	if !errors.Is(err, ErrRedisURLRequired) {
		t.Fatalf("expected ErrRedisURLRequired, got %v", err)
	}
}

func TestRedisRelayPublishSubscribe(t *testing.T) {
	redisServer := testredis.Run(t)

	relay, err := NewRedisRelay(RedisRelayConfig{
		RedisURL: "redis://" + redisServer.Addr(),
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewRedisRelay failed: %v", err)
	}
	defer relay.Close()

	received := make(chan Event, 1)
	errCh := make(chan error, 1)

	subCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		errCh <- relay.Subscribe(subCtx, PostCreate, func(event Event) error {
			select {
			case received <- event:
			default:
			}
			cancel()
			return nil
		})
	}()

	time.Sleep(20 * time.Millisecond)

	pubCtx := observe.CtxWithRequestID(context.Background(), "req-1")
	pubCtx = observe.CtxWithUserID(pubCtx, "user-7")
	pubCtx = observe.CtxWithTraceID(pubCtx, "trace-9")

	err = relay.Publish(pubCtx, Event{
		Signal:    PostCreate,
		ModelName: "Article",
		Payload: map[string]any{
			"id":    42,
			"title": "hello",
		},
	})
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case event := <-received:
		if event.Signal != PostCreate {
			t.Fatalf("signal = %q, want %q", event.Signal, PostCreate)
		}
		if event.ModelName != "Article" {
			t.Fatalf("model name = %q, want Article", event.ModelName)
		}
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			t.Fatalf("payload type = %T, want map[string]any", event.Payload)
		}
		if payload["title"] != "hello" {
			t.Fatalf("title = %#v, want hello", payload["title"])
		}
		if observe.RequestIDFromCtx(event.Ctx) != "req-1" {
			t.Fatalf("request id = %q, want req-1", observe.RequestIDFromCtx(event.Ctx))
		}
		if observe.UserIDFromCtx(event.Ctx) != "user-7" {
			t.Fatalf("user id = %q, want user-7", observe.UserIDFromCtx(event.Ctx))
		}
		if observe.TraceIDFromCtx(event.Ctx) != "trace-9" {
			t.Fatalf("trace id = %q, want trace-9", observe.TraceIDFromCtx(event.Ctx))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for relayed event")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Subscribe returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscribe loop to stop")
	}
}

func TestRedisRelayForwardToBus(t *testing.T) {
	redisServer := testredis.Run(t)

	relay, err := NewRedisRelay(RedisRelayConfig{
		RedisURL:      "redis://" + redisServer.Addr(),
		ChannelPrefix: "test:signals:",
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewRedisRelay failed: %v", err)
	}
	defer relay.Close()

	bus := NewBus(slog.Default())
	received := make(chan Event, 1)
	bus.On(PostUpdate, func(event Event) error {
		received <- event
		return nil
	})

	subCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- relay.ForwardToBus(subCtx, PostUpdate, bus)
	}()

	time.Sleep(20 * time.Millisecond)

	if err := relay.Publish(context.Background(), Event{
		Signal:    PostUpdate,
		ModelName: "User",
		Payload: map[string]any{
			"id": 99,
		},
	}); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case event := <-received:
		if event.Signal != PostUpdate {
			t.Fatalf("signal = %q, want %q", event.Signal, PostUpdate)
		}
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for bus event")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ForwardToBus returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for forwarder to stop")
	}
}

func TestNormalizeRelayContext(t *testing.T) {
	ctx1 := context.WithValue(context.Background(), "key", "value1")
	ctx2 := context.WithValue(context.Background(), "key", "value2")

	// Primary context takes precedence
	if got := normalizeRelayContext(ctx1, ctx2); got != ctx1 {
		t.Error("expected primary context to be returned")
	}

	// Fallback context used when primary is nil
	if got := normalizeRelayContext(nil, ctx2); got != ctx2 {
		t.Error("expected fallback context to be returned when primary is nil")
	}

	// Background context when both are nil
	if got := normalizeRelayContext(nil, nil); got == nil {
		t.Error("expected background context when both are nil")
	}
}
