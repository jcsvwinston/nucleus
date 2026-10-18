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
// It exists because a bus that moves `any` makes every handler assert the
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
// The bus is keyed by name: two topics with the same name reach the same
// subscribers, whatever their type parameter, and the subscriber's side
// decodes what arrives (see coerce). That is how an event that travelled —
// through the Redis relay, or through the outbox as a transactional transport
// — reaches a typed handler as the type it declared.
type Topic[T any] struct {
	name string
}

// Define names a topic and fixes its payload type.
func Define[T any](name string) Topic[T] { return Topic[T]{name: name} }

// String returns the topic's name: the string Define was given. It is what
// Bus.Clear and RedisRelay.ForwardToBus take.
func (t Topic[T]) String() string { return t.name }

// Subscribe registers a typed handler.
//
// The handler receives the context the event was published with, so work it
// starts belongs to the request or the job that caused it.
func Subscribe[T any](b *Bus, topic Topic[T], handler func(ctx context.Context, payload T) error) {
	if b == nil || handler == nil {
		return
	}
	b.on(topic.name, func(e event) error {
		payload, err := coerce[T](e.payload)
		if err != nil {
			return fmt.Errorf("signals: %s: %w", topic.name, err)
		}
		ctx := e.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		return handler(ctx, payload)
	})
}

// Publish dispatches a typed event synchronously, returning what the handlers
// returned: the handlers run in registration order, the first error stops the
// chain and reaches the caller, so a subscriber can refuse the operation that
// published the event. A panicking handler is returned as ErrHandlerPanic.
func Publish[T any](ctx context.Context, b *Bus, topic Topic[T], payload T) error {
	if b == nil {
		return nil
	}
	return b.emit(event{name: topic.name, payload: payload, ctx: ctx})
}

// PublishAsync dispatches a typed event without waiting for its handlers.
// Errors and panics are logged, not returned; past the bus's in-flight limit
// a dispatch is dropped and counted (Bus.Dropped).
func PublishAsync[T any](ctx context.Context, b *Bus, topic Topic[T], payload T) {
	if b == nil {
		return
	}
	b.emitAsync(event{name: topic.name, payload: payload, ctx: ctx})
}

// coerce turns what the bus moved into the payload the topic promises.
//
// The direct case is a value published through this same API. The other one is
// the reason this function exists at all: an event that travelled — through the
// Redis relay, or through the outbox as a transactional transport — arrives as
// decoded JSON or as the JSON bytes themselves, and a typed subscriber must not
// have to care which door the event came in by.
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
