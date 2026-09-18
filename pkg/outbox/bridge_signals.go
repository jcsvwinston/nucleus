// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package outbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/jcsvwinston/nucleus/pkg/signals"
)

// BusBridge delivers outbox messages onto the in-process event bus.
//
// It is what makes the outbox a TRANSPORT of the bus rather than a third,
// separate way of telling something that happened. Until it existed, an outbox
// topic and a signal of the same name were unrelated: pkg/outbox delivered to
// external bridges, pkg/signals to in-process handlers, and an application had
// to choose a transport instead of a subscription — pick the outbox and your
// in-process listeners never hear it; pick the bus and the event dies with the
// transaction that produced it.
//
// With this registered, an event published inside a transaction reaches the
// same handlers as one published directly, once the commit lands:
//
//	store.EnqueueTx(ctx, tx, outbox.Entry{Topic: "invoice.paid", Payload: p})
//
//	signals.Subscribe(bus, InvoicePaid, func(ctx context.Context, p Payload) error { ... })
//
// The payload arrives as the JSON the outbox stored, which the typed API
// decodes into the topic's type; an untyped handler receives the raw bytes.
type BusBridge struct {
	name string
	bus  *signals.Bus
}

// NewBusBridge builds the bridge. The name is what a route points at.
func NewBusBridge(name string, bus *signals.Bus) (*BusBridge, error) {
	if name == "" {
		return nil, errors.New("outbox: bus bridge needs a name")
	}
	if bus == nil {
		return nil, errors.New("outbox: bus bridge needs a bus")
	}
	return &BusBridge{name: name, bus: bus}, nil
}

func (b *BusBridge) Name() string { return b.name }

// Send emits the message on the bus, synchronously.
//
// Synchronously on purpose: the dispatcher decides what a failed delivery
// means — retry with backoff, and the dead letter when the attempts run out —
// and it can only do that if the handlers' error reaches it. An async emit
// would report success for work nobody did.
func (b *BusBridge) Send(ctx context.Context, msg Message) error {
	if b == nil || b.bus == nil {
		return errors.New("outbox: bus bridge is not configured")
	}
	if err := b.bus.Emit(signals.Event{
		Signal:  signals.Signal(msg.Topic),
		Payload: msg.Payload,
		Ctx:     ctx,
	}); err != nil {
		return fmt.Errorf("outbox: bus bridge %s: %w", b.name, err)
	}
	return nil
}

// Healthy reports the bridge is usable: a bus needs nothing to be reachable.
func (b *BusBridge) Healthy(context.Context) error {
	if b == nil || b.bus == nil {
		return errors.New("outbox: bus bridge is not configured")
	}
	return nil
}

// Close releases nothing: the bus outlives the bridge.
func (b *BusBridge) Close() error { return nil }
