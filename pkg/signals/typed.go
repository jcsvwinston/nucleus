// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package signals

import (
	"context"
	"encoding/json"
	"fmt"
)

// Topic is a named event with a payload type.
//
// It exists because the untyped bus moves `any`: every handler asserts the
// type it hopes for, and a producer that changes the payload breaks its
// subscribers at run time, in production, one event at a time. A Topic makes
// the payload part of the name, so the compiler is the one that notices.
//
//	var InvoicePaid = signals.Define[InvoicePayload]("invoice.paid")
//
//	signals.Subscribe(bus, InvoicePaid, func(ctx context.Context, p InvoicePayload) error {
//	        return mailer.Send(ctx, p.CustomerEmail)
//	})
//
//	_ = signals.Publish(ctx, bus, InvoicePaid, InvoicePayload{...})
//
// It is delivered ALONGSIDE the untyped API, not instead of it: Signal, Event
// and Handler are published, and QADR-0010 holds breaking changes until the
// major at the close of A12. A typed publish reaches untyped subscribers of the
// same signal and the other way round — it is one bus, with two doors.
type Topic[T any] struct {
	name Signal
}

// Define names a topic and fixes its payload type.
func Define[T any](name string) Topic[T] { return Topic[T]{name: Signal(name)} }

// Name returns the underlying signal, which is what untyped subscribers use.
func (t Topic[T]) Name() Signal { return t.name }

// Subscribe registers a typed handler.
//
// The handler receives the context the event was published with, so work it
// starts belongs to the request or the job that caused it — the untyped API
// carries the context inside the event and most handlers ignore it.
func Subscribe[T any](b *Bus, topic Topic[T], handler func(ctx context.Context, payload T) error) {
	if b == nil || handler == nil {
		return
	}
	b.On(topic.name, func(e Event) error {
		payload, err := coerce[T](e.Payload)
		if err != nil {
			return fmt.Errorf("signals: %s: %w", topic.name, err)
		}
		ctx := e.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		return handler(ctx, payload)
	})
}

// Publish emits a typed event synchronously, returning what the handlers
// returned — the same semantics as Emit, which aborts the caller's operation
// when a subscriber refuses it.
func Publish[T any](ctx context.Context, b *Bus, topic Topic[T], payload T) error {
	if b == nil {
		return nil
	}
	return b.Emit(Event{Signal: topic.name, Payload: payload, Ctx: ctx})
}

// PublishAsync emits a typed event without waiting for its handlers.
func PublishAsync[T any](ctx context.Context, b *Bus, topic Topic[T], payload T) {
	if b == nil {
		return
	}
	b.EmitAsync(Event{Signal: topic.name, Payload: payload, Ctx: ctx})
}

// coerce turns what the bus moved into the payload the topic promises.
//
// The direct case is a value published through this same API. The other one is
// the reason this function exists at all: an event that travelled — through the
// Redis relay, or through the outbox as a transactional transport — arrives as
// decoded JSON, and a typed subscriber must not have to care which door the
// event came in by.
func coerce[T any](raw any) (T, error) {
	var zero T
	if raw == nil {
		return zero, nil
	}
	if typed, ok := raw.(T); ok {
		return typed, nil
	}
	if data, ok := raw.([]byte); ok {
		var out T
		if err := json.Unmarshal(data, &out); err != nil {
			return zero, fmt.Errorf("payload is not %T: %w", zero, err)
		}
		return out, nil
	}
	// A map or a slice from a JSON decoder: round-trip it into the wanted
	// shape rather than refusing an event that is the right thing in the
	// wrong representation.
	data, err := json.Marshal(raw)
	if err != nil {
		return zero, fmt.Errorf("payload is %T, not %T", raw, zero)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return zero, fmt.Errorf("payload is %T, not %T", raw, zero)
	}
	return out, nil
}
