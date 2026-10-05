// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package stripe

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v87/webhook"

	"github.com/jcsvwinston/nucleus/pkg/billing"
)

// signed is a delivery as Stripe sends it: the body, and the
// Stripe-Signature header over it.
func signed(body []byte, secret string, at time.Time) http.Header {
	p := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: body, Secret: secret, Timestamp: at})
	h := http.Header{}
	h.Set(SignatureHeader, p.Header)
	return h
}

// The four events the seam carries are read into its types.
func TestParseWebhook_TheEventsTheSeamCarries(t *testing.T) {
	b := billing.New(Name, openTest(t, defaultConfig("")))
	cases := []struct {
		stripe string
		want   billing.EventType
		status billing.Status
		object string
	}{
		{"customer.subscription.created", billing.SubscriptionCreated, billing.StatusIncomplete, subscriptionJSON("incomplete")},
		{"customer.subscription.updated", billing.SubscriptionUpdated, billing.StatusPastDue, subscriptionJSON("past_due")},
		{"customer.subscription.deleted", billing.SubscriptionCanceled, billing.StatusCanceled, subscriptionJSON("canceled")},
		{"invoice.payment_failed", billing.PaymentFailed, "", failedInvoice},
	}
	for _, c := range cases {
		t.Run(c.stripe, func(t *testing.T) {
			body := eventJSON("evt_"+c.stripe, c.stripe, APIVersion, c.object)
			ev, err := b.ParseWebhook(signed(body, testWebhook, time.Now()), body)
			if err != nil {
				t.Fatalf("ParseWebhook: %v", err)
			}
			if ev.ID != "evt_"+c.stripe || ev.Type != c.want || ev.Provider != Name || ev.Live || ev.OccurredAt.Unix() != 1790000000 {
				t.Fatalf("event = %+v", ev)
			}
			if c.want == billing.PaymentFailed {
				p := ev.Payment
				if p == nil || p.Invoice != "in_1" || p.Customer != "cus_1" || p.Subscription != "sub_1" || p.Reference != "user-42" ||
					p.AmountDue != 2900 || p.Currency != "eur" || p.AttemptCount != 2 || p.NextAttemptAt.Unix() != 1790300000 {
					t.Fatalf("payment = %+v", p)
				}
				return
			}
			s := ev.Subscription
			if s == nil || s.ID != "sub_1" || s.Customer != "cus_1" || s.Reference != "user-42" || s.Price != "price_team" ||
				s.CurrentPeriodEnd.Unix() != 1792000000 || s.Status != c.status {
				t.Fatalf("subscription = %+v", s)
			}
		})
	}
}

// Whatever fails verification is billing.ErrSignature, says why without
// repeating the secret or the header, and is decided before the body is
// read: a forged event of a type the seam does not carry is a forgery, not
// an unsupported event.
func TestParseWebhook_RefusesWhatFailsVerification(t *testing.T) {
	b := billing.New(Name, openTest(t, defaultConfig("")))
	body := eventJSON("evt_1", "customer.subscription.updated", APIVersion, subscriptionJSON("active"))
	other := eventJSON("evt_1", "customer.subscription.updated", APIVersion, subscriptionJSON("canceled"))
	unsupported := eventJSON("evt_2", "charge.refunded", APIVersion, `{"id":"ch_1","object":"charge"}`)
	cases := []struct {
		name   string
		header http.Header
		body   []byte
		reason string
	}{
		{"no signature", http.Header{}, body, "no Stripe-Signature"},
		{"a malformed header", http.Header{SignatureHeader: {"t=notanumber,v1=deadbeef"}}, body, "malformed"},
		{"another secret", signed(body, "whsec_someoneelse", time.Now()), body, "no signature matches"},
		{"another body", signed(other, testWebhook, time.Now()), body, "no signature matches"},
		{"an expired timestamp", signed(body, testWebhook, time.Now().Add(-6*time.Minute)), body, "outside the tolerance"},
		{"a forged unsupported event", signed(unsupported, "whsec_someoneelse", time.Now()), unsupported, "no signature matches"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := b.ParseWebhook(c.header, c.body)
			if !errors.Is(err, billing.ErrSignature) || !strings.Contains(err.Error(), c.reason) {
				t.Fatalf("err = %v, want ErrSignature saying %q", err, c.reason)
			}
			if strings.Contains(err.Error(), testWebhook) || strings.Contains(err.Error(), "whsec_someoneelse") {
				t.Fatalf("the refusal repeats a secret: %v", err)
			}
		})
	}

	// The tolerance is the configuration's: the same delivery, a minute old,
	// passes with 5m and not with 30s.
	tight := billing.New(Name, openTest(t, Config{SecretKey: "env:STRIPE_SECRET_KEY", WebhookSecret: "env:STRIPE_WEBHOOK_SECRET", Tolerance: 30 * time.Second}))
	minuteOld := signed(body, testWebhook, time.Now().Add(-time.Minute))
	if _, err := b.ParseWebhook(minuteOld, body); err != nil {
		t.Fatalf("a minute old with 5m: %v", err)
	}
	if _, err := tight.ParseWebhook(minuteOld, body); !errors.Is(err, billing.ErrSignature) {
		t.Fatalf("a minute old with 30s: %v", err)
	}
}

// A verified event the seam does not carry is acknowledged as such; an
// event rendered at an API version of another release line is refused with
// the version to use.
func TestParseWebhook_VerifiedAndNotCarried(t *testing.T) {
	b := billing.New(Name, openTest(t, defaultConfig("")))
	unsupported := eventJSON("evt_2", "charge.refunded", APIVersion, `{"id":"ch_1","object":"charge"}`)
	if _, err := b.ParseWebhook(signed(unsupported, testWebhook, time.Now()), unsupported); !errors.Is(err, billing.ErrUnsupportedEvent) {
		t.Fatalf("an unsupported event: %v", err)
	}

	old := eventJSON("evt_3", "customer.subscription.updated", "2024-06-20", subscriptionJSON("active"))
	_, err := b.ParseWebhook(signed(old, testWebhook, time.Now()), old)
	if err == nil || errors.Is(err, billing.ErrSignature) || !strings.Contains(err.Error(), APIVersion) || !strings.Contains(err.Error(), "2024-06-20") {
		t.Fatalf("an event at another API version: %v", err)
	}
}
