// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Handler receives one verified event. A non-nil error — or a panic — makes
// the outbox deliver the event again later, with backoff, until the
// attempts run out and it goes to the dead letter. Every handler registered
// for the type runs again on a retry, so a handler is idempotent: key what
// it does by Event.ID, or by the state it reads (Subscription.Status).
type Handler func(ctx context.Context, ev Event) error

// Billing is the application's billing: the provider the configuration
// selected, behind requests validated the same way whichever provider it
// is, and the handlers its verified events are delivered to.
//
// A module takes it with nucleus.BillingFrom(rt) — in OnStart, which is
// where it registers its handlers, before the outbox starts delivering.
type Billing struct {
	name     string
	provider Provider

	mu       sync.RWMutex
	handlers map[EventType][]Handler
}

// New wraps a provider. The framework calls it through Open; a test of an
// application's billing code can call it with a fake Provider.
func New(name string, p Provider) *Billing {
	return &Billing{name: normalize(name), provider: p, handlers: map[EventType][]Handler{}}
}

// Name is the provider's registered name.
func (b *Billing) Name() string { return b.name }

// CreateCustomer creates a customer at the provider. Email is required.
func (b *Billing) CreateCustomer(ctx context.Context, req CustomerRequest) (Customer, error) {
	if strings.TrimSpace(req.Email) == "" || !strings.Contains(req.Email, "@") {
		return Customer{}, fmt.Errorf("%w: CustomerRequest.Email is required, and is an email address", ErrInvalidRequest)
	}
	return b.provider.CreateCustomer(ctx, req)
}

// CreateCheckout starts a hosted checkout for a subscription. Price,
// SuccessURL and CancelURL are required, the URLs absolute; Quantity 0 is 1.
func (b *Billing) CreateCheckout(ctx context.Context, req CheckoutRequest) (Checkout, error) {
	if strings.TrimSpace(req.Price) == "" {
		return Checkout{}, fmt.Errorf("%w: CheckoutRequest.Price is required", ErrInvalidRequest)
	}
	if req.Quantity < 0 {
		return Checkout{}, fmt.Errorf("%w: CheckoutRequest.Quantity is %d; it is how many of the price, at least 1", ErrInvalidRequest, req.Quantity)
	}
	if req.Quantity == 0 {
		req.Quantity = 1
	}
	if req.TrialDays < 0 {
		return Checkout{}, fmt.Errorf("%w: CheckoutRequest.TrialDays is %d", ErrInvalidRequest, req.TrialDays)
	}
	if err := absoluteURL("CheckoutRequest.SuccessURL", req.SuccessURL); err != nil {
		return Checkout{}, err
	}
	if err := absoluteURL("CheckoutRequest.CancelURL", req.CancelURL); err != nil {
		return Checkout{}, err
	}
	return b.provider.CreateCheckout(ctx, req)
}

// CreatePortal starts a session of the provider's hosted portal. Customer
// and an absolute ReturnURL are required.
func (b *Billing) CreatePortal(ctx context.Context, req PortalRequest) (Portal, error) {
	if strings.TrimSpace(req.Customer) == "" {
		return Portal{}, fmt.Errorf("%w: PortalRequest.Customer is required", ErrInvalidRequest)
	}
	if err := absoluteURL("PortalRequest.ReturnURL", req.ReturnURL); err != nil {
		return Portal{}, err
	}
	return b.provider.CreatePortal(ctx, req)
}

// Subscription reads a subscription's current state from the provider.
// Prefer the state the events carry for decisions taken on every request;
// this is a call to the provider.
func (b *Billing) Subscription(ctx context.Context, id string) (Subscription, error) {
	if strings.TrimSpace(id) == "" {
		return Subscription{}, fmt.Errorf("%w: a subscription id is required", ErrInvalidRequest)
	}
	return b.provider.Subscription(ctx, id)
}

// ParseWebhook verifies a webhook delivery through the provider and checks
// the event it returns: an id, a type the seam carries, and the part of the
// event that type promises. The event is stamped with the provider's name.
func (b *Billing) ParseWebhook(header http.Header, payload []byte) (Event, error) {
	ev, err := b.provider.ParseWebhook(header, payload)
	if err != nil {
		return Event{}, err
	}
	ev.Provider = b.name
	switch {
	case strings.TrimSpace(ev.ID) == "":
		return Event{}, fmt.Errorf("billing: provider %q returned an event without an id; without one a second delivery cannot be recognised", b.name)
	case !ev.Type.Known():
		return Event{}, fmt.Errorf("billing: provider %q returned an event of type %q, which the seam does not carry", b.name, ev.Type)
	case ev.Type == PaymentFailed && ev.Payment == nil:
		return Event{}, fmt.Errorf("billing: provider %q returned %s without the payment", b.name, ev.Type)
	case ev.Type != PaymentFailed && ev.Subscription == nil:
		return Event{}, fmt.Errorf("billing: provider %q returned %s without the subscription", b.name, ev.Type)
	}
	return ev, nil
}

// On registers h for the events of type t. Register in a module's OnStart:
// the outbox starts delivering once every module has started, so an event
// that arrived while the application was down reaches the handlers that
// were registered for it.
func (b *Billing) On(t EventType, h Handler) error {
	if !t.Known() {
		return fmt.Errorf("billing: On(%q): not an event type the seam carries (%v)", t, EventTypes())
	}
	if h == nil {
		return fmt.Errorf("billing: On(%q): nil handler", t)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[t] = append(b.handlers[t], h)
	return nil
}

// Deliver runs the handlers registered for ev.Type, in registration order,
// and returns what they returned, joined. A handler that panics is
// recovered and counts as an error, so the others still run. An event with
// no handler is delivered to nobody, and that is not an error.
//
// The framework calls it from the outbox; a test can call it directly.
func (b *Billing) Deliver(ctx context.Context, ev Event) error {
	b.mu.RLock()
	handlers := append([]Handler(nil), b.handlers[ev.Type]...)
	b.mu.RUnlock()
	var errs []error
	for i, h := range handlers {
		if err := runHandler(ctx, h, ev); err != nil {
			errs = append(errs, fmt.Errorf("billing: handler %d of %s for event %s: %w", i+1, ev.Type, ev.ID, err))
		}
	}
	return errors.Join(errs...)
}

func runHandler(ctx context.Context, h Handler, ev Event) (err error) {
	defer func() {
		if rv := recover(); rv != nil {
			err = fmt.Errorf("panic: %v", rv)
		}
	}()
	return h(ctx, ev)
}

// absoluteURL refuses a URL the provider would refuse later, or would send
// the customer somewhere relative to its own domain.
func absoluteURL(field, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("%w: %s is required and is an absolute http(s) URL", ErrInvalidRequest, field)
	}
	return nil
}
