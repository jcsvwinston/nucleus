// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/billing"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// testpay is a billing provider small enough to read: it signs the way the
// common providers do — an HMAC of "<timestamp>.<body>" under the endpoint's
// secret, the timestamp within a tolerance — and its events are JSON of the
// fields the seam carries. It exercises the framework's half of the webhook:
// what is answered, what is stored, what is delivered and how often.
const (
	testpaySecret    = "testpay-endpoint-secret"
	testpayTolerance = 5 * time.Minute
)

func init() {
	billing.MustRegister("testpay", func(cfg billing.Config) (billing.Provider, error) {
		var c struct {
			Secret string `koanf:"secret"`
		}
		if err := cfg.Bind(&c); err != nil {
			return nil, err
		}
		if c.Secret == "" {
			return nil, errors.New("billing.testpay.secret is required")
		}
		return &testpay{secret: c.Secret}, nil
	})
}

type testpay struct{ secret string }

func (p *testpay) CreateCustomer(_ context.Context, r billing.CustomerRequest) (billing.Customer, error) {
	return billing.Customer{ID: "cus_testpay", Email: r.Email, Reference: r.Reference}, nil
}

func (p *testpay) CreateCheckout(_ context.Context, r billing.CheckoutRequest) (billing.Checkout, error) {
	return billing.Checkout{ID: "cs_testpay", URL: "https://pay.example/cs_testpay?ref=" + r.Reference}, nil
}

func (p *testpay) CreatePortal(context.Context, billing.PortalRequest) (billing.Portal, error) {
	return billing.Portal{URL: "https://pay.example/portal"}, nil
}

func (p *testpay) Subscription(_ context.Context, id string) (billing.Subscription, error) {
	return billing.Subscription{ID: id, Status: billing.StatusActive}, nil
}

func (p *testpay) ParseWebhook(header http.Header, body []byte) (billing.Event, error) {
	ts, sig, ok := strings.Cut(header.Get("Testpay-Signature"), ",")
	if !ok {
		return billing.Event{}, fmt.Errorf("%w: no signature", billing.ErrSignature)
	}
	sent, err := strconv.ParseInt(strings.TrimPrefix(ts, "t="), 10, 64)
	if err != nil {
		return billing.Event{}, fmt.Errorf("%w: no timestamp", billing.ErrSignature)
	}
	if !hmac.Equal([]byte(strings.TrimPrefix(sig, "v1=")), []byte(testpaySign(p.secret, sent, body))) {
		return billing.Event{}, fmt.Errorf("%w: the signature does not match", billing.ErrSignature)
	}
	if age := time.Since(time.Unix(sent, 0)); age > testpayTolerance || age < -testpayTolerance {
		return billing.Event{}, fmt.Errorf("%w: the timestamp is outside the tolerance", billing.ErrSignature)
	}
	var ev billing.Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return billing.Event{}, err
	}
	if !ev.Type.Known() {
		return billing.Event{}, fmt.Errorf("%w: %s", billing.ErrUnsupportedEvent, ev.Type)
	}
	return ev, nil
}

func testpaySign(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// billingConsumer is a module of the application: it takes the billing in
// OnStart and registers a handler, the way the documentation says to.
type billingConsumer struct {
	mu       sync.Mutex
	got      []billing.Event
	failures int // the handler fails this many times before it succeeds
	b        *billing.Billing
}

func (m *billingConsumer) spec() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name: "plans",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			b, ok := nucleus.BillingFrom(rt)
			if !ok {
				return errors.New("plans: no billing")
			}
			m.b = b
			return b.On(billing.SubscriptionUpdated, func(_ context.Context, ev billing.Event) error {
				m.mu.Lock()
				defer m.mu.Unlock()
				if m.failures > 0 {
					m.failures--
					return errors.New("plans: the database is down")
				}
				m.got = append(m.got, ev)
				return nil
			})
		},
	}.Build()
}

func (m *billingConsumer) delivered() []billing.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]billing.Event(nil), m.got...)
}

// waitDelivered waits for n deliveries; the outbox dispatcher polls once a
// second.
func (m *billingConsumer) waitDelivered(t *testing.T, n int) []billing.Event {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if got := m.delivered(); len(got) >= n {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%d event(s) delivered after 15s, want %d", len(m.delivered()), n)
	return nil
}

const billingBlock = "billing:\n  provider: testpay\n  testpay:\n    secret: " + testpaySecret + "\noutbox:\n  enabled: true\n  retry_backoff: 50ms\n"

func startBilling(t *testing.T, block string, m *billingConsumer) *nucleustest.Server {
	t.Helper()
	srv := nucleustest.Start(t, nucleus.New().
		FromConfigFile(starterConfig(t, block)).
		WithOpenAuthz().
		WithoutDefaults().
		Mount(nucleus.BillingWebhook()).
		Mount(m.spec()))
	t.Cleanup(srv.Stop)
	return srv
}

const subscriptionUpdated = `{"id":"evt_1","type":"billing.subscription.updated","occurred_at":"2026-10-05T10:00:00Z",` +
	`"subscription":{"id":"sub_1","customer":"cus_1","status":"past_due","reference":"user-42"}}`

func postBillingWebhook(t *testing.T, srv *nucleustest.Server, body string, signature string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.BaseURL+"/webhooks/billing/testpay", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if signature != "" {
		req.Header.Set("Testpay-Signature", signature)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(raw))
}

func signedNow(body string) string {
	now := time.Now().Unix()
	return fmt.Sprintf("t=%d,v1=%s", now, testpaySign(testpaySecret, now, []byte(body)))
}

// A verified event is stored, answered 200 and delivered to the handler as
// the typed event; the same delivery again — the provider's retry, or a
// replay inside the tolerance — is answered 200 and not delivered twice.
func TestBillingWebhook_DeliversAVerifiedEventOnce(t *testing.T) {
	m := &billingConsumer{}
	srv := startBilling(t, billingBlock, m)

	sig := signedNow(subscriptionUpdated)
	if status, body := postBillingWebhook(t, srv, subscriptionUpdated, sig); status != http.StatusOK || body != `{"received":true}` {
		t.Fatalf("a verified event: %d %s", status, body)
	}
	got := m.waitDelivered(t, 1)
	ev := got[0]
	if ev.ID != "evt_1" || ev.Type != billing.SubscriptionUpdated || ev.Provider != "testpay" ||
		ev.Subscription == nil || ev.Subscription.Status != billing.StatusPastDue || ev.Subscription.Reference != "user-42" {
		t.Fatalf("delivered %+v", ev)
	}

	if status, body := postBillingWebhook(t, srv, subscriptionUpdated, sig); status != http.StatusOK || !strings.Contains(body, `"duplicate":true`) {
		t.Fatalf("the same delivery again: %d %s", status, body)
	}
	time.Sleep(2500 * time.Millisecond) // two dispatcher passes
	if n := len(m.delivered()); n != 1 {
		t.Fatalf("the event was delivered %d times", n)
	}
	if rows := outboxRows(t, srv, "billing:testpay:evt_1"); rows != 1 {
		t.Fatalf("the outbox holds %d rows for the event", rows)
	}
}

// What fails verification is answered 400 and goes nowhere: not stored,
// not delivered.
func TestBillingWebhook_RefusesWhatFailsVerification(t *testing.T) {
	m := &billingConsumer{}
	srv := startBilling(t, billingBlock, m)

	stale := time.Now().Add(-testpayTolerance - time.Minute).Unix()
	cases := map[string]string{
		"no signature":                "",
		"a forged signature":          fmt.Sprintf("t=%d,v1=%s", time.Now().Unix(), testpaySign("not-the-secret", time.Now().Unix(), []byte(subscriptionUpdated))),
		"a signature of another body": signedNow(strings.Replace(subscriptionUpdated, "past_due", "active", 1)),
		"an expired timestamp":        fmt.Sprintf("t=%d,v1=%s", stale, testpaySign(testpaySecret, stale, []byte(subscriptionUpdated))),
	}
	for name, sig := range cases {
		if status, body := postBillingWebhook(t, srv, subscriptionUpdated, sig); status != http.StatusBadRequest || strings.Contains(body, "received\":true") {
			t.Errorf("%s: %d %s", name, status, body)
		}
	}
	time.Sleep(1500 * time.Millisecond)
	if got := m.delivered(); len(got) != 0 {
		t.Fatalf("a refused delivery reached the handler: %+v", got)
	}
	if rows := outboxRows(t, srv, "billing:testpay:evt_1"); rows != 0 {
		t.Fatalf("a refused delivery was stored (%d rows)", rows)
	}

	// The route is POST only.
	if resp := srv.Get("/webhooks/billing/testpay"); resp.Status != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", resp.Status)
	}
}

// A verified event of a kind the seam does not carry is acknowledged — the
// provider stops retrying it — and stored nowhere.
func TestBillingWebhook_AcknowledgesWhatItDoesNotCarry(t *testing.T) {
	m := &billingConsumer{}
	srv := startBilling(t, billingBlock, m)
	body := `{"id":"evt_9","type":"billing.invoice.paid"}`
	if status, answer := postBillingWebhook(t, srv, body, signedNow(body)); status != http.StatusOK || !strings.Contains(answer, `"delivered":false`) {
		t.Fatalf("%d %s", status, answer)
	}
	if rows := outboxRows(t, srv, "billing:testpay:evt_9"); rows != 0 {
		t.Fatalf("stored %d rows", rows)
	}
}

// A handler that fails gets the event again, with backoff, until it takes it.
func TestBillingWebhook_RetriesAFailingHandler(t *testing.T) {
	m := &billingConsumer{failures: 2}
	srv := startBilling(t, billingBlock, m)
	if status, body := postBillingWebhook(t, srv, subscriptionUpdated, signedNow(subscriptionUpdated)); status != http.StatusOK {
		t.Fatalf("%d %s", status, body)
	}
	if got := m.waitDelivered(t, 1); got[0].ID != "evt_1" {
		t.Fatalf("delivered %+v", got)
	}
	if m.failures != 0 {
		t.Fatalf("the handler still has %d failures to give", m.failures)
	}
}

// The route needs a provider and the outbox, and says which is missing.
func TestBillingWebhook_RefusesToStartWithoutWhatItNeeds(t *testing.T) {
	cases := map[string]struct{ block, want string }{
		"no provider": {block: "outbox:\n  enabled: true\n", want: "billing.provider"},
		"no outbox":   {block: "billing:\n  provider: testpay\n  testpay:\n    secret: s\n", want: "outbox.enabled: true"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			a, err := nucleus.New().FromConfigFile(starterConfig(t, c.block)).WithOpenAuthz().WithoutDefaults().
				Mount(nucleus.BillingWebhook()).Build()
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			a.Config.Port = 0
			err = nucleus.RunContext(ctx, a)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Run = %v, want an error naming %s", err, c.want)
			}
		})
	}
}

// BillingFrom reports no billing when the block selects none, and the
// selected provider's handle when it does — the one CreateCheckout goes
// through.
func TestBillingFrom(t *testing.T) {
	srv := nucleustest.Start(t, nucleus.New().FromConfigFile(starterConfig(t, "")).WithOpenAuthz().WithoutDefaults())
	t.Cleanup(srv.Stop)
	if b, ok := nucleus.BillingFrom(srv.Runtime()); ok || b != nil {
		t.Fatalf("BillingFrom without a billing block = %v, %v", b, ok)
	}

	m := &billingConsumer{}
	srv = startBilling(t, billingBlock, m)
	b, ok := nucleus.BillingFrom(srv.Runtime())
	if !ok || b != m.b || b.Name() != "testpay" {
		t.Fatalf("BillingFrom = %v, %v; the module got %v", b, ok, m.b)
	}
	co, err := b.CreateCheckout(t.Context(), billing.CheckoutRequest{
		Price: "price_1", Reference: "user-42", SuccessURL: "https://app.example/ok", CancelURL: "https://app.example/pricing",
	})
	if err != nil || co.URL != "https://pay.example/cs_testpay?ref=user-42" {
		t.Fatalf("CreateCheckout = %+v, %v", co, err)
	}
}

// outboxRows counts the outbox rows with the given id in the application's
// default database.
func outboxRows(t *testing.T, srv *nucleustest.Server, id string) int {
	t.Helper()
	var db *sql.DB = srv.Runtime().DB()
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM nucleus_outbox WHERE id = ?", id).Scan(&n); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	return n
}
