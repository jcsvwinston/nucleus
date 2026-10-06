// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package stripe is the Stripe provider of a Nucleus application's billing
// (pkg/billing): customers, a hosted Checkout for subscriptions, the
// Customer portal, a subscription's state, and the events of a Stripe
// webhook endpoint, verified and translated into billing events.
//
// Import it for its side effect, once, in the program that assembles the
// application, select it, and mount the webhook route:
//
//	import _ "github.com/jcsvwinston/nucleus/providers/billing-stripe"
//
//	nucleus.New().FromConfigFile("nucleus.yml").Mount(nucleus.BillingWebhook()).Start()
//
//	# nucleus.yml
//	billing:
//	  provider: stripe
//	  stripe:
//	    secret_key: env:STRIPE_SECRET_KEY
//	    webhook_secret: env:STRIPE_WEBHOOK_SECRET
//	outbox:
//	  enabled: true
//
// `nucleus add stripe` does all three.
//
// The two keys are references, never the keys themselves: env:NAME (or a
// bare NAME) reads an environment variable, aws-sm:<secret-id>[#key] reads
// AWS Secrets Manager once `nucleus add aws-sm` is in the build. A Stripe
// key written in clear in the configuration is refused, and no message of
// this package repeats a key. Prefer a restricted key (rk_…) with write
// access to Customers, Checkout Sessions and the Customer portal and read
// access to Subscriptions; a secret key (sk_…) works too.
//
// A webhook delivery is verified by the SDK — the Stripe-Signature header,
// an HMAC-SHA256 of the timestamp and the raw body under the endpoint's
// signing secret, compared in constant time, the timestamp within
// billing.stripe.tolerance (5m) — before anything in it is read. The events
// are read at the API version of the SDK this module pins (APIVersion); an
// endpoint created with a version of another release line is refused, with
// the version to use.
//
// The SDK, github.com/stripe/stripe-go, is linked only by applications that
// import this package: the framework does not depend on it (ADR-037).
package stripe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	stripego "github.com/stripe/stripe-go/v87"

	"github.com/jcsvwinston/nucleus/pkg/auth/secrets"
	"github.com/jcsvwinston/nucleus/pkg/billing"
)

// Name is the name the provider registers under: what billing.provider
// selects and the key of its configuration subtree.
const Name = "stripe"

// APIVersion is the Stripe API version this module reads events at — the
// version of the SDK it pins. Create the webhook endpoint with a version of
// the same release line (the part after the dot).
const APIVersion = stripego.APIVersion

// ReferenceKey is the metadata key the application's reference is stored
// under, on the customers and the subscriptions this module creates.
const ReferenceKey = "nucleus_reference"

func init() {
	billing.MustRegister(Name, New)
}

// Config is the billing.stripe subtree.
type Config struct {
	// SecretKey is a REFERENCE to the API key: env:STRIPE_SECRET_KEY by
	// default, or aws-sm:<secret-id>[#key].
	SecretKey string `koanf:"secret_key" default:"env:STRIPE_SECRET_KEY"`

	// WebhookSecret is a REFERENCE to the webhook endpoint's signing secret
	// (whsec_…): env:STRIPE_WEBHOOK_SECRET by default.
	WebhookSecret string `koanf:"webhook_secret" default:"env:STRIPE_WEBHOOK_SECRET"`

	// Tolerance is how old a delivery's signature may be. It bounds how long
	// a captured delivery can be replayed; within it, the framework
	// recognises the event id. Between 1s and 15m.
	Tolerance time.Duration `koanf:"tolerance" default:"5m"`

	// APIURL replaces Stripe's API address — for stripe-mock or a test
	// double. It must be https, except on the loopback interface.
	APIURL string `koanf:"api_url"`
}

// New builds the provider from its configuration subtree. It is the factory
// registered under Name; the framework calls it at boot, and an error stops
// the boot.
func New(cfg billing.Config) (billing.Provider, error) {
	var c Config
	if err := cfg.Bind(&c); err != nil {
		return nil, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return open(context.Background(), c, logger, secrets.NewChain())
}

// resolver reads a secret reference; secrets.Chain in production.
type resolver interface {
	Resolve(ctx context.Context, ref string) ([]byte, error)
}

// provider is the billing.Provider over the Stripe API.
type provider struct {
	client        *stripego.Client
	webhookSecret string
	tolerance     time.Duration
	live          bool
}

func open(ctx context.Context, c Config, logger *slog.Logger, refs resolver) (*provider, error) {
	if c.Tolerance < time.Second || c.Tolerance > 15*time.Minute {
		return nil, fmt.Errorf("billing.stripe.tolerance is %s; it is between 1s and 15m — the window a captured delivery can be replayed in", c.Tolerance)
	}
	key, err := resolveSecret(ctx, refs, "secret_key", c.SecretKey)
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(key, "pk_"):
		return nil, errors.New("billing.stripe.secret_key resolves to a publishable key (pk_…), which cannot call the API; use a restricted key (rk_…) or a secret key (sk_…)")
	case !strings.HasPrefix(key, "sk_") && !strings.HasPrefix(key, "rk_"):
		return nil, errors.New("billing.stripe.secret_key resolves to something that is not a Stripe API key (rk_… or sk_…)")
	}
	whsec, err := resolveSecret(ctx, refs, "webhook_secret", c.WebhookSecret)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(whsec, "whsec_") {
		return nil, errors.New("billing.stripe.webhook_secret resolves to something that is not a webhook signing secret (whsec_…, the endpoint's Signing secret)")
	}

	backend := &stripego.BackendConfig{
		HTTPClient:    &http.Client{Timeout: 30 * time.Second},
		LeveledLogger: leveled{logger},
	}
	if u := strings.TrimSpace(c.APIURL); u != "" {
		if err := checkAPIURL(u); err != nil {
			return nil, err
		}
		backend.URL = stripego.String(strings.TrimRight(u, "/"))
	}
	p := &provider{
		client:        stripego.NewClient(key, stripego.WithBackends(stripego.NewBackendsWithConfig(backend))),
		webhookSecret: whsec,
		tolerance:     c.Tolerance,
		live:          strings.Contains(key, "_live_"),
	}
	mode := "test"
	if p.live {
		mode = "live"
	}
	attrs := []any{"mode", mode, "api_version", APIVersion, "tolerance", c.Tolerance.String()}
	if backend.URL != nil {
		attrs = append(attrs, "api_url", *backend.URL)
	}
	logger.Info("billing: stripe provider ready", attrs...)
	return p, nil
}

// resolveSecret reads one of the two keys. The value in the configuration is
// a reference; a Stripe key written there in clear is refused rather than
// used, so a key that leaked into a repository does not also work.
func resolveSecret(ctx context.Context, refs resolver, field, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	for _, prefix := range []string{"sk_", "rk_", "pk_", "whsec_"} {
		if strings.HasPrefix(ref, prefix) {
			return "", fmt.Errorf("billing.stripe.%s holds a Stripe key in clear; it takes a reference — env:STRIPE_%s, or aws-sm:<secret-id> (nucleus add aws-sm) — so the key is never written in the configuration. Rotate the key that was written there",
				field, strings.ToUpper(field))
		}
	}
	raw, err := refs.Resolve(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("billing.stripe.%s (%s): %w", field, ref, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// checkAPIURL accepts an absolute https URL, or http on the loopback
// interface (stripe-mock, a test double): the key travels in every request.
func checkAPIURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("billing.stripe.api_url is not an absolute URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); u.Scheme == "http" && (host == "localhost" || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return errors.New("billing.stripe.api_url must be https (http only on the loopback interface): the API key is sent with every request")
}

func (p *provider) CreateCustomer(ctx context.Context, req billing.CustomerRequest) (billing.Customer, error) {
	params := &stripego.CustomerCreateParams{
		Email:    stripego.String(req.Email),
		Metadata: withReference(req.Metadata, req.Reference),
	}
	if req.Name != "" {
		params.Name = stripego.String(req.Name)
	}
	c, err := p.client.V1Customers.Create(ctx, params)
	if err != nil {
		return billing.Customer{}, apiError("create a customer", err)
	}
	return billing.Customer{ID: c.ID, Email: c.Email, Name: c.Name, Reference: c.Metadata[ReferenceKey]}, nil
}

func (p *provider) CreateCheckout(ctx context.Context, req billing.CheckoutRequest) (billing.Checkout, error) {
	// No payment_method_types: the payment methods are the account's,
	// chosen in the Dashboard, which is how Stripe recommends it.
	params := &stripego.CheckoutSessionCreateParams{
		Mode:       stripego.String(string(stripego.CheckoutSessionModeSubscription)),
		LineItems:  []*stripego.CheckoutSessionCreateLineItemParams{{Price: stripego.String(req.Price), Quantity: stripego.Int64(req.Quantity)}},
		SuccessURL: stripego.String(req.SuccessURL),
		CancelURL:  stripego.String(req.CancelURL),
		SubscriptionData: &stripego.CheckoutSessionCreateSubscriptionDataParams{
			// The reference travels on the subscription, so every event
			// about it says whose it is.
			Metadata: withReference(req.Metadata, req.Reference),
		},
	}
	if req.Reference != "" {
		params.ClientReferenceID = stripego.String(req.Reference)
	}
	switch {
	case req.Customer != "":
		params.Customer = stripego.String(req.Customer)
	case req.CustomerEmail != "":
		params.CustomerEmail = stripego.String(req.CustomerEmail)
	}
	if req.TrialDays > 0 {
		params.SubscriptionData.TrialPeriodDays = stripego.Int64(req.TrialDays)
	}
	s, err := p.client.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return billing.Checkout{}, apiError("create a checkout session", err)
	}
	return billing.Checkout{ID: s.ID, URL: s.URL, ExpiresAt: unix(s.ExpiresAt)}, nil
}

func (p *provider) CreatePortal(ctx context.Context, req billing.PortalRequest) (billing.Portal, error) {
	s, err := p.client.V1BillingPortalSessions.Create(ctx, &stripego.BillingPortalSessionCreateParams{
		Customer:  stripego.String(req.Customer),
		ReturnURL: stripego.String(req.ReturnURL),
	})
	if err != nil {
		return billing.Portal{}, apiError("create a portal session", err)
	}
	return billing.Portal{URL: s.URL}, nil
}

func (p *provider) Subscription(ctx context.Context, id string) (billing.Subscription, error) {
	s, err := p.client.V1Subscriptions.Retrieve(ctx, id, nil)
	if err != nil {
		return billing.Subscription{}, apiError("read the subscription", err)
	}
	return subscriptionOf(s), nil
}

// withReference is the metadata with the application's reference under
// ReferenceKey; nil when both are empty.
func withReference(meta map[string]string, ref string) map[string]string {
	if len(meta) == 0 && ref == "" {
		return nil
	}
	out := make(map[string]string, len(meta)+1)
	for k, v := range meta {
		out[k] = v
	}
	if ref != "" {
		out[ReferenceKey] = ref
	}
	return out
}

// apiError translates an API failure into an error that carries Stripe's
// message, code and request id — and none of the SDK's types: a resource
// Stripe does not know is billing.ErrNotFound.
func apiError(doing string, err error) error {
	var se *stripego.Error
	if !errors.As(err, &se) {
		return fmt.Errorf("stripe: %s: %v", doing, err)
	}
	msg := fmt.Sprintf("stripe: %s: %s (status %d, code %q, request %s)", doing, se.Msg, se.HTTPStatusCode, se.Code, se.RequestID)
	if se.Code == stripego.ErrorCodeResourceMissing || se.HTTPStatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", billing.ErrNotFound, msg)
	}
	return errors.New(msg)
}

func unix(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// leveled routes the SDK's log lines to the application's logger. Its
// per-request lines go to debug; what it reports as a problem, to warn.
type leveled struct{ logger *slog.Logger }

func (l leveled) Debugf(format string, v ...any) {
	l.logger.Debug("stripe: " + fmt.Sprintf(format, v...))
}
func (l leveled) Infof(format string, v ...any) {
	l.logger.Debug("stripe: " + fmt.Sprintf(format, v...))
}
func (l leveled) Warnf(format string, v ...any) {
	l.logger.Warn("stripe: " + fmt.Sprintf(format, v...))
}
func (l leveled) Errorf(format string, v ...any) {
	l.logger.Warn("stripe: " + fmt.Sprintf(format, v...))
}
