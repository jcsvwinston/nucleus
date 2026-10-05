// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package billing is the provider-neutral seam between an application and
// the service that bills its customers: the values an application exchanges
// with a billing provider, the contract a provider module implements, and
// the registry configuration selects a provider from (ADR-037).
//
// What an application does with it is small on purpose:
//
//   - create a customer, linked to the application's own user by a
//     reference (CreateCustomer);
//   - send a customer to the provider's hosted checkout for a subscription
//     (CreateCheckout), and to its hosted portal to manage it
//     (CreatePortal);
//   - read a subscription's state (Subscription);
//   - receive what happened to a subscription afterwards — created,
//     updated, canceled, a payment that failed — as typed events.
//
// The events arrive through a webhook the provider calls. The provider
// module verifies the delivery (ParseWebhook) and turns it into an Event;
// the framework stores it in the transactional outbox under the provider's
// event id, which makes a second delivery of the same event a no-op, and
// the outbox hands it to the handlers the application registered with
// Billing.On — retried with backoff when a handler fails, like every other
// outbox message.
//
// What it does not do is the provider's business: invoicing, tax,
// proration, dunning, prices and their catalogue stay in the provider and
// its dashboard. Card data never reaches the application — checkout and the
// portal are the provider's pages.
//
// A provider is a module of its own that registers here from an init
// function (`nucleus add stripe` installs github.com/jcsvwinston/nucleus/
// providers/billing-stripe), so the vendor SDK is linked by the
// applications that bill and by nobody else. This package imports the
// standard library and nothing a provider author would have to carry.
package billing

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Provider is what a billing module implements. The framework calls it
// through Billing, which validates the requests first, so an implementation
// can rely on the fields Billing documents as required.
//
// Errors are the provider's own, wrapped: none of the vendor SDK's types
// reaches the application. A subscription the provider does not know is
// ErrNotFound; a webhook delivery that fails verification is ErrSignature;
// a verified event of a kind this package does not carry is
// ErrUnsupportedEvent.
type Provider interface {
	// CreateCustomer creates a customer at the provider.
	CreateCustomer(ctx context.Context, req CustomerRequest) (Customer, error)

	// CreateCheckout starts a hosted checkout that subscribes a customer to
	// a price, and returns where to send the customer.
	CreateCheckout(ctx context.Context, req CheckoutRequest) (Checkout, error)

	// CreatePortal starts a session of the provider's hosted portal, where
	// a customer updates a payment method or cancels a subscription.
	CreatePortal(ctx context.Context, req PortalRequest) (Portal, error)

	// Subscription reads a subscription's current state from the provider.
	Subscription(ctx context.Context, id string) (Subscription, error)

	// ParseWebhook verifies one webhook delivery — the request's headers
	// and its raw body, exactly as received — and translates the event it
	// carries.
	//
	// Verification comes first and covers every outcome: an event that is
	// not authentic is ErrSignature whatever its type, so a forged delivery
	// cannot learn which types are carried. A delivery whose signature
	// timestamp is outside the provider's tolerance is ErrSignature too.
	ParseWebhook(header http.Header, payload []byte) (Event, error)
}

var (
	// ErrSignature is a webhook delivery that failed verification: no
	// signature, a signature that does not match the body under the
	// endpoint's secret, or a timestamp outside the tolerance.
	ErrSignature = errors.New("billing: the webhook delivery failed verification")

	// ErrUnsupportedEvent is a verified event of a kind this package does
	// not carry. The webhook route acknowledges it, so the provider does not
	// retry it, and delivers nothing.
	ErrUnsupportedEvent = errors.New("billing: the event is verified and is not one this framework carries")

	// ErrNotFound is a customer or a subscription the provider does not
	// know.
	ErrNotFound = errors.New("billing: not found at the provider")

	// ErrInvalidRequest is a request Billing refuses before it reaches the
	// provider: a required field missing, a URL that is not absolute.
	ErrInvalidRequest = errors.New("billing: invalid request")
)

// EventType names what happened. It is also the outbox topic the event
// travels under, so an outbox bridge routed to "billing.*" receives every
// billing event.
type EventType string

// The events the seam carries.
const (
	// SubscriptionCreated: a subscription exists, typically because a
	// checkout completed. Its status may still be incomplete while the
	// first payment settles.
	SubscriptionCreated EventType = "billing.subscription.created"
	// SubscriptionUpdated: anything about a subscription changed — its
	// status (active, past_due…), its price or quantity, a cancellation
	// scheduled for the end of the period.
	SubscriptionUpdated EventType = "billing.subscription.updated"
	// SubscriptionCanceled: a subscription ended. It does not come back;
	// a customer who subscribes again gets a new one.
	SubscriptionCanceled EventType = "billing.subscription.canceled"
	// PaymentFailed: a payment for an invoice failed. The provider retries
	// on its own schedule (Payment.NextAttemptAt); the subscription's
	// status follows in a SubscriptionUpdated event.
	PaymentFailed EventType = "billing.payment.failed"
)

// EventTypes returns every type the seam carries.
func EventTypes() []EventType {
	return []EventType{SubscriptionCreated, SubscriptionUpdated, SubscriptionCanceled, PaymentFailed}
}

// Known reports whether t is one of the types the seam carries.
func (t EventType) Known() bool {
	for _, k := range EventTypes() {
		if t == k {
			return true
		}
	}
	return false
}

// Event is one verified event from the provider.
type Event struct {
	// ID is the provider's id for the event. A provider delivers an event
	// more than once — a retry after a lost response is the common case —
	// and the id is how the framework recognises the second delivery.
	ID string `json:"id"`
	// Type is what happened.
	Type EventType `json:"type"`
	// Provider is the registered name of the provider that sent it.
	Provider string `json:"provider"`
	// Live is false for an event from the provider's test mode.
	Live bool `json:"live"`
	// OccurredAt is when the provider recorded it.
	OccurredAt time.Time `json:"occurred_at"`
	// Subscription is the subscription's state as of the event, for the
	// subscription events.
	Subscription *Subscription `json:"subscription,omitempty"`
	// Payment is the failed payment, for PaymentFailed.
	Payment *Payment `json:"payment,omitempty"`
}

// Status is a subscription's state. The values are the common vocabulary of
// subscription billing; a provider maps its own onto them.
type Status string

// The statuses.
const (
	StatusActive            Status = "active"
	StatusTrialing          Status = "trialing"
	StatusPastDue           Status = "past_due"
	StatusUnpaid            Status = "unpaid"
	StatusPaused            Status = "paused"
	StatusIncomplete        Status = "incomplete"
	StatusIncompleteExpired Status = "incomplete_expired"
	StatusCanceled          Status = "canceled"
)

// Active reports whether a subscription in this status is in good standing:
// active, or in its trial. past_due is not — whether a customer keeps access
// while the provider retries a payment is the application's decision.
func (s Status) Active() bool { return s == StatusActive || s == StatusTrialing }

// Subscription is a subscription's state at the provider.
type Subscription struct {
	ID string `json:"id"`
	// Customer is the provider's id for the customer.
	Customer string `json:"customer"`
	Status   Status `json:"status"`
	// Reference is the application's own reference — typically its user
	// id — given to CreateCheckout, carried by every event about the
	// subscription. Empty when the subscription was not created through
	// CreateCheckout.
	Reference string `json:"reference,omitempty"`
	// Price is the provider's id of the price subscribed to, and Quantity
	// how many of it. A subscription with several items reports its first.
	Price    string `json:"price,omitempty"`
	Quantity int64  `json:"quantity,omitempty"`
	// CurrentPeriodEnd is when the period paid for ends.
	CurrentPeriodEnd time.Time `json:"current_period_end,omitzero"`
	// CancelAtPeriodEnd reports a cancellation scheduled for the end of the
	// current period: the subscription is still active until then.
	CancelAtPeriodEnd bool `json:"cancel_at_period_end"`
	// CanceledAt is when the cancellation was requested, zero when none was.
	CanceledAt time.Time `json:"canceled_at,omitzero"`
	// Metadata is the subscription's metadata at the provider.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Payment is a payment for an invoice.
type Payment struct {
	// Invoice is the provider's id for the invoice.
	Invoice  string `json:"invoice"`
	Customer string `json:"customer"`
	// Subscription is the subscription the invoice belongs to, when it
	// belongs to one, and Reference the application's reference it carries.
	Subscription string `json:"subscription,omitempty"`
	Reference    string `json:"reference,omitempty"`
	// AmountDue is in the currency's minor unit (cents for USD and EUR).
	AmountDue int64  `json:"amount_due"`
	Currency  string `json:"currency"`
	// AttemptCount is how many times the payment was attempted.
	AttemptCount int64 `json:"attempt_count"`
	// NextAttemptAt is when the provider tries again; zero when it will not.
	NextAttemptAt time.Time `json:"next_attempt_at,omitzero"`
}

// CustomerRequest creates a customer.
type CustomerRequest struct {
	// Email is required: it is where the provider sends receipts.
	Email string
	Name  string
	// Reference is the application's own reference for the customer —
	// typically its user id. It is stored with the customer so the
	// provider's records point back at the application's.
	Reference string
	Metadata  map[string]string
}

// Customer is a customer at the provider.
type Customer struct {
	ID        string `json:"id"`
	Email     string `json:"email,omitempty"`
	Name      string `json:"name,omitempty"`
	Reference string `json:"reference,omitempty"`
}

// CheckoutRequest starts a hosted checkout for a subscription.
type CheckoutRequest struct {
	// Customer is the provider's id of an existing customer. Empty lets the
	// checkout create one, with CustomerEmail prefilled when it is set.
	Customer      string
	CustomerEmail string
	// Price is the provider's id of the price to subscribe to. Required.
	Price string
	// Quantity of the price; 0 means 1.
	Quantity int64
	// Reference is the application's own reference — typically its user id
	// — stored on the subscription the checkout creates, so every event
	// about it says whose it is.
	Reference string
	// SuccessURL and CancelURL are where the provider sends the customer
	// back. Both are required and absolute.
	SuccessURL string
	CancelURL  string
	// TrialDays starts the subscription with a trial of that many days.
	TrialDays int64
	// Metadata is stored on the subscription.
	Metadata map[string]string
}

// Checkout is a started checkout.
type Checkout struct {
	ID string `json:"id"`
	// URL is the provider's page; send the customer there with a 303.
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

// PortalRequest starts a session of the provider's hosted portal.
type PortalRequest struct {
	// Customer is the provider's id of the customer. Required.
	Customer string
	// ReturnURL is where the portal sends the customer back. Required and
	// absolute.
	ReturnURL string
}

// Portal is a started portal session.
type Portal struct {
	// URL is the provider's page; send the customer there with a 303.
	URL string `json:"url"`
}
