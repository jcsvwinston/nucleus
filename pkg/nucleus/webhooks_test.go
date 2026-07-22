package nucleus

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		// Non-canonical paths (NU8-2): before the path.Clean guard these were
		// accepted and mounted at a pattern the cleaned request URL never
		// reaches (the ServeMux 307s to the cleaned path) — a webhook that is
		// mounted but unreachable, when the contract says broken
		// registrations fail boot.
		{"dot-dot segment", "/a/../b", WebhookSpec{Handler: okHandler}, "not canonical"},
		{"leading dot-dot", "/../up", WebhookSpec{Handler: okHandler}, "not canonical"},
		{"dot segment", "/a/./b", WebhookSpec{Handler: okHandler}, "not canonical"},
		{"duplicate slash", "/a//b", WebhookSpec{Handler: okHandler}, "not canonical"},
		{"trailing slash", "/github/", WebhookSpec{Handler: okHandler}, "not canonical"},
		// SEC-4: a bare "/" is canonical (path.Clean leaves it) but has no
		// non-empty segment, so it would mount a catch-all subtree under the
		// module rather than a named webhook. Reject it explicitly.
		{"root path", "/", WebhookSpec{Handler: okHandler}, "at least one non-empty segment"},
		{"whitespace-only becomes root", "  /  ", WebhookSpec{Handler: okHandler}, "at least one non-empty segment"},
		{"nil handler", "/hook", WebhookSpec{}, "Handler is required"},
		{"negative max bytes", "/hook", WebhookSpec{Handler: okHandler, MaxBytes: -1}, "must not be negative"},
		{"empty method entry", "/hook", WebhookSpec{Handler: okHandler, Methods: []string{"POST", " "}}, "empty entry"},
		// NU8-3: the timestamped scheme is only sound when the timestamp is
		// signed, and a negative tolerance is meaningless.
		{"negative timestamp tolerance", "/hook", WebhookSpec{Handler: okHandler, Secret: "k", TimestampTolerance: -time.Minute}, "must not be negative"},
		{"timestamp tolerance without secret", "/hook", WebhookSpec{Handler: okHandler, TimestampTolerance: time.Minute}, "requires a Secret"},
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

// TestWebhookRegister_RejectsUnsafeModuleName is the SEC-4 module-name guard:
// the mount pattern is `<prefix>/<module><path>`, so a module name carrying a
// slash, a dot segment, or whitespace/query characters would shift the mount
// off its own subtree — a `..` name would even escape the webhooks prefix.
// Register canonicalizes the name (same treatment the path gets) and fails
// boot on anything that is not a single clean segment.
func TestWebhookRegister_RejectsUnsafeModuleName(t *testing.T) {
	cases := []struct {
		label  string
		module string
		want   string
	}{
		{"dot-dot module", "..", "not canonical"},
		{"escaping module", "../evil", "single path segment"},
		{"slash in module", "a/b", "single path segment"},
		{"nested traversal module", "a/../../etc", "single path segment"},
		{"dot module", ".", "not canonical"},
		{"whitespace in module", "a b", "single path segment"},
		{"query char in module", "a?x", "single path segment"},
		{"empty module", "  ", "must not be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			h, _ := newTestModuleWebhooks()
			err := h.register(tc.module, "/hook", WebhookSpec{Handler: okHandler})
			if !errors.Is(err, ErrInvalidWebhookSpec) {
				t.Fatalf("want ErrInvalidWebhookSpec, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestWebhookRegister_AcceptsOrdinaryModuleNames is the positive counterpart
// of the SEC-4 module-name guard: single-segment names keep registering.
func TestWebhookRegister_AcceptsOrdinaryModuleNames(t *testing.T) {
	h, _ := newTestModuleWebhooks()
	for _, m := range []string{"billing", "reports", "ci-runner", "a.b", "v2"} {
		if err := h.register(m, "/hook", WebhookSpec{Handler: okHandler}); err != nil {
			t.Fatalf("register(module=%q) must succeed: %v", m, err)
		}
	}
}

// TestRun_RootWebhookPathFailsBoot pins SEC-4 at the public surface: a module
// registering "/" as its webhook path stops Run before any route is live,
// instead of mounting a catch-all subtree under the module.
func TestRun_RootWebhookPathFailsBoot(t *testing.T) {
	modDef := Module[struct{}]{
		Name: "greedy",
		Webhooks: func(w WebhookRegistry, _ struct{}) {
			_ = w.Register("/", WebhookSpec{Handler: okHandler})
		},
	}

	cfg := app.DefaultConfig()
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "boot.db")},
	}

	err := Run(App{
		Config:  cfg,
		Options: []app.Option{app.WithoutDefaults()},
		Modules: map[string]ModuleSpec{"greedy": modDef.Build()},
	})
	if !errors.Is(err, ErrInvalidWebhookSpec) {
		t.Fatalf("Run must fail boot with ErrInvalidWebhookSpec, got %v", err)
	}
	if !strings.Contains(err.Error(), "at least one non-empty segment") {
		t.Fatalf("boot error %q does not explain the root-path rule", err)
	}
}

// TestRun_UnsafeModuleNameFailsBoot pins SEC-4's module-name half at the
// public surface: a module whose name would traverse out of its mount subtree
// fails boot instead of mounting somewhere surprising.
func TestRun_UnsafeModuleNameFailsBoot(t *testing.T) {
	modDef := Module[struct{}]{
		Name: "../evil",
		Webhooks: func(w WebhookRegistry, _ struct{}) {
			_ = w.Register("/hook", WebhookSpec{Handler: okHandler})
		},
	}

	cfg := app.DefaultConfig()
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "boot.db")},
	}

	err := Run(App{
		Config:  cfg,
		Options: []app.Option{app.WithoutDefaults()},
		Modules: map[string]ModuleSpec{"../evil": modDef.Build()},
	})
	if !errors.Is(err, ErrInvalidWebhookSpec) {
		t.Fatalf("Run must fail boot with ErrInvalidWebhookSpec, got %v", err)
	}
	if !strings.Contains(err.Error(), "single path segment") {
		t.Fatalf("boot error %q does not explain the module-name rule", err)
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

// TestWebhookRegister_CanonicalPathsAccepted is the positive counterpart of
// the NU8-2 guard: ordinary single- and multi-segment paths (with or without
// the leading slash, which normalisation adds) keep registering.
func TestWebhookRegister_CanonicalPathsAccepted(t *testing.T) {
	h, _ := newTestModuleWebhooks()
	for _, p := range []string{"/github", "stripe", "/ci/build-finished", "/a/b/c"} {
		if err := h.register("m", p, WebhookSpec{Handler: okHandler}); err != nil {
			t.Fatalf("register(%q) must succeed for a canonical path: %v", p, err)
		}
	}
}

// TestRun_NonCanonicalWebhookPathFailsBoot pins the claim at the public
// surface: a module registering a `..` webhook path stops Run before any
// route is live, instead of leaving a mounted-but-unreachable webhook
// (NU8-2).
func TestRun_NonCanonicalWebhookPathFailsBoot(t *testing.T) {
	modDef := Module[struct{}]{
		Name: "sneaky",
		Webhooks: func(w WebhookRegistry, _ struct{}) {
			_ = w.Register("/a/../b", WebhookSpec{Handler: okHandler})
		},
	}

	cfg := app.DefaultConfig()
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "boot.db")},
	}

	err := Run(App{
		Config:  cfg,
		Options: []app.Option{app.WithoutDefaults()},
		Modules: map[string]ModuleSpec{"sneaky": modDef.Build()},
	})
	if !errors.Is(err, ErrInvalidWebhookSpec) {
		t.Fatalf("Run must fail boot with ErrInvalidWebhookSpec, got %v", err)
	}
	if !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("boot error %q does not explain the canonical-path rule", err)
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

// TestSignWebhookBodyWithTimestamp_RoundTrip: the timestamped signer emits a
// decimal Unix timestamp and a signature over `<timestamp>.<body>` that the
// timestamped verifier accepts — and that deliberately differs from the
// body-only signature (the schemes do not mix).
func TestSignWebhookBodyWithTimestamp_RoundTrip(t *testing.T) {
	body := []byte(`{"a":1}`)
	now := time.Now()

	sig, ts := SignWebhookBodyWithTimestamp("k", now, body)
	if !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("signature format: %q", sig)
	}
	if want := now.Unix(); ts != strconvFormatInt(want) {
		t.Fatalf("timestamp header value = %q, want %d as decimal", ts, want)
	}
	if !verifyWebhookSignature("k", timestampedSignatureMaterial(ts, body), sig) {
		t.Fatal("timestamped verifier must accept the timestamped signer's output")
	}
	if sig == SignWebhookBody("k", body) {
		t.Fatal("timestamped signature must differ from the body-only signature")
	}
	if verifyWebhookSignature("k", body, sig) {
		t.Fatal("a timestamped signature must not verify as a body-only signature")
	}
}

// strconvFormatInt keeps the test honest about the exact header encoding
// without importing strconv into every assertion.
func strconvFormatInt(v int64) string { return fmt.Sprintf("%d", v) }

// timestampedRequest builds a request signed under the timestamped scheme,
// with hooks to skew the signed time or overwrite the sent timestamp header.
func timestampedRequest(body, secret string, ts time.Time) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	sig, tsHeader := SignWebhookBodyWithTimestamp(secret, ts, []byte(body))
	req.Header.Set(WebhookSignatureHeader, sig)
	req.Header.Set(WebhookTimestampHeader, tsHeader)
	return req
}

// TestWebhookHandler_TimestampTolerance drives the opt-in anti-replay
// window (NU8-3): a request signed with SignWebhookBodyWithTimestamp inside
// the tolerance passes; stale, future, missing, malformed, tampered, or
// body-only-signed requests are all 401 without the module handler running.
func TestWebhookHandler_TimestampTolerance(t *testing.T) {
	h, logs := newTestModuleWebhooks()
	handlerRan := 0
	err := h.register("billing", "/stripe", WebhookSpec{
		Secret:             "s3cret",
		TimestampTolerance: 5 * time.Minute,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			handlerRan++
			w.WriteHeader(http.StatusOK)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := h.handlerFor(h.entries[0])
	const body = `{"event":"charge"}`

	// Inside the tolerance (fresh, and skewed less than 5m either way) → 200.
	for _, ts := range []time.Time{
		time.Now(),
		time.Now().Add(-4 * time.Minute),
		time.Now().Add(4 * time.Minute),
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, timestampedRequest(body, "s3cret", ts))
		if rec.Code != http.StatusOK {
			t.Fatalf("timestamp %s within tolerance: want 200, got %d", ts, rec.Code)
		}
	}
	if handlerRan != 3 {
		t.Fatalf("handler ran %d times, want 3", handlerRan)
	}

	for _, tc := range []struct {
		label string
		req   *http.Request
	}{
		{"stale timestamp", timestampedRequest(body, "s3cret", time.Now().Add(-10*time.Minute))},
		{"future timestamp", timestampedRequest(body, "s3cret", time.Now().Add(10*time.Minute))},
		{"missing timestamp with body-only signature", signedRequest(http.MethodPost, "/x", body, "s3cret")},
		{"malformed timestamp", func() *http.Request {
			r := timestampedRequest(body, "s3cret", time.Now())
			r.Header.Set(WebhookTimestampHeader, "not-a-unix-time")
			return r
		}()},
		{"tampered timestamp", func() *http.Request {
			// Signed at one instant, header rewritten to another instant that
			// is still within tolerance: the signature must not cover it.
			r := timestampedRequest(body, "s3cret", time.Now())
			_, other := SignWebhookBodyWithTimestamp("s3cret", time.Now().Add(time.Minute), []byte(body))
			r.Header.Set(WebhookTimestampHeader, other)
			return r
		}()},
		{"wrong secret", timestampedRequest(body, "other", time.Now())},
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
	if !strings.Contains(logs.String(), "timestamp") {
		t.Fatalf("expected timestamp-rejection WARNs in the boot log, got %q", logs.String())
	}
}

// TestWebhookHandler_BodyOnlyCompatWithoutTolerance pins backwards
// compatibility: with TimestampTolerance unset, the body-only scheme of
// SignWebhookBody keeps working exactly as before — no timestamp header
// required, and a stray timestamp header changes nothing.
func TestWebhookHandler_BodyOnlyCompatWithoutTolerance(t *testing.T) {
	h, _ := newTestModuleWebhooks()
	err := h.register("billing", "/github", WebhookSpec{
		Secret:  "s3cret",
		Handler: okHandler,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := h.handlerFor(h.entries[0])
	const body = `{"event":"push"}`

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, signedRequest(http.MethodPost, "/x", body, "s3cret"))
	if rec.Code != http.StatusOK {
		t.Fatalf("body-only signature without tolerance: want 200, got %d", rec.Code)
	}

	// An extra timestamp header on a body-only receiver is ignored (the
	// sender may be shared between receivers with and without tolerance).
	rec = httptest.NewRecorder()
	req := signedRequest(http.MethodPost, "/x", body, "s3cret")
	req.Header.Set(WebhookTimestampHeader, "1234567890")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stray timestamp header on a body-only receiver: want 200, got %d", rec.Code)
	}
}
