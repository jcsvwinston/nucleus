package quark

import (
	"context"
	"fmt"
)

// EventPayload represents a message received from a database event channel.
type EventPayload struct {
	Channel string
	Payload string
}

// EventListener defines an interface for listening to database events.
// This is typically implemented via PubSub mechanisms like PostgreSQL's LISTEN/NOTIFY.
type EventListener interface {
	// Listen subscribes to a specific channel.
	Listen(ctx context.Context, channel string) error

	// Unlisten unsubscribes from a channel.
	Unlisten(ctx context.Context, channel string) error

	// Receive blocks until an event is received, returning the payload or an error.
	Receive(ctx context.Context) (EventPayload, error)

	// Close terminates the listener connection.
	Close() error
}

// EventBus provides a dialect-agnostic factory for creating EventListeners.
// Since not all databases support PubSub natively (e.g., SQLite), this may return
// ErrNotSupported for certain dialects.
type EventBus struct {
	client *Client
}

// NewEventBus creates a new EventBus for the given client.
func NewEventBus(client *Client) *EventBus {
	return &EventBus{client: client}
}

// CreateListener creates an EventListener based on the dialect.
func (eb *EventBus) CreateListener() (EventListener, error) {
	// In a real implementation, we would check the dialect and return
	// a specific listener implementation (e.g., using github.com/lib/pq for Postgres).
	// For V1, we return a mock or an error indicating lack of native driver integration.
	
	switch eb.client.dialect.Name() {
	case "postgres":
		// Here you would instantiate a Postgres specific listener using the active connection config.
		// Example: return newPostgresListener(eb.client.dsn), nil
		return nil, fmt.Errorf("native postgres listen/notify requires specific driver integration not yet mapped in V1")
	default:
		return nil, fmt.Errorf("event listeners are not natively supported by the %s dialect", eb.client.dialect.Name())
	}
}

// Notify is a helper to trigger a database event (e.g., NOTIFY in Postgres).
func Notify(ctx context.Context, provider ClientProvider, channel, payload string) error {
	client, err := provider.GetClient(ctx)
	if err != nil {
		return err
	}

	if err := client.guard.ValidateIdentifier(channel); err != nil {
		return fmt.Errorf("invalid channel name: %w", err)
	}

	var sqlStr string
	switch client.dialect.Name() {
	case "postgres":
		// In Postgres, payload must be a string literal, we use bound parameters if supported
		// However, NOTIFY command doesn't typically support prepared parameters in pq.
		// For safety, we use pg_notify function which DOES support parameters.
		sqlStr = "SELECT pg_notify($1, $2)"
		_, err = client.db.ExecContext(ctx, sqlStr, channel, payload)
	case "mysql":
		// MySQL doesn't have PubSub. This could fall back to a dummy or error.
		return fmt.Errorf("notify not supported in MySQL")
	case "sqlite":
		return fmt.Errorf("notify not supported in SQLite")
	default:
		return fmt.Errorf("notify not supported by dialect")
	}

	return err
}
