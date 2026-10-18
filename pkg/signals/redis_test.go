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

type article struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

var (
	articleCreated = Define[article]("article.created")
	articleUpdated = Define[article]("article.updated")
)

func TestNewRedisRelayRequiresRedisURL(t *testing.T) {
	_, err := NewRedisRelay(RedisRelayConfig{}, nil)
	if !errors.Is(err, ErrRedisURLRequired) {
		t.Fatalf("expected ErrRedisURLRequired, got %v", err)
	}
}

func TestRelayPublishRefusesAnEmptyTopicName(t *testing.T) {
	redisServer := testredis.Run(t)
	relay, err := NewRedisRelay(RedisRelayConfig{RedisURL: "redis://" + redisServer.Addr()}, slog.Default())
	if err != nil {
		t.Fatalf("NewRedisRelay failed: %v", err)
	}
	defer relay.Close()
	if err := RelayPublish(context.Background(), relay, Define[article](""), article{}); !errors.Is(err, ErrSignalRequired) {
		t.Fatalf("RelayPublish on an unnamed topic: %v, want ErrSignalRequired", err)
	}
	if err := RelaySubscribe[article](context.Background(), relay, articleCreated, nil); !errors.Is(err, ErrNilHandler) {
		t.Fatalf("RelaySubscribe with a nil handler: %v, want ErrNilHandler", err)
	}
}

// A typed event crosses Redis and arrives typed, with the correlation ids of
// the publishing context back on the handler's context.
func TestRedisRelayPublishSubscribe(t *testing.T) {
	redisServer := testredis.Run(t)

	relay, err := NewRedisRelay(RedisRelayConfig{
		RedisURL: "redis://" + redisServer.Addr(),
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewRedisRelay failed: %v", err)
	}
	defer relay.Close()

	type received struct {
		ctx     context.Context
		payload article
	}
	got := make(chan received, 1)
	errCh := make(chan error, 1)

	subCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		errCh <- RelaySubscribe(subCtx, relay, articleCreated, func(ctx context.Context, p article) error {
			select {
			case got <- received{ctx: ctx, payload: p}:
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

	if err := RelayPublish(pubCtx, relay, articleCreated, article{ID: 42, Title: "hello"}); err != nil {
		t.Fatalf("RelayPublish failed: %v", err)
	}

	select {
	case r := <-got:
		if r.payload.ID != 42 || r.payload.Title != "hello" {
			t.Fatalf("payload = %+v, want {42 hello}", r.payload)
		}
		if observe.RequestIDFromCtx(r.ctx) != "req-1" {
			t.Fatalf("request id = %q, want req-1", observe.RequestIDFromCtx(r.ctx))
		}
		if observe.UserIDFromCtx(r.ctx) != "user-7" {
			t.Fatalf("user id = %q, want user-7", observe.UserIDFromCtx(r.ctx))
		}
		if observe.TraceIDFromCtx(r.ctx) != "trace-9" {
			t.Fatalf("trace id = %q, want trace-9", observe.TraceIDFromCtx(r.ctx))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for relayed event")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("RelaySubscribe returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscribe loop to stop")
	}
}

// What arrives on the channel is re-published on the local bus by name, and
// the bus's typed subscriber decodes the relayed JSON into its own type.
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
	received := make(chan article, 1)
	Subscribe(bus, articleUpdated, func(_ context.Context, p article) error {
		received <- p
		return nil
	})

	subCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- relay.ForwardToBus(subCtx, articleUpdated.String(), bus)
	}()

	time.Sleep(20 * time.Millisecond)

	if err := RelayPublish(context.Background(), relay, articleUpdated, article{ID: 99}); err != nil {
		t.Fatalf("RelayPublish failed: %v", err)
	}

	select {
	case p := <-received:
		if p.ID != 99 {
			t.Fatalf("forwarded payload = %+v, want ID 99", p)
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
