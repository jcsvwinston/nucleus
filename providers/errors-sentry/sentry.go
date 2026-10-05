// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package sentry reports a Nucleus application's unhandled errors to Sentry:
// the errors handlers return that the framework answers with a 500, and the
// panics it recovers — each with the request it happened on.
//
// It is a request interceptor (ADR-029). Import it for its side effect, once,
// in the program that assembles the application, and put it in the request
// path:
//
//	import _ "github.com/jcsvwinston/nucleus/providers/errors-sentry"
//
//	# nucleus.yml
//	http_interceptors: [sentry]
//	interceptors:
//	  sentry:
//	    dsn: https://<public key>@o0.ingest.sentry.io/<project id>
//	    environment: production
//	    sample_rate: 1.0
//
// `nucleus add sentry` does both. An empty dsn, environment or release is
// read from SENTRY_DSN, SENTRY_ENVIRONMENT or SENTRY_RELEASE; with no DSN at
// all the interceptor stays in place and sends nothing, and says so once at
// boot. A key under interceptors.sentry that this package does not declare
// fails the boot, naming it.
//
// Every event carries the request's method, its route template
// (/items/{id}, not /items/42), the status it was answered with, the request
// id the framework assigned and, when the request is authenticated, the id
// of the user the framework attributes it to. What leaves the process is
// scrubbed first: a header or query parameter whose name is on the
// framework's redaction list (pkg/observe, the list log lines are redacted
// with, plus redact_extra_keys) is replaced with [REDACTED]; Sentry's own
// default denylist still applies on top; cookies, request bodies and the
// client's address are never sent.
//
// The SDK it wraps, github.com/getsentry/sentry-go, is linked only by
// applications that import this package: the framework does not depend on
// it (ADR-030, ADR-031).
package sentry

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

	sentrygo "github.com/getsentry/sentry-go"

	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/router/interceptor"
)

// Name is the name the interceptor is registered under: what
// http_interceptors lists and the key of its configuration subtree.
const Name = "sentry"

func init() {
	if err := interceptor.Register(Name, New); err != nil {
		// Two packages claiming "sentry" would make the reporter depend on
		// the order of an import block; the registry refuses, and so does
		// this program.
		panic(err)
	}
}

// Config is the interceptors.sentry subtree.
type Config struct {
	// DSN is the project's client key, from Sentry's project settings.
	// Empty reads SENTRY_DSN; with neither set nothing is sent.
	DSN string `koanf:"dsn"`

	// Environment names the deployment (production, staging). Empty reads
	// SENTRY_ENVIRONMENT.
	Environment string `koanf:"environment"`

	// Release names the build. Empty reads SENTRY_RELEASE, then the commit
	// variables of the common CI systems, then the VCS revision the binary
	// was built from.
	Release string `koanf:"release"`

	// SampleRate is the fraction of events sent, in (0, 1]. 0 is read as
	// unset and means 1; to send nothing, take sentry out of
	// http_interceptors or leave the DSN empty.
	SampleRate float64 `koanf:"sample_rate" default:"1"`

	// RedactExtraKeys are header and query-parameter names redacted beyond
	// the framework's list, matched the same way (whole name, any case). Give
	// it the keys log_redact_extra_keys names, so an event redacts what a
	// log line does.
	RedactExtraKeys []string `koanf:"redact_extra_keys"`
}

// New builds the interceptor from its configuration subtree. It is the
// factory registered under Name; the framework calls it at boot.
func New(cfg interceptor.Config) (interceptor.Interceptor, error) {
	var c Config
	if err := cfg.Bind(&c); err != nil {
		return nil, err
	}
	rep, err := newReporter(c)
	if err != nil {
		return nil, err
	}
	logBoot(slog.Default(), rep)
	return rep.intercept, nil
}

// reporter turns a failed request into an event and hands it to the client.
type reporter struct {
	client *sentrygo.Client
	redact map[string]struct{}
}

// newReporter builds the client from the configuration.
func newReporter(c Config) (*reporter, error) {
	if c.SampleRate < 0 || c.SampleRate > 1 {
		return nil, fmt.Errorf("interceptors.sentry.sample_rate is %v; it is a fraction of the events to send, between 0 and 1", c.SampleRate)
	}
	redact := map[string]struct{}{}
	for _, k := range observe.DefaultRedactedKeys() {
		redact[k] = struct{}{}
	}
	for _, k := range c.RedactExtraKeys {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			redact[k] = struct{}{}
		}
	}
	rep := &reporter{redact: redact}

	client, err := sentrygo.NewClient(sentrygo.ClientOptions{
		Dsn:         strings.TrimSpace(c.DSN),
		Environment: c.Environment,
		Release:     c.Release,
		SampleRate:  c.SampleRate,
		// The events are built here, request and all, from what the
		// request carries; nothing is collected implicitly. SendDefaultPII
		// stays false, which keeps the SDK's own denylist (and its rule
		// against the client's address) in force under the redaction.
		SendDefaultPII: false,
		BeforeSend: func(event *sentrygo.Event, _ *sentrygo.EventHint) *sentrygo.Event {
			rep.scrub(event)
			return event
		},
	})
	if err != nil {
		return nil, dsnError(err)
	}
	rep.client = client
	return rep, nil
}

// dsnError says what is wrong with a DSN without repeating it: a DSN carries
// the project's key, and "dsn" is on the redaction list for that reason.
func dsnError(err error) error {
	var parse *sentrygo.DsnParseError
	if errors.As(err, &parse) {
		reason := parse.Message
		if strings.HasPrefix(reason, "invalid url") {
			reason = "it does not parse as a URL"
		}
		return fmt.Errorf("interceptors.sentry.dsn is not a Sentry DSN (%s); it reads https://<public key>@<host>/<project id>", reason)
	}
	return fmt.Errorf("sentry: %w", err)
}

// enabled reports whether events go anywhere: a client without a DSN sends
// nothing.
func (rep *reporter) enabled() bool {
	return rep.client.Options().Dsn != ""
}

// logBoot says once what the interceptor will do — and, when it will do
// nothing, why — without printing the key the DSN carries.
func logBoot(logger *slog.Logger, rep *reporter) {
	if !rep.enabled() {
		logger.Warn("sentry: no DSN (interceptors.sentry.dsn or SENTRY_DSN): the interceptor is in place and sends nothing")
		return
	}
	opts := rep.client.Options()
	host, project := "", ""
	if dsn, err := sentrygo.NewDsn(opts.Dsn); err == nil {
		host, project = dsn.GetHost(), dsn.GetProjectID()
	}
	logger.Info("sentry: reporting handler errors answered with a 500 and recovered panics",
		"host", host, "project", project, "environment", opts.Environment, "sample_rate", opts.SampleRate)
}

// intercept is the interceptor: the writer handed down is where the router
// reports the error behind a 500 (interceptor.ErrorReporter), and a panic is
// recovered here, reported and re-raised, so the framework still answers
// 500 and logs the stack.
func (rep *reporter) intercept(next http.Handler) http.Handler {
	if !rep.enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &writer{ResponseWriter: w, rep: rep}
		defer func() {
			if rv := recover(); rv != nil {
				// http.ErrAbortHandler is how a handler stops a response on
				// purpose; it is not a failure to report.
				if err, ok := rv.(error); !ok || !errors.Is(err, http.ErrAbortHandler) {
					rep.capture(r, rw.statusOr(http.StatusInternalServerError), panicError(rv), sentrygo.LevelFatal, "panic")
				}
				panic(rv)
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// panicError is what a panic value is reported as: an error stays itself (a
// runtime error keeps its type), anything else becomes an error whose type
// reads "panic".
func panicError(rv any) error {
	if err, ok := rv.(error); ok {
		return err
	}
	return recovered{value: rv}
}

type recovered struct{ value any }

func (p recovered) Error() string { return fmt.Sprint(p.value) }

// capture builds the event for a failed request and sends it.
func (rep *reporter) capture(r *http.Request, status int, err error, level sentrygo.Level, source string) {
	event := rep.client.EventFromException(err, level)
	if _, ok := err.(recovered); ok && len(event.Exception) > 0 {
		event.Exception[len(event.Exception)-1].Type = "panic"
	}
	ctx := r.Context()
	route := router.RouteFromContext(ctx)
	if route == "" {
		route = r.URL.Path
	}
	event.Transaction = r.Method + " " + route
	event.Tags = map[string]string{
		"http.method":      r.Method,
		"http.route":       route,
		"http.status_code": fmt.Sprint(status),
		"nucleus.source":   source,
	}
	if id := observe.RequestIDFromCtx(ctx); id != "" {
		event.Tags["request_id"] = id
	}
	if id := observe.UserIDFromCtx(ctx); id != "" {
		event.User = sentrygo.User{ID: id}
	}
	event.Request = rep.request(r)
	rep.client.CaptureEvent(event, nil, sentrygo.NewScope())
}

// request is the part of the HTTP request an event carries: method, URL
// without its query, the query and the headers — filtered by the SDK's
// denylist, then redacted by the framework's list. No cookies, no body, no
// client address.
func (rep *reporter) request(r *http.Request) *sentrygo.Request {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	dc := rep.client.GetDataCollection()
	headers := make(map[string]string, len(r.Header)+1)
	for k, v := range r.Header {
		if strings.EqualFold(k, "Cookie") {
			continue
		}
		headers[k] = strings.Join(v, ",")
	}
	headers["Host"] = r.Host
	headers = dc.FilterRequestHeaders(headers)
	return &sentrygo.Request{
		URL:         scheme + "://" + r.Host + r.URL.Path,
		Method:      r.Method,
		QueryString: rep.redactQuery(dc.FilterQueryString(r.URL.RawQuery)),
		Headers:     rep.redactMap(headers),
	}
}

// writer is the ResponseWriter the interceptor hands down: it remembers the
// status written, and it is where the router reports a handler's error.
type writer struct {
	http.ResponseWriter
	rep *reporter

	mu     sync.Mutex
	status int
	wrote  bool
}

// ReportError implements interceptor.ErrorReporter: the router calls it with
// the error behind a 500 it is about to answer.
func (w *writer) ReportError(r *http.Request, err error) {
	w.rep.capture(r, w.statusOr(http.StatusInternalServerError), err, sentrygo.LevelError, "handler_error")
}

// statusOr is the status already written, or fallback when none was: what
// the client receives once the framework answers.
func (w *writer) statusOr(fallback int) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.wrote {
		return w.status
	}
	return fallback
}

func (w *writer) WriteHeader(code int) {
	w.mu.Lock()
	if !w.wrote {
		w.status, w.wrote = code, true
	}
	w.mu.Unlock()
	w.ResponseWriter.WriteHeader(code)
}

func (w *writer) Write(b []byte) (int, error) {
	w.mu.Lock()
	if !w.wrote {
		w.status, w.wrote = http.StatusOK, true
	}
	w.mu.Unlock()
	return w.ResponseWriter.Write(b)
}

// WroteHeader is the accessor the framework's recoverer reads, through
// Unwrap, before it answers a panic.
func (w *writer) WroteHeader() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.wrote
}

// Unwrap lets http.ResponseController and the framework reach the writer
// underneath.
func (w *writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Flush keeps a streaming handler streaming through the interceptor.
func (w *writer) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack keeps a WebSocket upgrade working through the interceptor: the
// framework's realtime handler asserts http.Hijacker on the writer it gets.
func (w *writer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}
