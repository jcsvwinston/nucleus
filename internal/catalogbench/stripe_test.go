// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package catalogbench

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// standInStripe is the Stripe API small enough to read: it answers the
// checkout-session call the bench makes through the starter, and records
// what it was sent. The starter's billing.stripe.api_url points at it, so
// nothing reaches Stripe.
type standInStripe struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []stripeCall
}

type stripeCall struct {
	method, path, auth string
	form               url.Values
}

func newStandInStripe(tb testing.TB) *standInStripe {
	tb.Helper()
	s := &standInStripe{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		s.mu.Lock()
		s.seen = append(s.seen, stripeCall{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), form: form})
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/v1/checkout/sessions" {
			_, _ = io.WriteString(w, `{"id":"cs_test_catalogbench","object":"checkout.session","url":"https://checkout.stripe.com/c/pay/cs_test_catalogbench"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","code":"resource_missing","message":"not in the stand-in"}}`)
	}))
	tb.Cleanup(s.srv.Close)
	return s
}

func (e *env) stripeAPI() *standInStripe {
	e.stripeOnce.Do(func() { e.stripeSrv = newStandInStripe(e.tb) })
	return e.stripeSrv
}

func (s *standInStripe) calls() []stripeCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]stripeCall(nil), s.seen...)
}

// The keys the person exports after `nucleus add stripe`: a restricted test
// key and the endpoint's signing secret. The configuration holds references
// to them, never the values.
const (
	benchStripeKey     = "rk_test_catalogbench"
	benchStripeWebhook = "whsec_catalogbench"
	// benchStripeAPIVersion is the API version the module's SDK pins
	// (stripe-go v87); an endpoint at another release line is refused.
	benchStripeAPIVersion = "2026-09-30.endive"
)

func stripeEnv(*env) []string {
	return []string{"STRIPE_SECRET_KEY=" + benchStripeKey, "STRIPE_WEBHOOK_SECRET=" + benchStripeWebhook}
}

// stripeWebhookLine is the line of the recipe the probe adds the stand-in's
// address after: the person never does this; a test double does.
const stripeWebhookLine = "    webhook_secret: env:STRIPE_WEBHOOK_SECRET\n"

func stripeEdit(t *testing.T, e *env, config string) string {
	if !strings.Contains(config, stripeWebhookLine) {
		t.Logf("nucleus.yml carries no %q; booting it as written", strings.TrimSpace(stripeWebhookLine))
		return config
	}
	return strings.Replace(config, stripeWebhookLine, stripeWebhookLine+"    api_url: "+e.stripeAPI().srv.URL+"\n", 1)
}

// benchBillingModule is what an application that bills already has: a
// module that registers handlers for the billing events and starts a
// checkout. No entry writes it, so the probe adds it to the project
// `nucleus add stripe` left; it shows what it received at GET
// /bench/billing/events.
const benchBillingModule = `package main

import (
	"context"
	"errors"
	"sync"

	"github.com/jcsvwinston/nucleus/pkg/billing"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

type benchBillingState struct {
	mu     sync.Mutex
	events []billing.Event
	b      *billing.Billing
}

func benchBilling() nucleus.ModuleSpec {
	m := &benchBillingState{}
	return nucleus.Module[struct{}]{
		Name:   "benchbilling",
		Prefix: "/bench/billing",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			b, ok := nucleus.BillingFrom(rt)
			if !ok {
				return errors.New("benchbilling: the application has no billing")
			}
			m.b = b
			for _, t := range billing.EventTypes() {
				if err := b.On(t, func(_ context.Context, ev billing.Event) error {
					m.mu.Lock()
					defer m.mu.Unlock()
					m.events = append(m.events, ev)
					return nil
				}); err != nil {
					return err
				}
			}
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/events", func(c *nucleus.Context) error {
				m.mu.Lock()
				defer m.mu.Unlock()
				return c.JSON(200, append([]billing.Event{}, m.events...))
			})
			r.Post("/checkout", func(c *nucleus.Context) error {
				co, err := m.b.CreateCheckout(c.Request.Context(), billing.CheckoutRequest{
					Price: "price_catalogbench", Reference: "user-42",
					SuccessURL: "https://app.example/billing/done", CancelURL: "https://app.example/pricing",
				})
				if err != nil {
					return err
				}
				return c.JSON(200, co)
			})
		},
	}.Build()
}
`

func addBenchBilling(t *testing.T, dir string) {
	must(t, os.WriteFile(filepath.Join(dir, "benchbilling.go"), []byte(benchBillingModule), 0o644))
	mainGo := filepath.Join(dir, "main.go")
	src, err := os.ReadFile(mainGo)
	must(t, err)
	edited := strings.Replace(string(src), "\t\tStart(); err != nil", "\t\tMount(benchBilling()).\n\t\tStart(); err != nil", 1)
	if edited == string(src) {
		t.Fatalf("the starter's main.go has no Start() to mount the billing module before:\n%s", src)
	}
	must(t, os.WriteFile(mainGo, []byte(edited), 0o644))
}

// stripeEvent is a customer.subscription.updated event as a Stripe
// endpoint at benchStripeAPIVersion receives it.
func stripeEvent(id, status string) []byte {
	return []byte(fmt.Sprintf(`{"id":%q,"object":"event","api_version":%q,"created":%d,"livemode":false,`+
		`"type":"customer.subscription.updated","data":{"object":{"id":"sub_catalogbench","object":"subscription",`+
		`"customer":"cus_catalogbench","status":%q,"metadata":{"nucleus_reference":"user-42"},"items":{"object":"list","data":[`+
		`{"id":"si_1","object":"subscription_item","price":{"id":"price_catalogbench","object":"price"},"quantity":1,"current_period_end":%d}]}}}}`,
		id, benchStripeAPIVersion, time.Now().Unix(), status, time.Now().Add(30*24*time.Hour).Unix()))
}

// stripeSignature is the Stripe-Signature header Stripe sends: the
// timestamp, and an HMAC-SHA256 of "<timestamp>.<body>" under the signing
// secret. Computed here from the published scheme, not with the SDK the
// module uses, so the module's verification is measured against Stripe's
// scheme rather than against itself.
func stripeSignature(secret string, at time.Time, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", at.Unix())
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", at.Unix(), hex.EncodeToString(mac.Sum(nil)))
}

func postStripe(port int, body []byte, signature string) (int, string, error) {
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/webhooks/billing/stripe", port), bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	if signature != "" {
		req.Header.Set("Stripe-Signature", signature)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(raw)), nil
}

type benchBillingEvent struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Provider     string `json:"provider"`
	Subscription *struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		Reference string `json:"reference"`
		Price     string `json:"price"`
	} `json:"subscription"`
}

func benchBillingEvents(port int) ([]benchBillingEvent, error) {
	status, body := get(port, "/bench/billing/events")
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET /bench/billing/events answered %d: %s", status, firstLines(body, 2))
	}
	var events []benchBillingEvent
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		return nil, err
	}
	return events, nil
}

// stripeDelivers is EN-08's wiring check, against the running starter:
//
//   - a customer.subscription.updated delivery signed the way Stripe signs
//     it is answered 200, and the module's handler receives it as a typed
//     billing.subscription.updated event — status, reference and all —
//     through the outbox;
//   - the same delivery again is answered 200 and not delivered twice;
//   - a delivery signed with another secret, and one whose timestamp is
//     outside the tolerance, are answered 400 and delivered to nobody;
//   - a checkout started through the application reaches the Stripe API —
//     the stand-in — with the key the reference names.
func stripeDelivers(t *testing.T, e *env, run coreRun) (bool, string) {
	body := stripeEvent("evt_catalogbench_1", "past_due")
	sig := stripeSignature(benchStripeWebhook, time.Now(), body)
	status, answer, err := postStripe(run.port, body, sig)
	if err != nil {
		return false, err.Error()
	}
	if status != http.StatusOK {
		return false, fmt.Sprintf("a delivery signed with the endpoint's secret was answered %d %s\n%s", status, answer, firstLines(lastLines(run.output(), 6), 6))
	}

	var got []benchBillingEvent
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if got, err = benchBillingEvents(run.port); err != nil {
			return false, err.Error()
		}
		if len(got) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(got) == 0 {
		return false, "the delivery was answered 200 and no billing event reached the module's handler within 15s"
	}
	ev := got[0]
	if ev.ID != "evt_catalogbench_1" || ev.Type != "billing.subscription.updated" || ev.Provider != "stripe" || ev.Subscription == nil ||
		ev.Subscription.ID != "sub_catalogbench" || ev.Subscription.Status != "past_due" || ev.Subscription.Reference != "user-42" ||
		ev.Subscription.Price != "price_catalogbench" {
		raw, _ := json.Marshal(ev)
		return false, "the handler received " + string(raw)
	}

	if status, answer, _ := postStripe(run.port, body, sig); status != http.StatusOK || !strings.Contains(answer, `"duplicate":true`) {
		return false, fmt.Sprintf("the same delivery again was answered %d %s", status, answer)
	}
	forged := stripeEvent("evt_catalogbench_2", "canceled")
	if status, _, _ := postStripe(run.port, forged, stripeSignature("whsec_not_the_endpoints", time.Now(), forged)); status != http.StatusBadRequest {
		return false, fmt.Sprintf("a delivery signed with another secret was answered %d", status)
	}
	stale := stripeEvent("evt_catalogbench_3", "canceled")
	if status, _, _ := postStripe(run.port, stale, stripeSignature(benchStripeWebhook, time.Now().Add(-10*time.Minute), stale)); status != http.StatusBadRequest {
		return false, fmt.Sprintf("a delivery signed ten minutes ago was answered %d", status)
	}
	time.Sleep(2500 * time.Millisecond) // two passes of the outbox dispatcher
	if got, _ = benchBillingEvents(run.port); len(got) != 1 {
		return false, fmt.Sprintf("after the repeated, the forged and the stale delivery the handler has %d events, want 1", len(got))
	}

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/bench/billing/checkout", run.port), "application/json", nil)
	if err != nil {
		return false, err.Error()
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), "cs_test_catalogbench") {
		return false, fmt.Sprintf("POST /bench/billing/checkout answered %d %s", resp.StatusCode, firstLines(string(raw), 2))
	}
	var checkout *stripeCall
	for _, c := range e.stripeAPI().calls() {
		if c.path == "/v1/checkout/sessions" {
			c := c
			checkout = &c
		}
	}
	if checkout == nil || checkout.auth != "Bearer "+benchStripeKey || checkout.form.Get("mode") != "subscription" ||
		checkout.form.Get("subscription_data[metadata][nucleus_reference]") != "user-42" {
		return false, fmt.Sprintf("the stand-in Stripe API saw %+v", checkout)
	}
	return true, "a signed customer.subscription.updated reached the module's handler as billing.subscription.updated (past_due, reference user-42); " +
		"the same delivery again was answered 200 duplicate and not delivered; another secret and a ten-minute-old signature were answered 400; " +
		"a checkout reached the Stripe API with the key the reference names"
}

// stripeConfig is the selection CAT-01 boots the starter with when the
// module is not added: the block `nucleus add stripe` writes.
const stripeConfig = "billing:\n  provider: stripe\n  stripe:\n    secret_key: env:STRIPE_SECRET_KEY\n    webhook_secret: env:STRIPE_WEBHOOK_SECRET\n"
