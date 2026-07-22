// Package nucleus — webhooks.go wires ModuleSpec.Webhooks to real routes
// (ADR-010 Phase 2). Each registration mounts a handler at
// `<webhooks_prefix>/<module-name><path>` on the application router, behind
// the framework's checks: method allow-list, request-body cap, and — when the
// spec carries a Secret — HMAC-SHA256 signature verification of the raw body
// against the X-Nucleus-Signature header. Webhooks authenticate by signature,
// not by CSRF token, so Run exempts the webhook prefix from CSRF when both
// are enabled.
//
// Anti-replay is a declared limit of the signature check: a valid signed
// request that is captured can be re-sent verbatim and will verify again.
// WebhookSpec.TimestampTolerance narrows the replay window by binding a
// signed X-Nucleus-Timestamp into the signature material; deduplicating by
// event ID in the handler closes it. See WebhookSpec.Secret.
package nucleus

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// ErrInvalidWebhookSpec is returned (wrapped, naming module and path) for any
// invalid WebhookRegistry.Register call. It reaches the user as a Run error:
// a module that declares a broken webhook fails boot instead of silently not
// mounting it.
var ErrInvalidWebhookSpec = errors.New("nucleus: invalid webhook registration")

// WebhookSignatureHeader carries the HMAC-SHA256 body signature a caller must
// send when the receiving WebhookSpec sets a Secret.
const WebhookSignatureHeader = "X-Nucleus-Signature"

// WebhookTimestampHeader carries the Unix-seconds send time a caller must
// include when the receiving WebhookSpec sets a TimestampTolerance. The
// value is bound into the signature material by SignWebhookBodyWithTimestamp,
// so it cannot be altered in transit without invalidating the signature.
const WebhookTimestampHeader = "X-Nucleus-Timestamp"

// defaultWebhookMaxBytes caps webhook request bodies when the spec leaves
// MaxBytes unset.
const defaultWebhookMaxBytes = 1 << 20 // 1 MiB

// SignWebhookBody returns the X-Nucleus-Signature value ("sha256=<hex>") for
// body under secret — the exact string the webhook verifier expects. Exported
// for webhook senders and for tests of signed receivers.
//
// The signature authenticates the body only: it does not bind a send time,
// so a captured request can be replayed as-is (see WebhookSpec.Secret for
// the declared limit). Receivers that set WebhookSpec.TimestampTolerance
// require SignWebhookBodyWithTimestamp instead.
func SignWebhookBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// SignWebhookBodyWithTimestamp returns the two header values a sender needs
// for a webhook receiver that sets WebhookSpec.TimestampTolerance: the
// X-Nucleus-Signature value ("sha256=<hex>") and the X-Nucleus-Timestamp
// value (ts as decimal Unix seconds). The signature covers
// `<timestamp>.<body>`, so the timestamp the verifier trusts for the
// tolerance check is exactly the one the sender signed.
//
// SignWebhookBody remains the signer for receivers without a
// TimestampTolerance; the two schemes do not mix — a body-only signature is
// rejected by a timestamped receiver and vice versa.
func SignWebhookBodyWithTimestamp(secret string, ts time.Time, body []byte) (signature, timestamp string) {
	timestamp = strconv.FormatInt(ts.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil)), timestamp
}

// verifyWebhookSignature reports whether header is a well-formed
// "sha256=<hex>" signature of material under secret, comparing in constant
// time. material is the raw body for the body-only scheme, or
// `<timestamp>.<body>` for the timestamped scheme.
func verifyWebhookSignature(secret string, material []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(material)
	return hmac.Equal(want, mac.Sum(nil))
}

// timestampedSignatureMaterial builds the `<timestamp>.<body>` byte string
// the timestamped scheme signs. tsRaw is the raw header value: signer and
// verifier must agree byte-for-byte, and the verifier separately requires it
// to parse as decimal Unix seconds before any MAC comparison.
func timestampedSignatureMaterial(tsRaw string, body []byte) []byte {
	material := make([]byte, 0, len(tsRaw)+1+len(body))
	material = append(material, tsRaw...)
	material = append(material, '.')
	material = append(material, body...)
	return material
}

// cleanWebhookPath applies path.Clean to a webhook registration path. Split
// out because register's parameter shadows the path package.
func cleanWebhookPath(p string) string {
	return path.Clean(p)
}

// webhookEntry is one accepted registration, held until mount() places it on
// the router. methods and maxBytes are the normalised effective values.
type webhookEntry struct {
	module   string
	path     string
	spec     WebhookSpec
	methods  []string
	maxBytes int64
}

// moduleWebhooks collects every module's webhook registrations and mounts
// them once the router exists.
type moduleWebhooks struct {
	logger  *slog.Logger
	entries []*webhookEntry
	paths   map[string]string // "<module><path>" -> module, duplicate guard
}

func newModuleWebhooks(logger *slog.Logger) *moduleWebhooks {
	return &moduleWebhooks{logger: logger, paths: map[string]string{}}
}

// scopedWebhookRegistry is the WebhookRegistry a single module's Webhooks
// closure sees: it stamps the module name onto every registration.
type scopedWebhookRegistry struct {
	hooks  *moduleWebhooks
	module string
	errs   []error
}

func (r *scopedWebhookRegistry) Register(path string, spec WebhookSpec) error {
	err := r.hooks.register(r.module, path, spec)
	if err != nil {
		// Recorded as well as returned — Webhooks closures have no error
		// return, so collect() converts the record into a boot failure.
		r.errs = append(r.errs, err)
	}
	return err
}

// collect invokes one module's Webhooks closure against a scoped registry
// and returns the first registration error, failing boot loudly.
func (h *moduleWebhooks) collect(spec ModuleSpec) error {
	reg := &scopedWebhookRegistry{hooks: h, module: spec.Name()}
	spec.Webhooks(reg)
	if len(reg.errs) > 0 {
		return reg.errs[0]
	}
	return nil
}

func (h *moduleWebhooks) register(module, path string, spec WebhookSpec) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: module %q webhook %q: %s", ErrInvalidWebhookSpec, module, path, fmt.Sprintf(format, args...))
	}

	// Module-name guard (SEC-4): mount() builds the pattern
	// `<prefix>/<module><path>`, so the module name occupies exactly one path
	// segment. A name controlled today by the developer, but a slash, a dot
	// segment, or whitespace/query characters would shift the mount off the
	// module's own subtree — a ".." name would escape the webhooks prefix
	// entirely (the pattern "/webhooks/../evil/hook" traverses up). Apply the
	// same canonicalization the path gets and fail boot on anything that is
	// not a single clean segment.
	if strings.TrimSpace(module) == "" {
		return fail("module name must not be empty")
	}
	if strings.ContainsAny(module, "/ \t\r\n?#") {
		return fail("module name must be a single path segment without slashes, whitespace, or query/fragment characters")
	}
	if cleaned := cleanWebhookPath("/" + module); cleaned != "/"+module {
		return fail("module name is not canonical (path.Clean rewrites it to %q); dot segments are rejected", strings.TrimPrefix(cleaned, "/"))
	}

	path = strings.TrimSpace(path)
	if path == "" {
		return fail("path must not be empty")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.ContainsAny(path, " \t\r\n?#") {
		return fail("path must not contain whitespace or query/fragment characters")
	}
	// Canonical-path guard (NU8-2): a path that path.Clean would rewrite —
	// "." or ".." segments, duplicate or trailing slashes — used to be
	// accepted here and then mounted at a pattern the cleaned request URL
	// never matches: the ServeMux answered 307 to the cleaned path and the
	// webhook was mounted-but-unreachable, while the documented contract says
	// a broken registration fails boot. Reject it loudly instead.
	if cleaned := cleanWebhookPath(path); cleaned != path {
		return fail("path is not canonical (path.Clean rewrites it to %q); dot segments, duplicate or trailing slashes are rejected", cleaned)
	}
	// Root-path guard (SEC-4): "/" is canonical (path.Clean leaves it) yet has
	// no non-empty segment, so it would mount a catch-all subtree under the
	// module — every request beneath `<prefix>/<module>/` funnelled into one
	// handler — instead of a named webhook. Require at least one segment.
	if path == "/" {
		return fail("path must have at least one non-empty segment; %q would mount a catch-all subtree under the module", "/")
	}
	if spec.Handler == nil {
		return fail("Handler is required")
	}
	if spec.MaxBytes < 0 {
		return fail("MaxBytes %d must not be negative", spec.MaxBytes)
	}
	if spec.TimestampTolerance < 0 {
		return fail("TimestampTolerance %s must not be negative", spec.TimestampTolerance)
	}
	if spec.TimestampTolerance > 0 && spec.Secret == "" {
		return fail("TimestampTolerance requires a Secret: the timestamp is only trustworthy when it is part of the signed material")
	}

	methods := spec.Methods
	if len(methods) == 0 {
		methods = []string{http.MethodPost}
	}
	normalized := make([]string, 0, len(methods))
	for _, m := range methods {
		m = strings.ToUpper(strings.TrimSpace(m))
		if m == "" {
			return fail("Methods must not contain an empty entry")
		}
		normalized = append(normalized, m)
	}

	maxBytes := spec.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultWebhookMaxBytes
	}

	key := module + path
	if owner, dup := h.paths[key]; dup {
		return fail("duplicate webhook path (already registered by module %q)", owner)
	}
	h.paths[key] = module

	h.entries = append(h.entries, &webhookEntry{
		module:   module,
		path:     path,
		spec:     spec,
		methods:  normalized,
		maxBytes: maxBytes,
	})
	return nil
}

// mount places every collected entry on the application router under prefix
// (the normalised webhooks_prefix). Unsigned webhooks are flagged at WARN —
// mounting an inbound endpoint with no framework-side authentication is
// legitimate only when the handler authenticates the caller itself, and that
// choice should be visible in the boot log.
func (h *moduleWebhooks) mount(core *app.App, prefix string) {
	for _, e := range h.entries {
		pattern := prefix + "/" + e.module + e.path
		core.Router.Mux.Handle(pattern, h.handlerFor(e))
		if e.spec.Secret == "" {
			h.logger.Warn("nucleus: webhook mounted without signature verification; its handler must authenticate callers itself",
				"module", e.module, "path", pattern)
		} else {
			h.logger.Info("nucleus: webhook mounted",
				"module", e.module, "path", pattern)
		}
	}
}

// handlerFor builds the HTTP handler enforcing the entry's checks in order:
// method (405 + Allow), body cap (413), signature when a Secret is set (401),
// then the module handler with the body replayed.
func (h *moduleWebhooks) handlerFor(e *webhookEntry) http.Handler {
	allowed := make(map[string]struct{}, len(e.methods))
	for _, m := range e.methods {
		allowed[m] = struct{}{}
	}
	allowHeader := strings.Join(e.methods, ", ")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowed[r.Method]; !ok {
			w.Header().Set("Allow", allowHeader)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, e.maxBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		if e.spec.Secret != "" {
			material := body
			if e.spec.TimestampTolerance > 0 {
				// Timestamped scheme: the header must parse as Unix seconds,
				// sit within ±tolerance of now, and be part of the signed
				// material (SignWebhookBodyWithTimestamp). The raw header
				// string enters the MAC, so signer and verifier cannot drift
				// on encoding.
				tsRaw := r.Header.Get(WebhookTimestampHeader)
				ts, err := strconv.ParseInt(tsRaw, 10, 64)
				if err != nil {
					h.logger.Warn("nucleus: webhook timestamp missing or malformed",
						"module", e.module, "path", r.URL.Path)
					http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
					return
				}
				if skew := time.Since(time.Unix(ts, 0)); skew > e.spec.TimestampTolerance || skew < -e.spec.TimestampTolerance {
					h.logger.Warn("nucleus: webhook timestamp outside tolerance",
						"module", e.module, "path", r.URL.Path, "skew", skew, "tolerance", e.spec.TimestampTolerance)
					http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
					return
				}
				material = timestampedSignatureMaterial(tsRaw, body)
			}
			if !verifyWebhookSignature(e.spec.Secret, material, r.Header.Get(WebhookSignatureHeader)) {
				h.logger.Warn("nucleus: webhook signature verification failed",
					"module", e.module, "path", r.URL.Path)
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}
		}

		// Replay the body for the module handler: it was consumed above for
		// the size cap and signature check.
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		e.spec.Handler(w, r)
	})
}

// webhookPathPrefix normalises the configured webhooks_prefix: guaranteed
// leading "/", no trailing "/", default "/webhooks" when unset.
func webhookPathPrefix(cfg *app.Config) string {
	prefix := "/webhooks"
	if cfg != nil && strings.TrimSpace(cfg.WebhooksPrefix) != "" {
		prefix = strings.TrimSpace(cfg.WebhooksPrefix)
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimRight(prefix, "/")
}

// anyModuleDeclaresWebhooks reports whether at least one mounted module has a
// Webhooks closure — decided via the moduleIntrospector view, before any
// closure runs, so Run can exempt the webhook prefix from CSRF ahead of
// app.New building the middleware stack.
func anyModuleDeclaresWebhooks(specs map[string]ModuleSpec) bool {
	for _, spec := range specs {
		if intro, ok := spec.(moduleIntrospector); ok && intro.hasWebhooks() {
			return true
		}
	}
	return false
}
