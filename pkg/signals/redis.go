package signals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/redis/go-redis/v9"
)

const defaultRedisChannelPrefix = "nucleus:signals:"

var (
	ErrRedisURLRequired = errors.New("signals: redis url is required")
	// ErrSignalRequired is returned when a topic has an empty name: a relay
	// cannot derive a channel from it.
	ErrSignalRequired = errors.New("signals: topic name is required")
	ErrNilHandler     = errors.New("signals: handler is required")
	ErrNilBus         = errors.New("signals: bus is nil")
	ErrNilRelay       = errors.New("signals: relay is nil")
)

type RedisRelayConfig struct {
	RedisURL      string
	ChannelPrefix string
}

// RedisRelay carries a topic's events between processes over Redis pub/sub:
// one channel per topic name, the payload as JSON, and the request, user and
// trace ids of the publishing context in the envelope so the receiving
// handler gets them back on its context.
//
// A payload crosses as JSON and arrives as generic decoded data, so a typed
// subscriber — RelaySubscribe, or a Subscribe on the bus ForwardToBus feeds —
// decodes it into its topic's type the way an outbox-delivered event is
// decoded.
type RedisRelay struct {
	client        *redis.Client
	channelPrefix string
	logger        *slog.Logger
	closeOnce     sync.Once
}

// redisEnvelope is the wire format. The topic name travels under the
// "signal" key, as it did in v1, so a relay on either side of an upgrade
// reads the other's envelopes.
type redisEnvelope struct {
	Topic       string `json:"signal"`
	Payload     any    `json:"payload,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
}

func NewRedisRelay(cfg RedisRelayConfig, logger *slog.Logger) (*RedisRelay, error) {
	redisURL := strings.TrimSpace(cfg.RedisURL)
	if redisURL == "" {
		return nil, ErrRedisURLRequired
	}

	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("signals.NewRedisRelay parse redis url: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}

	prefix := strings.TrimSpace(cfg.ChannelPrefix)
	if prefix == "" {
		prefix = defaultRedisChannelPrefix
	}

	return &RedisRelay{
		client:        redis.NewClient(options),
		channelPrefix: prefix,
		logger:        logger,
	}, nil
}

func (r *RedisRelay) Close() error {
	if r == nil {
		return nil
	}
	var closeErr error
	r.closeOnce.Do(func() {
		closeErr = r.client.Close()
	})
	return closeErr
}

// Channel returns the Redis channel a topic name is relayed on.
func (r *RedisRelay) Channel(topic string) string {
	if r == nil {
		return ""
	}
	return r.channelPrefix + topic
}

// RelayPublish sends a typed event to every process subscribed to the topic
// through the relay. The request, user and trace ids are read from ctx.
func RelayPublish[T any](ctx context.Context, r *RedisRelay, topic Topic[T], payload T) error {
	return r.publish(ctx, topic.name, payload)
}

// RelaySubscribe receives the topic's events from Redis and runs the handler
// for each one, with the payload decoded into the topic's type and the
// publishing context's correlation ids restored. It is the receive loop: it
// returns when ctx ends, at the first message that does not decode, and at
// the first handler error, which it returns.
func RelaySubscribe[T any](ctx context.Context, r *RedisRelay, topic Topic[T], handler func(ctx context.Context, payload T) error) error {
	if handler == nil {
		return ErrNilHandler
	}
	return r.subscribe(ctx, topic.name, func(ctx context.Context, raw any) error {
		payload, err := coerce[T](raw)
		if err != nil {
			return fmt.Errorf("signals: %s: %w", topic.name, err)
		}
		return handler(ctx, payload)
	})
}

// ForwardToBus re-publishes, onto the local bus, every event that arrives on
// the topic's channel: the second replica's subscribers of that name run as
// if the event had been published there, decoding the relayed JSON into their
// topic's type. It is the receive loop and returns the way RelaySubscribe
// does.
func (r *RedisRelay) ForwardToBus(ctx context.Context, topic string, bus *Bus) error {
	if bus == nil {
		return ErrNilBus
	}
	return r.subscribe(ctx, topic, func(ctx context.Context, raw any) error {
		return bus.emit(event{name: topic, payload: raw, ctx: ctx})
	})
}

func (r *RedisRelay) publish(ctx context.Context, topic string, payload any) error {
	if r == nil {
		return ErrNilRelay
	}
	if strings.TrimSpace(topic) == "" {
		return ErrSignalRequired
	}

	ctx = normalizeRelayContext(ctx, nil)
	body, err := json.Marshal(encodeEnvelope(ctx, topic, payload))
	if err != nil {
		return fmt.Errorf("signals.RelayPublish: %w", err)
	}
	if err := r.client.Publish(ctx, r.Channel(topic), body).Err(); err != nil {
		return fmt.Errorf("signals.RelayPublish: %w", err)
	}
	return nil
}

func (r *RedisRelay) subscribe(ctx context.Context, topic string, handler func(context.Context, any) error) error {
	if r == nil {
		return ErrNilRelay
	}
	if strings.TrimSpace(topic) == "" {
		return ErrSignalRequired
	}
	if handler == nil {
		return ErrNilHandler
	}

	ctx = normalizeRelayContext(ctx, nil)
	pubsub := r.client.Subscribe(ctx, r.Channel(topic))
	defer pubsub.Close()

	if _, err := pubsub.Receive(ctx); err != nil {
		return fmt.Errorf("signals.RelaySubscribe: %w", err)
	}

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}

			eventCtx, payload, err := decodeEnvelope(msg.Payload)
			if err != nil {
				return fmt.Errorf("signals.RelaySubscribe decode: %w", err)
			}
			if err := handler(eventCtx, payload); err != nil {
				return fmt.Errorf("signals.RelaySubscribe handler: %w", err)
			}
		}
	}
}

func encodeEnvelope(ctx context.Context, topic string, payload any) redisEnvelope {
	out := redisEnvelope{
		Topic:   topic,
		Payload: payload,
	}
	if ctx != nil {
		out.RequestID = observe.RequestIDFromCtx(ctx)
		out.UserID = observe.UserIDFromCtx(ctx)
		out.TraceID = observe.TraceIDFromCtx(ctx)
	}
	out.PublishedAt = time.Now().UTC().Format(time.RFC3339)
	return out
}

// decodeEnvelope reads an envelope back: the context carrying the
// correlation ids it travelled with, and the payload as the JSON decoder
// leaves it (a map, a slice, a string or a number), for coerce to shape.
func decodeEnvelope(raw string) (context.Context, any, error) {
	var env redisEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(env.Topic) == "" {
		return nil, nil, ErrSignalRequired
	}

	ctx := context.Background()
	if env.RequestID != "" {
		ctx = observe.CtxWithRequestID(ctx, env.RequestID)
	}
	if env.UserID != "" {
		ctx = observe.CtxWithUserID(ctx, env.UserID)
	}
	if env.TraceID != "" {
		ctx = observe.CtxWithTraceID(ctx, env.TraceID)
	}
	return ctx, env.Payload, nil
}

func normalizeRelayContext(primary, fallback context.Context) context.Context {
	if primary != nil {
		return primary
	}
	if fallback != nil {
		return fallback
	}
	return context.Background()
}
