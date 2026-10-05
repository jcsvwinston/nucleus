// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package stripe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	stripego "github.com/stripe/stripe-go/v87"

	"github.com/jcsvwinston/nucleus/pkg/billing"
)

// Nothing in these tests reaches Stripe: the API is an httptest server that
// answers the way Stripe's does, and the webhook deliveries are signed here
// with a test secret, the way Stripe signs them.

const (
	testKey     = "rk_test_catalogbenchrestrictedkey"
	testWebhook = "whsec_catalogbenchsigningsecret"
)

// refs resolves the references the tests use, the way the secrets chain
// resolves env: references.
type refs map[string]string

func (r refs) Resolve(_ context.Context, ref string) ([]byte, error) {
	v, ok := r[ref]
	if !ok {
		return nil, fmt.Errorf("secrets: env var %q resolved to an empty value", strings.TrimPrefix(ref, "env:"))
	}
	return []byte(v), nil
}

var defaultRefs = refs{"env:STRIPE_SECRET_KEY": testKey, "env:STRIPE_WEBHOOK_SECRET": testWebhook}

func defaultConfig(apiURL string) Config {
	return Config{SecretKey: "env:STRIPE_SECRET_KEY", WebhookSecret: "env:STRIPE_WEBHOOK_SECRET", Tolerance: 5 * time.Minute, APIURL: apiURL}
}

// fakeStripe answers the four calls the provider makes and records what it
// was sent.
type fakeStripe struct {
	srv *httptest.Server
	mu  sync.Mutex
	got []recorded
}

type recorded struct {
	method, path, auth string
	form               url.Values
}

func newFakeStripe(t *testing.T) *fakeStripe {
	t.Helper()
	f := &fakeStripe{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		f.mu.Lock()
		f.got = append(f.got, recorded{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), form: form})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "req_fake")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/customers":
			fmt.Fprintf(w, `{"id":"cus_1","object":"customer","email":%q,"name":%q,"metadata":{"nucleus_reference":%q}}`,
				form.Get("email"), form.Get("name"), form.Get("metadata[nucleus_reference]"))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/checkout/sessions":
			fmt.Fprint(w, `{"id":"cs_test_1","object":"checkout.session","url":"https://checkout.stripe.com/c/pay/cs_test_1","expires_at":1791100000}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/billing_portal/sessions":
			fmt.Fprint(w, `{"id":"bps_1","object":"billing_portal.session","url":"https://billing.stripe.com/p/session/test_1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/subscriptions/sub_1":
			fmt.Fprint(w, subscriptionJSON("active"))
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":{"type":"invalid_request_error","code":"resource_missing","message":"No such subscription: '%s'"}}`, strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/"))
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeStripe) last() recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.got[len(f.got)-1]
}

func subscriptionJSON(status string) string {
	return `{"id":"sub_1","object":"subscription","customer":"cus_1","status":"` + status + `","cancel_at_period_end":true,` +
		`"canceled_at":1790000000,"metadata":{"nucleus_reference":"user-42","plan":"team"},` +
		`"items":{"object":"list","data":[` +
		`{"id":"si_1","object":"subscription_item","price":{"id":"price_team","object":"price"},"quantity":3,"current_period_end":1791000000},` +
		`{"id":"si_2","object":"subscription_item","price":{"id":"price_seat","object":"price"},"quantity":1,"current_period_end":1792000000}]}}`
}

func openTest(t *testing.T, c Config) *provider {
	t.Helper()
	p, err := open(t.Context(), c, slog.New(slog.DiscardHandler), defaultRefs)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return p
}

// The provider registers itself under "stripe" when the package is imported.
func TestRegistersUnderItsName(t *testing.T) {
	for _, name := range billing.RegisteredProviders() {
		if name == Name {
			return
		}
	}
	t.Fatalf("registered: %v", billing.RegisteredProviders())
}

// The requests carry what the seam promises: a subscription checkout for
// the price, the reference on the subscription the checkout creates, no
// payment_method_types (the account's own), and the key as a bearer token.
func TestCreateCheckout(t *testing.T) {
	f := newFakeStripe(t)
	b := billing.New(Name, openTest(t, defaultConfig(f.srv.URL)))
	co, err := b.CreateCheckout(t.Context(), billing.CheckoutRequest{
		Customer: "cus_1", Price: "price_team", Quantity: 3, Reference: "user-42", TrialDays: 14,
		SuccessURL: "https://app.example/billing/done", CancelURL: "https://app.example/pricing",
		Metadata: map[string]string{"plan": "team"},
	})
	if err != nil {
		t.Fatalf("CreateCheckout: %v", err)
	}
	if co.ID != "cs_test_1" || co.URL != "https://checkout.stripe.com/c/pay/cs_test_1" || co.ExpiresAt.Unix() != 1791100000 {
		t.Fatalf("checkout = %+v", co)
	}
	got := f.last()
	if got.auth != "Bearer "+testKey {
		t.Fatalf("Authorization = %q", got.auth)
	}
	want := map[string]string{
		"mode":                    "subscription",
		"line_items[0][price]":    "price_team",
		"line_items[0][quantity]": "3",
		"customer":                "cus_1",
		"client_reference_id":     "user-42",
		"success_url":             "https://app.example/billing/done",
		"cancel_url":              "https://app.example/pricing",
		"subscription_data[metadata][nucleus_reference]": "user-42",
		"subscription_data[metadata][plan]":              "team",
		"subscription_data[trial_period_days]":           "14",
	}
	for k, v := range want {
		if got.form.Get(k) != v {
			t.Errorf("%s = %q, want %q (form %v)", k, got.form.Get(k), v, got.form)
		}
	}
	for k := range got.form {
		if strings.HasPrefix(k, "payment_method_types") {
			t.Errorf("the checkout fixes the payment methods: %s", k)
		}
	}
}

func TestCustomerPortalAndSubscription(t *testing.T) {
	f := newFakeStripe(t)
	b := billing.New(Name, openTest(t, defaultConfig(f.srv.URL)))

	c, err := b.CreateCustomer(t.Context(), billing.CustomerRequest{Email: "ada@example.com", Name: "Ada", Reference: "user-42"})
	if err != nil || c.ID != "cus_1" || c.Reference != "user-42" {
		t.Fatalf("CreateCustomer = %+v, %v", c, err)
	}
	if got := f.last().form.Get("metadata[nucleus_reference]"); got != "user-42" {
		t.Fatalf("the customer's reference: %q", got)
	}

	p, err := b.CreatePortal(t.Context(), billing.PortalRequest{Customer: "cus_1", ReturnURL: "https://app.example/account"})
	if err != nil || p.URL != "https://billing.stripe.com/p/session/test_1" {
		t.Fatalf("CreatePortal = %+v, %v", p, err)
	}

	s, err := b.Subscription(t.Context(), "sub_1")
	if err != nil {
		t.Fatalf("Subscription: %v", err)
	}
	if s.ID != "sub_1" || s.Customer != "cus_1" || s.Status != billing.StatusActive || s.Reference != "user-42" ||
		s.Price != "price_team" || s.Quantity != 3 || !s.CancelAtPeriodEnd ||
		s.CurrentPeriodEnd.Unix() != 1792000000 || s.CanceledAt.Unix() != 1790000000 || s.Metadata["plan"] != "team" {
		t.Fatalf("subscription = %+v", s)
	}

	_, err = b.Subscription(t.Context(), "sub_missing")
	if !errors.Is(err, billing.ErrNotFound) || !strings.Contains(err.Error(), "req_fake") {
		t.Fatalf("an unknown subscription: %v", err)
	}
	var sdk *stripego.Error
	if errors.As(err, &sdk) {
		t.Fatal("the SDK's error type reaches the application")
	}
}

// The keys are references. A key written in clear is refused — and the
// refusal does not repeat it — as is a key of the wrong kind, a tolerance
// outside its bounds and an API address the key would travel to in clear.
func TestConfigurationRefusals(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		refs refs
		want string
	}{
		{"a secret key in clear", Config{SecretKey: "sk_live_writtenincleartext", WebhookSecret: "env:STRIPE_WEBHOOK_SECRET", Tolerance: time.Minute}, defaultRefs, "in clear"},
		{"a webhook secret in clear", Config{SecretKey: "env:STRIPE_SECRET_KEY", WebhookSecret: "whsec_writtenincleartext", Tolerance: time.Minute}, defaultRefs, "in clear"},
		{"a publishable key", defaultConfig(""), refs{"env:STRIPE_SECRET_KEY": "pk_test_51Hpkvalue", "env:STRIPE_WEBHOOK_SECRET": testWebhook}, "publishable"},
		{"not a key", defaultConfig(""), refs{"env:STRIPE_SECRET_KEY": "hunter2", "env:STRIPE_WEBHOOK_SECRET": testWebhook}, "not a Stripe API key"},
		{"not a signing secret", defaultConfig(""), refs{"env:STRIPE_SECRET_KEY": testKey, "env:STRIPE_WEBHOOK_SECRET": "hunter2"}, "not a webhook signing secret"},
		{"an unset variable", defaultConfig(""), refs{"env:STRIPE_WEBHOOK_SECRET": testWebhook}, "STRIPE_SECRET_KEY"},
		{"a tolerance of zero", Config{SecretKey: "env:STRIPE_SECRET_KEY", WebhookSecret: "env:STRIPE_WEBHOOK_SECRET"}, defaultRefs, "tolerance"},
		{"a tolerance of an hour", Config{SecretKey: "env:STRIPE_SECRET_KEY", WebhookSecret: "env:STRIPE_WEBHOOK_SECRET", Tolerance: time.Hour}, defaultRefs, "tolerance"},
		{"an http API address", defaultConfig("http://stripe.example.com"), defaultRefs, "https"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := open(t.Context(), c.cfg, slog.New(slog.DiscardHandler), c.refs)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("open = %v, want an error naming %q", err, c.want)
			}
			for _, secret := range []string{"writtenincleartext", "hunter2", "pk_test_51Hpkvalue", testKey, testWebhook} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("the refusal repeats a secret: %v", err)
				}
			}
		})
	}
	// Loopback http is how stripe-mock and a test double are reached.
	openTest(t, defaultConfig("http://127.0.0.1:12111"))
}

// The factory the framework calls binds the subtree strictly and fills the
// defaults: env:STRIPE_SECRET_KEY, env:STRIPE_WEBHOOK_SECRET, 5m.
func TestNewReadsTheSubtree(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", testKey)
	t.Setenv("STRIPE_WEBHOOK_SECRET", testWebhook)
	p, err := New(billing.Config{Name: Name, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("New with the defaults: %v", err)
	}
	if got := p.(*provider); got.tolerance != 5*time.Minute || got.webhookSecret != testWebhook || got.live {
		t.Fatalf("provider = %+v", got)
	}
	_, err = New(billing.Config{Name: Name, ProviderConfig: map[string]any{"secret_key": "env:STRIPE_SECRET_KEY", "webhok_secret": "env:X"}})
	if err == nil || !strings.Contains(err.Error(), "webhok_secret") {
		t.Fatalf("an undeclared key: %v", err)
	}
}

// eventJSON is a Stripe event as an endpoint at APIVersion receives it.
func eventJSON(id, typ, version, object string) []byte {
	return []byte(fmt.Sprintf(`{"id":%q,"object":"event","api_version":%q,"created":1790000000,"livemode":false,"type":%q,"data":{"object":%s}}`,
		id, version, typ, object))
}

const failedInvoice = `{"id":"in_1","object":"invoice","customer":"cus_1","amount_due":2900,"currency":"eur","attempt_count":2,` +
	`"next_payment_attempt":1790300000,"parent":{"type":"subscription_details","subscription_details":{"subscription":"sub_1","metadata":{"nucleus_reference":"user-42"}}}}`
