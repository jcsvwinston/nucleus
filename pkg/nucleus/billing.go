// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/jcsvwinston/nucleus/pkg/billing"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

// Billing satisfies BillingSource: the billing the framework built from the
// `billing` block, or nil when the block selects no provider.
func (rt runtime) Billing() *billing.Billing {
	if rt.core == nil {
		return nil
	}
	return rt.core.Billing
}

// BillingSource is implemented by a Runtime that can hand out the
// application's billing. Like CacheSource it is an optional interface rather
// than a method on Runtime, which is published and does not grow before the
// major (QADR-0010).
type BillingSource interface {
	// Billing returns the application's billing, or nil when there is none.
	Billing() *billing.Billing
}

// BillingFrom returns the application's billing — the provider the
// `billing` block selects (`nucleus add stripe`) — and whether there is one.
//
// A module takes it in OnStart, registers the handlers its events go to,
// and keeps it for its routes:
//
//	OnStart: func(_ context.Context, rt nucleus.Runtime, _ Config) error {
//	        b, ok := nucleus.BillingFrom(rt)
//	        if !ok {
//	                return errors.New("plans: needs billing (nucleus add stripe)")
//	        }
//	        m.billing = b
//	        return b.On(billing.SubscriptionUpdated, func(ctx context.Context, ev billing.Event) error {
//	                return m.store.SetPlan(ctx, ev.Subscription.Reference, ev.Subscription.Status)
//	        })
//	},
//	// in a handler:
//	co, err := m.billing.CreateCheckout(ctx, billing.CheckoutRequest{
//	        Price: "price_…", Reference: userID,
//	        SuccessURL: "https://app.example.com/billing/done", CancelURL: "https://app.example.com/pricing",
//	})
//	// redirect the browser to co.URL with 303
func BillingFrom(rt Runtime) (*billing.Billing, bool) {
	source, ok := rt.(BillingSource)
	if !ok {
		return nil, false
	}
	b := source.Billing()
	if b == nil {
		return nil, false
	}
	return b, true
}

// billingWebhookMaxBytes caps a webhook delivery. A provider's event is a
// few kilobytes; a subscription with many items, a few tens.
const billingWebhookMaxBytes = 512 << 10

// BillingWebhook is the module that serves the billing provider's webhook,
// at <webhooks_prefix>/billing/<provider> — POST /webhooks/billing/stripe
// with the defaults. `nucleus add stripe` mounts it:
//
//	nucleus.New().
//	        FromConfigFile("nucleus.yml").
//	        Mount(nucleus.BillingWebhook()).
//	        Start()
//
// Each delivery goes through the module webhook checks (POST only, the body
// capped at 512 KiB, the route exempt from CSRF because it authenticates by
// signature) and then through the provider, which verifies it — signature
// and timestamp — before anything is read from it. A delivery that fails
// verification is answered 400 and delivers nothing. A verified event is
// stored in the outbox under the provider's event id before the answer, so
// the provider's next delivery of the same event is recognised, answered
// 200 and not delivered again; the outbox then hands the event to the
// handlers registered with Billing.On, and retries it when one fails. A
// verified event of a kind the seam does not carry is acknowledged and
// dropped.
//
// It needs billing.provider and outbox.enabled: true, and refuses to start
// without either.
func BillingWebhook() ModuleSpec {
	in := &billingIngress{}
	return Module[struct{}]{
		Name: "billing",
		OnStart: func(_ context.Context, rt Runtime, _ struct{}) error {
			b, ok := BillingFrom(rt)
			if !ok {
				return errors.New("billing: nucleus.BillingWebhook() is mounted and no provider is configured — set billing.provider (nucleus add stripe writes it)")
			}
			ob := rt.Outbox()
			if ob == nil {
				return errors.New("billing: the webhook route stores each verified event in the outbox before it answers, and the outbox is disabled — set outbox.enabled: true")
			}
			in.billing, in.outbox, in.logger = b, ob, rt.Logger()
			if in.logger == nil {
				in.logger = slog.Default()
			}
			return nil
		},
		Webhooks: func(w WebhookRegistry, _ struct{}) {
			if in.billing == nil {
				// OnStart failed and said why; there is nothing to mount.
				return
			}
			_ = w.Register("/"+in.billing.Name(), WebhookSpec{
				Handler:    in.serve,
				MaxBytes:   billingWebhookMaxBytes,
				verifiedBy: "billing provider " + in.billing.Name(),
			})
		},
	}.Build()
}

// billingIngress is the handler behind BillingWebhook.
type billingIngress struct {
	billing *billing.Billing
	outbox  *outbox.ManagedOutbox
	logger  *slog.Logger
}

// billingOutboxID is the outbox message id of an event: the provider's
// event id, scoped by the provider. The outbox's primary key is what makes
// a second delivery of the event a duplicate.
func billingOutboxID(ev billing.Event) string {
	return "billing:" + ev.Provider + ":" + ev.ID
}

func (in *billingIngress) serve(w http.ResponseWriter, r *http.Request) {
	provider := in.billing.Name()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	ev, err := in.billing.ParseWebhook(r.Header, body)
	switch {
	case errors.Is(err, billing.ErrSignature):
		// Only the reason; never the header, the body or the secret.
		in.logger.Warn("billing: webhook delivery refused: it failed verification", "provider", provider, "reason", err.Error())
		writeBillingAnswer(w, http.StatusBadRequest, `{"received":false,"error":"the delivery failed verification"}`)
		return
	case errors.Is(err, billing.ErrUnsupportedEvent):
		in.logger.Debug("billing: verified event acknowledged and not carried", "provider", provider, "reason", err.Error())
		writeBillingAnswer(w, http.StatusOK, `{"received":true,"delivered":false}`)
		return
	case err != nil:
		// Verified and unusable: a version of the provider's API this module
		// does not read, or a provider that broke its contract. The operator
		// has to act; another delivery of the same thing will not help.
		in.logger.Error("billing: webhook delivery could not be read", "provider", provider, "error", err.Error())
		writeBillingAnswer(w, http.StatusBadRequest, `{"received":false,"error":"the event could not be read"}`)
		return
	}

	_, err = in.outbox.Enqueue(r.Context(), outbox.Entry{
		ID:      billingOutboxID(ev),
		Topic:   string(ev.Type),
		Payload: ev,
	})
	switch {
	case err == nil:
		in.logger.Info("billing: event received", "provider", provider, "event", ev.ID, "type", string(ev.Type))
		writeBillingAnswer(w, http.StatusOK, `{"received":true}`)
	case db.IsUniqueViolation(err):
		// The provider delivers an event again when it did not see the
		// first answer; so does whoever replays a captured delivery inside
		// the signature's tolerance. Either way it was stored once and is
		// delivered once.
		in.logger.Info("billing: event already received; not delivered again", "provider", provider, "event", ev.ID, "type", string(ev.Type))
		writeBillingAnswer(w, http.StatusOK, `{"received":true,"duplicate":true}`)
	default:
		// Not stored, so not acknowledged: the provider delivers it again.
		in.logger.Error("billing: verified event could not be stored; the provider will deliver it again", "provider", provider, "event", ev.ID, "error", err.Error())
		writeBillingAnswer(w, http.StatusInternalServerError, `{"received":false}`)
	}
}

func writeBillingAnswer(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = fmt.Fprintln(w, body)
}
