// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/billing"
)

// What `nucleus add stripe` writes, read by a binary that does not link the
// module: the keys are not installed — not unknown — and say which command
// installs them.
func TestBilling_SubtreeOfAnUnlinkedProviderSaysNotInstalled(t *testing.T) {
	path := writeConfig(t, `
billing:
  provider: stripe
  stripe:
    secret_key: env:STRIPE_SECRET_KEY
    webhook_secret: env:STRIPE_WEBHOOK_SECRET
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("app.LoadConfig accepted the subtree of a billing provider that is not linked")
	}
	msg := err.Error()
	for _, want := range []string{
		"billing.stripe.secret_key (not installed: `nucleus add stripe`)",
		"billing.stripe.webhook_secret (not installed: `nucleus add stripe`)",
		"github.com/jcsvwinston/nucleus/providers/billing-stripe",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, msg)
		}
	}
}

// billing.provider alone, without the module: the application does not
// start, and the refusal names the command.
func TestBilling_UnlinkedProviderRefusesToStart(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Databases = map[string]DatabaseConfig{"default": {URL: "sqlite://" + t.TempDir() + "/app.db"}}
	cfg.Billing.Provider = "stripe"
	_, err := New(&cfg, WithoutDefaults())
	if err == nil || !strings.Contains(err.Error(), "nucleus add stripe") {
		t.Fatalf("New = %v, want a refusal naming nucleus add stripe", err)
	}
}

type nopProvider struct{}

func (nopProvider) CreateCustomer(context.Context, billing.CustomerRequest) (billing.Customer, error) {
	return billing.Customer{}, nil
}
func (nopProvider) CreateCheckout(context.Context, billing.CheckoutRequest) (billing.Checkout, error) {
	return billing.Checkout{}, nil
}
func (nopProvider) CreatePortal(context.Context, billing.PortalRequest) (billing.Portal, error) {
	return billing.Portal{}, nil
}
func (nopProvider) Subscription(context.Context, string) (billing.Subscription, error) {
	return billing.Subscription{}, nil
}
func (nopProvider) ParseWebhook(http.Header, []byte) (billing.Event, error) {
	return billing.Event{}, billing.ErrSignature
}

// A registered provider gets its own subtree on both configuration paths,
// strictly, and the outbox gets the bridge its events are delivered through.
func TestBilling_RegisteredProviderGetsItsSubtreeAndTheBridge(t *testing.T) {
	var got map[string]any
	if err := billing.Register("apptest", func(cfg billing.Config) (billing.Provider, error) {
		got = cfg.ProviderConfig
		var c struct {
			Key string `koanf:"key"`
		}
		return nopProvider{}, cfg.Bind(&c)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { billing.Unregister("apptest") })

	path := writeConfig(t, `
databases:
  default:
    url: sqlite://`+t.TempDir()+`/app.db
billing:
  provider: apptest
  apptest:
    key: env:APPTEST_KEY
outbox:
  enabled: true
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	a, err := New(cfg, WithoutDefaults())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	if a.Billing == nil || a.Billing.Name() != "apptest" {
		t.Fatalf("App.Billing = %v", a.Billing)
	}
	if got["key"] != "env:APPTEST_KEY" {
		t.Fatalf("the provider got %v", got)
	}
	if _, ok := a.Outbox.Registry().Get(BillingBridgeName); !ok {
		t.Fatal("the outbox has no billing bridge")
	}
	if bridges := a.Outbox.Router().Match(string(billing.SubscriptionUpdated)); len(bridges) != 1 || bridges[0] != BillingBridgeName {
		t.Fatalf("billing.subscription.updated is routed to %v", bridges)
	}

	// A key the provider does not declare stops the boot.
	bad := writeConfig(t, `
databases:
  default:
    url: sqlite://`+t.TempDir()+`/app.db
billing:
  provider: apptest
  apptest:
    kee: env:APPTEST_KEY
`)
	cfg, err = LoadConfig(bad)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if _, err := New(cfg, WithoutDefaults()); err == nil || !strings.Contains(err.Error(), "kee") {
		t.Fatalf("New with an undeclared key = %v", err)
	}
}
