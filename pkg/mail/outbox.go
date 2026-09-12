package mail

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

// OutboxTopic is the topic mail is queued under.
const OutboxTopic = "nucleus.mail"

// EnqueueTx queues a message for delivery INSIDE the caller's transaction,
// which is the only way a verification email and the row it announces can
// agree. Sending straight from a handler has a window that no retry closes:
// the database commits, the process dies, and the address is verified for an
// account whose owner never got the link — or the mail goes out and the
// commit rolls back, and the link points at nothing.
//
// The message is delivered later by the dispatcher, through the bridge
// NewOutboxBridge returns, with the outbox's own retry and backoff.
func EnqueueTx(ctx context.Context, store *outbox.Store, tx *sql.Tx, msg Message) (outbox.Message, error) {
	if store == nil {
		return outbox.Message{}, fmt.Errorf("mail: nil outbox store")
	}
	if err := validateMessage(msg); err != nil {
		return outbox.Message{}, err
	}
	// The message goes in as a VALUE, not as pre-encoded bytes: the store
	// marshals the payload itself, and handing it a []byte would store a
	// base64 string of JSON that nothing on the way out expects.
	return store.EnqueueTx(ctx, tx, outbox.Entry{Topic: OutboxTopic, Payload: msg})
}

// Enqueue queues a message outside a transaction. Prefer EnqueueTx whenever
// the mail accompanies a write: this one has the same window as sending
// directly, minus the retry.
func Enqueue(ctx context.Context, store *outbox.Store, msg Message) (outbox.Message, error) {
	if store == nil {
		return outbox.Message{}, fmt.Errorf("mail: nil outbox store")
	}
	if err := validateMessage(msg); err != nil {
		return outbox.Message{}, err
	}
	return store.Enqueue(ctx, outbox.Entry{Topic: OutboxTopic, Payload: msg})
}

// OutboxBridge delivers queued mail through a Sender. Register it on the
// outbox router for OutboxTopic and the dispatcher does the rest.
type OutboxBridge struct {
	sender Sender
	name   string
}

// NewOutboxBridge wraps a sender as an outbox bridge.
func NewOutboxBridge(sender Sender) *OutboxBridge {
	return &OutboxBridge{sender: sender, name: "mail"}
}

// Name implements outbox.Bridge.
func (b *OutboxBridge) Name() string { return b.name }

// Send implements outbox.Bridge: it decodes the queued message and hands it
// to the sender. A payload that does not decode is returned as an error, so
// it lands in the outbox's failed state with its reason instead of being
// dropped.
func (b *OutboxBridge) Send(ctx context.Context, msg outbox.Message) error {
	var decoded Message
	if err := json.Unmarshal(msg.Payload, &decoded); err != nil {
		return fmt.Errorf("mail: decode queued message %s: %w", msg.ID, err)
	}
	return b.sender.Send(ctx, decoded)
}

// Healthy implements outbox.Bridge, forwarding to the sender when it knows
// how to answer.
func (b *OutboxBridge) Healthy(ctx context.Context) error {
	if hc, ok := b.sender.(HealthChecker); ok {
		return hc.Healthy(ctx)
	}
	return nil
}

// Close implements outbox.Bridge. A sender owns no connection of its own —
// SMTP dials per message — so there is nothing to release.
func (b *OutboxBridge) Close() error { return nil }
