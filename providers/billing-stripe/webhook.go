// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package stripe

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	stripego "github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/webhook"

	"github.com/jcsvwinston/nucleus/pkg/billing"
)

// SignatureHeader is the header Stripe signs a delivery in.
const SignatureHeader = "Stripe-Signature"

// eventTypes maps the Stripe events the seam carries to its types. Anything
// else an endpoint sends is verified, acknowledged and dropped
// (billing.ErrUnsupportedEvent); subscribe the endpoint to these four.
var eventTypes = map[stripego.EventType]billing.EventType{
	"customer.subscription.created": billing.SubscriptionCreated,
	"customer.subscription.updated": billing.SubscriptionUpdated,
	"customer.subscription.deleted": billing.SubscriptionCanceled,
	"invoice.payment_failed":        billing.PaymentFailed,
}

// ParseWebhook verifies a delivery with the SDK — the signature, an
// HMAC-SHA256 of "<timestamp>.<body>" under the signing secret compared in
// constant time, and the timestamp within the tolerance — which reads
// nothing from the body until it has; then it checks the API version the
// event is rendered at, and translates it.
func (p *provider) ParseWebhook(header http.Header, payload []byte) (billing.Event, error) {
	ev, err := webhook.ConstructEventWithOptions(payload, header.Get(SignatureHeader), p.webhookSecret,
		webhook.ConstructEventOptions{Tolerance: p.tolerance})
	if err != nil {
		if reason, forged := signatureReason(err); forged {
			return billing.Event{}, fmt.Errorf("%w: %s", billing.ErrSignature, reason)
		}
		// Verified, and still refused: the API version, or a body that is
		// not an event.
		if ev.APIVersion != "" && ev.APIVersion != APIVersion {
			return billing.Event{}, fmt.Errorf("stripe: the endpoint sends events at API version %s, and this module reads %s; create the endpoint with a version of the same release line", ev.APIVersion, APIVersion)
		}
		return billing.Event{}, fmt.Errorf("stripe: the delivery is verified and is not an event this module reads: %v", err)
	}

	typ, carried := eventTypes[ev.Type]
	if !carried {
		return billing.Event{}, fmt.Errorf("%w: stripe %s", billing.ErrUnsupportedEvent, ev.Type)
	}
	out := billing.Event{
		ID:         ev.ID,
		Type:       typ,
		Live:       ev.Livemode,
		OccurredAt: unix(ev.Created),
	}
	if ev.Data == nil {
		return billing.Event{}, fmt.Errorf("stripe: event %s (%s) carries no object", ev.ID, ev.Type)
	}
	switch typ {
	case billing.PaymentFailed:
		var inv stripego.Invoice
		if err := json.Unmarshal(ev.Data.Raw, &inv); err != nil {
			return billing.Event{}, fmt.Errorf("stripe: event %s (%s): the invoice does not decode: %v", ev.ID, ev.Type, err)
		}
		out.Payment = paymentOf(&inv)
	default:
		var sub stripego.Subscription
		if err := json.Unmarshal(ev.Data.Raw, &sub); err != nil {
			return billing.Event{}, fmt.Errorf("stripe: event %s (%s): the subscription does not decode: %v", ev.ID, ev.Type, err)
		}
		s := subscriptionOf(&sub)
		out.Subscription = &s
	}
	return out, nil
}

// signatureReason says why a delivery failed verification, for the
// failures the SDK reports as such — none of its reasons repeats the header
// or the secret — and whether err is one of them.
func signatureReason(err error) (string, bool) {
	switch {
	case errors.Is(err, stripego.ErrWebhookNotSigned):
		return "the delivery carries no Stripe-Signature header", true
	case errors.Is(err, stripego.ErrWebhookInvalidHeader):
		return "the Stripe-Signature header is malformed", true
	case errors.Is(err, stripego.ErrWebhookTooOld):
		return "the signature's timestamp is outside the tolerance", true
	case errors.Is(err, stripego.ErrWebhookNoValidSignature):
		return "no signature matches the body under the endpoint's secret", true
	case errors.Is(err, stripego.ErrWebhookEmptySecret):
		return "the endpoint's signing secret is empty", true
	}
	return "", false
}

// subscriptionOf reads the fields the seam carries. Since the 2025-03-31
// API the billing period is per item; the subscription's is the latest end
// among them.
func subscriptionOf(s *stripego.Subscription) billing.Subscription {
	out := billing.Subscription{
		ID:                s.ID,
		Status:            billing.Status(s.Status),
		Reference:         s.Metadata[ReferenceKey],
		CancelAtPeriodEnd: s.CancelAtPeriodEnd,
		CanceledAt:        unix(s.CanceledAt),
		Metadata:          s.Metadata,
	}
	if s.Customer != nil {
		out.Customer = s.Customer.ID
	}
	if s.Items != nil {
		var end int64
		for i, item := range s.Items.Data {
			if item == nil {
				continue
			}
			if i == 0 {
				if item.Price != nil {
					out.Price = item.Price.ID
				}
				out.Quantity = item.Quantity
			}
			if item.CurrentPeriodEnd > end {
				end = item.CurrentPeriodEnd
			}
		}
		out.CurrentPeriodEnd = unix(end)
	}
	return out
}

// paymentOf reads a failed invoice. The subscription it belongs to, and the
// reference, are under the invoice's parent since the 2025-03-31 API.
func paymentOf(inv *stripego.Invoice) *billing.Payment {
	out := &billing.Payment{
		Invoice:       inv.ID,
		AmountDue:     inv.AmountDue,
		Currency:      string(inv.Currency),
		AttemptCount:  inv.AttemptCount,
		NextAttemptAt: unix(inv.NextPaymentAttempt),
	}
	if inv.Customer != nil {
		out.Customer = inv.Customer.ID
	}
	if inv.Parent != nil && inv.Parent.SubscriptionDetails != nil {
		d := inv.Parent.SubscriptionDetails
		if d.Subscription != nil {
			out.Subscription = d.Subscription.ID
		}
		out.Reference = d.Metadata[ReferenceKey]
	}
	return out
}
