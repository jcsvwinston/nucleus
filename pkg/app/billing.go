// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/billing"
	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

// BillingConfig is the `billing` block: the provider the application bills
// its customers through.
//
//	billing:
//	  provider: stripe                          # a name a provider registered with billing.Register
//	  stripe:                                   # the provider's own subtree, bound by the provider
//	    secret_key: env:STRIPE_SECRET_KEY
//	    webhook_secret: env:STRIPE_WEBHOOK_SECRET
//
// Nothing set is no billing. `stripe` is registered by importing
// github.com/jcsvwinston/nucleus/providers/billing-stripe, which `nucleus add
// stripe` writes together with this block; selected without the import the
// application refuses to start and names the command (ADR-037).
type BillingConfig struct {
	// Provider selects the provider by the name it registered with
	// billing.Register. Empty means the application does not bill.
	Provider string `koanf:"provider"`
}

// BillingBridgeName is the outbox bridge that hands verified billing events
// to the handlers the application registered (billing.Billing.On). It is
// routed "billing.*", the topics the events travel under; an operator's own
// bridge routed there — a webhook to a data warehouse — receives them too.
const BillingBridgeName = "billing-handlers"

// attachBilling builds the provider the configuration selects and, when the
// outbox is enabled, the bridge its events are delivered through. It runs
// on every stack, WithoutDefaults() included, after the outbox: a selected
// provider that an option could switch off would be ignored without a word.
func attachBilling(a *App, cfg *Config) error {
	name := strings.ToLower(strings.TrimSpace(cfg.Billing.Provider))
	if name == "" {
		return nil
	}
	b, err := billing.Open(billing.Config{Name: name, ProviderConfig: cfg.BillingProviderConfig, Logger: a.Logger})
	if err != nil {
		return err
	}
	a.Billing = b
	events := "the outbox is disabled: nothing receives the provider's webhook (outbox.enabled: true turns it on)"
	if a.Outbox != nil {
		if err := a.Outbox.RegisterBridge(&billingBridge{billing: b}); err != nil {
			return fmt.Errorf("billing: %w", err)
		}
		a.Outbox.AddRoute("billing.*", BillingBridgeName)
		events = "delivered through the outbox"
	}
	a.Logger.Info("billing provider initialized", "provider", name, "events", events)
	return nil
}

// billingBridge is the outbox bridge that delivers a stored billing event to
// the application's handlers. A handler's error is the bridge's error, so
// the outbox retries the event with backoff and, when the attempts run out,
// keeps it in the dead letter with the reason.
type billingBridge struct {
	billing *billing.Billing
}

func (b *billingBridge) Name() string { return BillingBridgeName }

func (b *billingBridge) Send(ctx context.Context, msg outbox.Message) error {
	var ev billing.Event
	if err := json.Unmarshal(msg.Payload, &ev); err != nil {
		// A payload that does not decode will not decode on the next
		// attempt either.
		return outbox.Permanent(fmt.Errorf("billing: message %s is not a billing event: %w", msg.ID, err))
	}
	if string(ev.Type) != msg.Topic {
		return outbox.Permanent(fmt.Errorf("billing: message %s is stored under %q and carries an event of type %q", msg.ID, msg.Topic, ev.Type))
	}
	return b.billing.Deliver(ctx, ev)
}

func (b *billingBridge) Healthy(context.Context) error {
	if b == nil || b.billing == nil {
		return errors.New("billing: bridge is not configured")
	}
	return nil
}

func (b *billingBridge) Close() error { return nil }
