// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// recorder is a Provider that records what reached it.
type recorder struct {
	checkouts []CheckoutRequest
	customers []CustomerRequest
	portals   []PortalRequest
	event     Event
	parseErr  error
}

func (r *recorder) CreateCustomer(_ context.Context, req CustomerRequest) (Customer, error) {
	r.customers = append(r.customers, req)
	return Customer{ID: "cus_1", Email: req.Email, Reference: req.Reference}, nil
}

func (r *recorder) CreateCheckout(_ context.Context, req CheckoutRequest) (Checkout, error) {
	r.checkouts = append(r.checkouts, req)
	return Checkout{ID: "cs_1", URL: "https://checkout.example/cs_1"}, nil
}

func (r *recorder) CreatePortal(_ context.Context, req PortalRequest) (Portal, error) {
	r.portals = append(r.portals, req)
	return Portal{URL: "https://portal.example/p_1"}, nil
}

func (r *recorder) Subscription(_ context.Context, id string) (Subscription, error) {
	if id == "sub_missing" {
		return Subscription{}, ErrNotFound
	}
	return Subscription{ID: id, Status: StatusActive}, nil
}

func (r *recorder) ParseWebhook(http.Header, []byte) (Event, error) {
	return r.event, r.parseErr
}

// A request the provider would refuse, or would turn into a page that sends
// the customer somewhere relative, is refused before it leaves.
func TestBilling_ValidatesRequestsBeforeTheProvider(t *testing.T) {
	ok := CheckoutRequest{Price: "price_1", SuccessURL: "https://app.example/ok", CancelURL: "https://app.example/no"}
	cases := []struct {
		name string
		call func(b *Billing) error
	}{
		{"checkout without a price", func(b *Billing) error {
			r := ok
			r.Price = " "
			_, err := b.CreateCheckout(t.Context(), r)
			return err
		}},
		{"checkout with a relative success URL", func(b *Billing) error {
			r := ok
			r.SuccessURL = "/billing/done"
			_, err := b.CreateCheckout(t.Context(), r)
			return err
		}},
		{"checkout with a javascript cancel URL", func(b *Billing) error {
			r := ok
			r.CancelURL = "javascript:alert(1)"
			_, err := b.CreateCheckout(t.Context(), r)
			return err
		}},
		{"checkout with a negative quantity", func(b *Billing) error {
			r := ok
			r.Quantity = -1
			_, err := b.CreateCheckout(t.Context(), r)
			return err
		}},
		{"checkout with negative trial days", func(b *Billing) error {
			r := ok
			r.TrialDays = -3
			_, err := b.CreateCheckout(t.Context(), r)
			return err
		}},
		{"customer without an email", func(b *Billing) error {
			_, err := b.CreateCustomer(t.Context(), CustomerRequest{Name: "Ada"})
			return err
		}},
		{"portal without a customer", func(b *Billing) error {
			_, err := b.CreatePortal(t.Context(), PortalRequest{ReturnURL: "https://app.example/account"})
			return err
		}},
		{"portal with a relative return URL", func(b *Billing) error {
			_, err := b.CreatePortal(t.Context(), PortalRequest{Customer: "cus_1", ReturnURL: "account"})
			return err
		}},
		{"subscription without an id", func(b *Billing) error {
			_, err := b.Subscription(t.Context(), "")
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &recorder{}
			err := c.call(New("fake", p))
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err = %v, want ErrInvalidRequest", err)
			}
			if len(p.checkouts)+len(p.customers)+len(p.portals) != 0 {
				t.Fatal("the request reached the provider")
			}
		})
	}

	p := &recorder{}
	co, err := New("fake", p).CreateCheckout(t.Context(), ok)
	if err != nil || co.URL == "" {
		t.Fatalf("a valid checkout: %+v %v", co, err)
	}
	if got := p.checkouts[0].Quantity; got != 1 {
		t.Fatalf("a checkout without a quantity reached the provider with %d, want 1", got)
	}
}

// ParseWebhook returns what a handler can rely on: an id, a type the seam
// carries, the part of the event the type promises, and the provider's name.
func TestBilling_ParseWebhookChecksTheProvidersEvent(t *testing.T) {
	sub := &Subscription{ID: "sub_1", Status: StatusActive}
	cases := []struct {
		name  string
		event Event
		ok    bool
	}{
		{"a subscription event", Event{ID: "evt_1", Type: SubscriptionUpdated, Subscription: sub}, true},
		{"a payment failure", Event{ID: "evt_2", Type: PaymentFailed, Payment: &Payment{Invoice: "in_1"}}, true},
		{"no id", Event{Type: SubscriptionUpdated, Subscription: sub}, false},
		{"a type the seam does not carry", Event{ID: "evt_3", Type: "billing.invoice.paid", Subscription: sub}, false},
		{"a subscription event without the subscription", Event{ID: "evt_4", Type: SubscriptionCreated}, false},
		{"a payment failure without the payment", Event{ID: "evt_5", Type: PaymentFailed}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, err := New("Fake", &recorder{event: c.event}).ParseWebhook(nil, nil)
			if c.ok != (err == nil) {
				t.Fatalf("err = %v, want ok=%v", err, c.ok)
			}
			if c.ok && ev.Provider != "fake" {
				t.Fatalf("Provider = %q, want the registered name", ev.Provider)
			}
		})
	}

	// The provider's verdict passes through untouched, so the route can
	// tell a forgery from an event it does not carry.
	for _, want := range []error{ErrSignature, ErrUnsupportedEvent} {
		_, err := New("fake", &recorder{parseErr: want}).ParseWebhook(nil, nil)
		if !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
	}
}

// Deliver runs every handler of the type, in order, and reports every
// failure — a panic included — so the outbox retries the event.
func TestBilling_DeliverRunsTheHandlersOfTheType(t *testing.T) {
	b := New("fake", &recorder{})
	var ran []string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(b.On(SubscriptionUpdated, func(context.Context, Event) error { ran = append(ran, "first"); return nil }))
	must(b.On(SubscriptionUpdated, func(context.Context, Event) error { ran = append(ran, "panics"); panic("a nil map") }))
	must(b.On(SubscriptionUpdated, func(context.Context, Event) error {
		ran = append(ran, "fails")
		return errors.New("the database is gone")
	}))
	must(b.On(SubscriptionCanceled, func(context.Context, Event) error { ran = append(ran, "other type"); return nil }))

	err := b.Deliver(t.Context(), Event{ID: "evt_1", Type: SubscriptionUpdated})
	if strings.Join(ran, ",") != "first,panics,fails" {
		t.Fatalf("ran %v", ran)
	}
	if err == nil || !strings.Contains(err.Error(), "a nil map") || !strings.Contains(err.Error(), "the database is gone") {
		t.Fatalf("Deliver = %v, want both failures", err)
	}

	ran = nil
	if err := b.Deliver(t.Context(), Event{ID: "evt_2", Type: PaymentFailed}); err != nil || len(ran) != 0 {
		t.Fatalf("an event nobody handles: ran %v err %v", ran, err)
	}

	if err := b.On("billing.invoice.paid", func(context.Context, Event) error { return nil }); err == nil {
		t.Fatal("On accepted a type the seam does not carry")
	}
	if err := b.On(SubscriptionUpdated, nil); err == nil {
		t.Fatal("On accepted a nil handler")
	}
}

func TestRegistry(t *testing.T) {
	factory := func(cfg Config) (Provider, error) {
		var c struct {
			Key string `koanf:"key" default:"env:FAKE_KEY"`
		}
		if err := cfg.Bind(&c); err != nil {
			return nil, err
		}
		if c.Key == "refuse" {
			return nil, errors.New("refused")
		}
		return &recorder{}, nil
	}
	if err := Register("Fake", factory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Unregister("fake") })

	if err := Register("fake", factory); err == nil {
		t.Fatal("a second registration of a name was accepted")
	}
	for _, bad := range []string{"", "a/b", "a.b", "a b"} {
		if err := Register(bad, factory); err == nil {
			t.Fatalf("Register(%q) was accepted", bad)
		}
	}
	if err := Register("nofactory", nil); err == nil {
		t.Fatal("a nil factory was accepted")
	}

	b, err := Open(Config{Name: " FAKE "})
	if err != nil || b.Name() != "fake" {
		t.Fatalf("Open: %v %v", b, err)
	}
	if _, err := Open(Config{Name: "fake", ProviderConfig: map[string]any{"kee": "x"}}); err == nil || !strings.Contains(err.Error(), "kee") {
		t.Fatalf("an undeclared key was accepted: %v", err)
	}
	if _, err := Open(Config{Name: "fake", ProviderConfig: map[string]any{"key": "refuse"}}); err == nil || !strings.Contains(err.Error(), `provider "fake": refused`) {
		t.Fatalf("a factory error does not stop Open: %v", err)
	}

	// A name this project publishes, not linked: the command that adds it.
	_, err = Open(Config{Name: "stripe"})
	if err == nil || !strings.Contains(err.Error(), "nucleus add stripe") || !strings.Contains(err.Error(), "providers/billing-stripe") {
		t.Fatalf("an unlinked first-party provider: %v", err)
	}
	// Any other name: what is registered.
	_, err = Open(Config{Name: "paddle"})
	if err == nil || !strings.Contains(err.Error(), "registered: fake") || strings.Contains(err.Error(), "nucleus add") {
		t.Fatalf("an unknown provider: %v", err)
	}
}

func TestStatusActive(t *testing.T) {
	for s, want := range map[Status]bool{
		StatusActive: true, StatusTrialing: true, StatusPastDue: false, StatusUnpaid: false,
		StatusCanceled: false, StatusIncomplete: false, StatusIncompleteExpired: false, StatusPaused: false,
	} {
		if s.Active() != want {
			t.Errorf("%s.Active() = %v", s, !want)
		}
	}
}
