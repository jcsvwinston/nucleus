// Package nucleus — webhooks.go wires ModuleSpec.Webhooks to real routes
// (ADR-010 Phase 2). Each registration mounts a handler at
// `<webhooks_prefix>/<module-name><path>` on the application router, behind
// the framework's checks: method allow-list, request-body cap, and — when the
// spec carries a Secret — HMAC-SHA256 signature verification of the raw body
// against the X-Nucleus-Signature header. Webhooks authenticate by signature,
// not by CSRF token, so Run exempts the webhook prefix from CSRF when both
// are enabled.
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
	"strings"

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

// defaultWebhookMaxBytes caps webhook request bodies when the spec leaves
// MaxBytes unset.
const defaultWebhookMaxBytes = 1 << 20 // 1 MiB

// SignWebhookBody returns the X-Nucleus-Signature value ("sha256=<hex>") for
// body under secret — the exact string the webhook verifier expects. Exported
// for webhook senders and for tests of signed receivers.
func SignWebhookBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// verifyWebhookSignature reports whether header is a well-formed
// "sha256=<hex>" signature of body under secret, comparing in constant time.
func verifyWebhookSignature(secret string, body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(want, mac.Sum(nil))
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
	if spec.Handler == nil {
		return fail("Handler is required")
	}
	if spec.MaxBytes < 0 {
		return fail("MaxBytes %d must not be negative", spec.MaxBytes)
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
			if !verifyWebhookSignature(e.spec.Secret, body, r.Header.Get(WebhookSignatureHeader)) {
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
