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

// An event that travelled — the Redis relay, or the outbox as a transactional
// transport — arrives as JSON. The bus is keyed by name, so a transport
// publishes under the same name with the payload it has, and the typed
// subscriber must not have to care which door it came in by.
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
	// As the outbox delivers it: the stored bytes, under the topic's name.
	asBytes := Define[[]byte](invoicePaid.String())
	if err := Publish(context.Background(), bus, asBytes, raw); err != nil {
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

	// And as the relay delivers it: what a JSON decoder leaves, a map.
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatal(err)
	}
	decoded := Define[map[string]any](invoicePaid.String())
	if err := Publish(context.Background(), bus, decoded, asMap); err != nil {
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
	notAnInvoice := Define[string](invoicePaid.String())
	if err := Publish(context.Background(), bus, notAnInvoice, "not an invoice"); err == nil {
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

// String is the way to read a topic's name: what Define was given, and what
// Bus.Clear and RedisRelay.ForwardToBus take.
func TestTyped_StringIsTheTopicName(t *testing.T) {
	topic := Define[invoice]("invoice.paid")
	if topic.String() != "invoice.paid" {
		t.Fatalf("String() = %q, want invoice.paid", topic.String())
	}
}

// The typed functions are no-ops on a nil bus rather than a panic: the bus is
// application-owned, and the place to check it is where it is built.
func TestTyped_NilBusIsANoOp(t *testing.T) {
	Subscribe[invoice](nil, invoicePaid, func(context.Context, invoice) error { return nil })
	if err := Publish(context.Background(), nil, invoicePaid, invoice{}); err != nil {
		t.Fatalf("Publish on a nil bus: %v", err)
	}
	PublishAsync(context.Background(), nil, invoicePaid, invoice{})
}
