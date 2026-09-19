// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package signals

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type invoice struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

var invoicePaid = Define[invoice]("invoice.paid")

// The payload arrives typed, with no assertion and no hoping.
func TestTyped_PublishAndSubscribe(t *testing.T) {
	bus := NewBus(discard())
	got := make(chan invoice, 1)
	Subscribe(bus, invoicePaid, func(_ context.Context, p invoice) error {
		got <- p
		return nil
	})
	if err := Publish(context.Background(), bus, invoicePaid, invoice{ID: "inv-1", Amount: 42}); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-got:
		if p.ID != "inv-1" || p.Amount != 42 {
			t.Fatalf("got %+v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("the typed handler never ran")
	}
}

// One bus with two doors: a typed subscriber hears an untyped emit of the same
// signal, and the other way round. Anything else would make the typed API a
// second bus, which is the problem it exists to remove.
func TestTyped_InteropsWithTheUntypedAPI(t *testing.T) {
	bus := NewBus(discard())
	typed := make(chan invoice, 1)
	Subscribe(bus, invoicePaid, func(_ context.Context, p invoice) error {
		typed <- p
		return nil
	})
	untyped := make(chan Event, 1)
	bus.On(invoicePaid.Name(), func(e Event) error {
		untyped <- e
		return nil
	})

	if err := bus.Emit(Event{Signal: invoicePaid.Name(), Payload: invoice{ID: "inv-2", Amount: 7}}); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-typed:
		if p.ID != "inv-2" {
			t.Fatalf("typed handler got %+v from an untyped emit", p)
		}
	case <-time.After(time.Second):
		t.Fatal("an untyped emit did not reach the typed subscriber")
	}
	select {
	case <-untyped:
	case <-time.After(time.Second):
		t.Fatal("the untyped subscriber did not run")
	}
}

// An event that travelled — the Redis relay, or the outbox as a transactional
// transport — arrives as JSON. A typed subscriber must not have to care which
// door it came in by.
func TestTyped_AcceptsAPayloadThatTravelled(t *testing.T) {
	bus := NewBus(discard())
	got := make(chan invoice, 1)
	Subscribe(bus, invoicePaid, func(_ context.Context, p invoice) error {
		got <- p
		return nil
	})

	raw, err := json.Marshal(invoice{ID: "inv-3", Amount: 99})
	if err != nil {
		t.Fatal(err)
	}
	// As the outbox stores it: bytes.
	if err := bus.Emit(Event{Signal: invoicePaid.Name(), Payload: raw}); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-got:
		if p.Amount != 99 {
			t.Fatalf("got %+v from a JSON payload", p)
		}
	case <-time.After(time.Second):
		t.Fatal("a JSON payload did not reach the typed subscriber")
	}

	// And as a JSON decoder leaves it: a map.
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatal(err)
	}
	if err := bus.Emit(Event{Signal: invoicePaid.Name(), Payload: asMap}); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-got:
		if p.ID != "inv-3" {
			t.Fatalf("got %+v from a decoded map", p)
		}
	case <-time.After(time.Second):
		t.Fatal("a decoded map did not reach the typed subscriber")
	}
}

// A payload that is genuinely the wrong shape is an error the publisher's
// operation sees, not a silent no-op.
func TestTyped_WrongShapeIsAnError(t *testing.T) {
	bus := NewBus(discard())
	Subscribe(bus, invoicePaid, func(context.Context, invoice) error { return nil })
	if err := bus.Emit(Event{Signal: invoicePaid.Name(), Payload: "not an invoice"}); err == nil {
		t.Fatal("a payload of the wrong type was accepted silently")
	}
}

// The context travels with the event, so work a handler starts belongs to the
// request or job that caused it.
func TestTyped_CarriesTheContext(t *testing.T) {
	type key string
	const tenant key = "tenant"

	bus := NewBus(discard())
	seen := make(chan string, 1)
	Subscribe(bus, invoicePaid, func(ctx context.Context, _ invoice) error {
		v, _ := ctx.Value(tenant).(string)
		seen <- v
		return nil
	})
	ctx := context.WithValue(context.Background(), tenant, "acme")
	if err := Publish(ctx, bus, invoicePaid, invoice{ID: "inv-4"}); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "acme" {
		t.Fatalf("the handler saw tenant %q", got)
	}
}
