package nucleus

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func newTestModuleWebhooks() (*moduleWebhooks, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return newModuleWebhooks(slog.New(slog.NewTextHandler(buf, nil))), buf
}

func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

// TestWebhookRegister_Validation drives every rejection path of
// WebhookRegistry.Register: each must wrap ErrInvalidWebhookSpec (these
// errors fail application boot).
func TestWebhookRegister_Validation(t *testing.T) {
	cases := []struct {
		label string
		path  string
		spec  WebhookSpec
		want  string
	}{
		{"empty path", "", WebhookSpec{Handler: okHandler}, "path must not be empty"},
		{"whitespace path", "/a b", WebhookSpec{Handler: okHandler}, "must not contain whitespace"},
		{"query in path", "/a?x=1", WebhookSpec{Handler: okHandler}, "must not contain whitespace"},
		{"nil handler", "/hook", WebhookSpec{}, "Handler is required"},
		{"negative max bytes", "/hook", WebhookSpec{Handler: okHandler, MaxBytes: -1}, "must not be negative"},
		{"empty method entry", "/hook", WebhookSpec{Handler: okHandler, Methods: []string{"POST", " "}}, "empty entry"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			h, _ := newTestModuleWebhooks()
			err := h.register("billing", tc.path, tc.spec)
			if !errors.Is(err, ErrInvalidWebhookSpec) {
				t.Fatalf("want ErrInvalidWebhookSpec, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestWebhookRegister_NormalizesAndDeduplicates: a path without a leading
// slash gains one; the same path twice in one module is rejected; the same
// path in another module is fine (the mount pattern embeds the module name).
func TestWebhookRegister_NormalizesAndDeduplicates(t *testing.T) {
	h, _ := newTestModuleWebhooks()
	if err := h.register("billing", "github", WebhookSpec{Handler: okHandler}); err != nil {
		t.Fatalf("register without leading slash: %v", err)
	}
	if got := h.entries[0].path; got != "/github" {
		t.Fatalf("path not normalised: got %q, want %q", got, "/github")
	}
	if err := h.register("billing", "/github", WebhookSpec{Handler: okHandler}); !errors.Is(err, ErrInvalidWebhookSpec) {
		t.Fatalf("duplicate path in module: want ErrInvalidWebhookSpec, got %v", err)
	}
	if err := h.register("reports", "/github", WebhookSpec{Handler: okHandler}); err != nil {
		t.Fatalf("same path in another module must be accepted: %v", err)
	}
}

// TestWebhookRegister_Defaults locks the effective defaults: POST-only and a
// 1 MiB body cap.
func TestWebhookRegister_Defaults(t *testing.T) {
	h, _ := newTestModuleWebhooks()
	if err := h.register("m", "/hook", WebhookSpec{Handler: okHandler}); err != nil {
		t.Fatal(err)
	}
	e := h.entries[0]
	if len(e.methods) != 1 || e.methods[0] != http.MethodPost {
		t.Fatalf("default methods: got %v, want [POST]", e.methods)
	}
	if e.maxBytes != defaultWebhookMaxBytes {
		t.Fatalf("default max bytes: got %d, want %d", e.maxBytes, defaultWebhookMaxBytes)
	}
}

// TestWebhooksCollect_RegistrationErrorFailsCollect mirrors the jobs-side
// boot-failure contract for webhooks.
func TestWebhooksCollect_RegistrationErrorFailsCollect(t *testing.T) {
	h, _ := newTestModuleWebhooks()
	spec := Module[struct{}]{
		Name: "broken",
		Webhooks: func(r WebhookRegistry, _ struct{}) {
			_ = r.Register("/bad", WebhookSpec{}) // nil handler
		},
	}.Build()
	if err := h.collect(spec); !errors.Is(err, ErrInvalidWebhookSpec) {
		t.Fatalf("collect must surface the registration error, got %v", err)
	}
}

// signedRequest builds a request whose body is signed under secret when
// secret is non-empty.
func signedRequest(method, target, body, secret string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if secret != "" {
		req.Header.Set(WebhookSignatureHeader, SignWebhookBody(secret, []byte(body)))
	}
	return req
}

// TestWebhookHandler_SignatureVerification is the HMAC gate: valid signature
// passes with the body replayed intact; missing, malformed, and
// wrongly-keyed signatures are all 401 without the module handler running.
func TestWebhookHandler_SignatureVerification(t *testing.T) {
	h, logs := newTestModuleWebhooks()

	var gotBody string
	handlerRan := 0
	err := h.register("billing", "/github", WebhookSpec{
		Secret: "s3cret",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			handlerRan++
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(http.StatusOK)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := h.handlerFor(h.entries[0])

	// Valid signature → 200, body replayed for the handler.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, signedRequest(http.MethodPost, "/webhooks/billing/github", `{"event":"push"}`, "s3cret"))
	if rec.Code != http.StatusOK {
		t.Fatalf("signed request: want 200, got %d", rec.Code)
	}
	if gotBody != `{"event":"push"}` {
		t.Fatalf("handler must see the replayed body, got %q", gotBody)
	}

	// Missing, malformed, wrong-key signatures → 401, handler untouched.
	for _, tc := range []struct {
		label string
		req   *http.Request
	}{
		{"missing signature", signedRequest(http.MethodPost, "/x", "body", "")},
		{"wrong secret", signedRequest(http.MethodPost, "/x", "body", "other-secret")},
		{"malformed header", func() *http.Request {
			r := signedRequest(http.MethodPost, "/x", "body", "")
			r.Header.Set(WebhookSignatureHeader, "sha256=zz-not-hex")
			return r
		}()},
		{"no sha256 prefix", func() *http.Request {
			r := signedRequest(http.MethodPost, "/x", "body", "")
			r.Header.Set(WebhookSignatureHeader, SignWebhookBody("s3cret", []byte("body"))[len("sha256="):])
			return r
		}()},
	} {
		rec := httptest.NewRecorder()
		before := handlerRan
		handler.ServeHTTP(rec, tc.req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: want 401, got %d", tc.label, rec.Code)
		}
		if handlerRan != before {
			t.Errorf("%s: module handler must not run", tc.label)
		}
	}
	if !strings.Contains(logs.String(), "signature verification failed") {
		t.Fatalf("expected the verification-failure WARN, got %q", logs.String())
	}
}

// TestWebhookHandler_MethodAndSizeLimits: disallowed method → 405 with Allow;
// oversized body → 413. Both before signature checking or the handler.
func TestWebhookHandler_MethodAndSizeLimits(t *testing.T) {
	h, _ := newTestModuleWebhooks()
	err := h.register("m", "/hook", WebhookSpec{
		Handler:  okHandler,
		Secret:   "k",
		MaxBytes: 16,
		Methods:  []string{"post", "PUT"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := h.entries[0]
	if e.methods[0] != "POST" || e.methods[1] != "PUT" {
		t.Fatalf("methods must be upper-cased, got %v", e.methods)
	}
	handler := h.handlerFor(e)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: want 405, got %d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "POST, PUT" {
		t.Fatalf("Allow header: got %q, want %q", allow, "POST, PUT")
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, signedRequest(http.MethodPost, "/x", strings.Repeat("A", 64), "k"))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: want 413, got %d", rec.Code)
	}
}

// TestWebhooksMount_RoutesAndUnsignedWarn mounts collected entries on a real
// router: the route serves under <prefix>/<module><path>, and an entry
// without a Secret triggers the boot WARN.
func TestWebhooksMount_RoutesAndUnsignedWarn(t *testing.T) {
	h, logs := newTestModuleWebhooks()
	if err := h.register("billing", "/github", WebhookSpec{Handler: okHandler}); err != nil {
		t.Fatal(err)
	}

	cfg := app.DefaultConfig()
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://:memory:"},
	}
	core, err := app.New(&cfg, app.WithoutDefaults())
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = core.Shutdown(t.Context()) })
	h.mount(core, "/webhooks")

	rec := httptest.NewRecorder()
	core.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/billing/github", strings.NewReader("{}")))
	if rec.Code != http.StatusOK {
		t.Fatalf("mounted webhook: want 200, got %d", rec.Code)
	}
	if !strings.Contains(logs.String(), "without signature verification") {
		t.Fatalf("expected the unsigned-webhook WARN, got %q", logs.String())
	}
}

// TestWebhookPathPrefix covers the webhooks_prefix normalisation.
func TestWebhookPathPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "/webhooks"},
		{"  ", "/webhooks"},
		{"/hooks", "/hooks"},
		{"hooks", "/hooks"},
		{"/hooks/", "/hooks"},
	}
	for _, tc := range cases {
		cfg := &app.Config{WebhooksPrefix: tc.in}
		if got := webhookPathPrefix(cfg); got != tc.want {
			t.Errorf("webhookPathPrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := webhookPathPrefix(nil); got != "/webhooks" {
		t.Errorf("webhookPathPrefix(nil) = %q, want /webhooks", got)
	}
}

// TestAnyModuleDeclaresWebhooks feeds the pre-app.New CSRF-exemption
// decision.
func TestAnyModuleDeclaresWebhooks(t *testing.T) {
	with := Module[struct{}]{Name: "w", Webhooks: func(WebhookRegistry, struct{}) {}}.Build()
	without := Module[struct{}]{Name: "p"}.Build()

	if anyModuleDeclaresWebhooks(map[string]ModuleSpec{"p": without}) {
		t.Fatal("no module declares webhooks; want false")
	}
	if !anyModuleDeclaresWebhooks(map[string]ModuleSpec{"p": without, "w": with}) {
		t.Fatal("one module declares webhooks; want true")
	}
}

// TestSignWebhookBody_RoundTrip: the exported signer produces exactly what
// the verifier accepts.
func TestSignWebhookBody_RoundTrip(t *testing.T) {
	body := []byte(`{"a":1}`)
	sig := SignWebhookBody("k", body)
	if !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("signature format: %q", sig)
	}
	if !verifyWebhookSignature("k", body, sig) {
		t.Fatal("verifier must accept the signer's output")
	}
	if verifyWebhookSignature("other", body, sig) {
		t.Fatal("verifier must reject a signature under another key")
	}
}
