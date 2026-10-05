---
sidebar_position: 3
title: Error reporting (Sentry)
covers:
  - pkg/observe.DefaultRedactedKeys
  - pkg/observe.RedactionPlaceholder
---

# Error reporting (Sentry)

An application's log says when a request failed. An error tracker groups
the failures, counts them, and tells you when a new one appears. The
`sentry` catalog entry sends two kinds of failure to
[Sentry](https://sentry.io), each with the request it happened on:

- an error a handler returns that the framework answers with a **500** —
  one that is neither a `DomainError` nor an `HTTPError`, the kind the log
  records as `handler error`;
- a **panic** the framework recovers.

It ships as its own module, `github.com/jcsvwinston/nucleus/providers/errors-sentry`.
The Sentry SDK is linked by the applications that add it and by no other: the
framework does not depend on it.

## Install

```bash
nucleus add sentry
```

The command does three things: `go get` of the module at the version
released with the CLI, the blank import in `main.go`, and this block in
`nucleus.yml` (written only when neither `http_interceptors` nor
`interceptors` is set there; printed to merge by hand otherwise):

```yaml
http_interceptors: [sentry]
interceptors:
  sentry:
    dsn: ""
    environment: ""
    release: ""
    sample_rate: 1.0
```

Then set the DSN — Sentry shows it under Project Settings, Client Keys — in
the file or in the environment:

```bash
export SENTRY_DSN=https://<public key>@o0.ingest.sentry.io/<project id>
```

The module is a [request interceptor](../concepts/routing.md#interceptors-declared-in-configuration):
the import registers it under the name `sentry`, and `http_interceptors`
puts it in the request path. An interceptor that is imported and not listed
does nothing, which is why the command writes the list too.

## Configuration

| key (`interceptors.sentry.*`) | default | what it does |
|---|---|---|
| `dsn` | `SENTRY_DSN` | The project's DSN. With neither set, the interceptor is in place, sends nothing, and the boot log says so once. |
| `environment` | `SENTRY_ENVIRONMENT` | The deployment the events belong to: `production`, `staging`. |
| `release` | `SENTRY_RELEASE`, then the commit variables of common CI systems, then the VCS revision the binary was built from | The build the events belong to. |
| `sample_rate` | `1.0` | The fraction of events sent, between 0 and 1. `0` is read as unset; to send nothing, leave the DSN empty or take `sentry` out of `http_interceptors`. |
| `redact_extra_keys` | none | Header and query-parameter names redacted beyond the framework's list (below). Give it the names `log_redact_extra_keys` lists, so an event redacts what a log line does. |

The configuration is strict. A key the module does not declare
(`interceptors.sentry.dns`), a sample rate outside 0–1 and a DSN that does
not parse stop the boot, naming the key; the refusal never repeats the DSN,
because it carries the project's key. When the module is not in the build,
the same block is refused with `not installed: nucleus add sentry`.

At boot the module logs one line naming where events go — the host and the
project id, never the key — or, with no DSN, one warning that nothing is
sent.

## What an event carries

| field | value |
|---|---|
| exception | the handler's error (type and message, its chain unwrapped), or the panic value — type `panic` for a value that is not an error |
| stack | for a panic, where it happened; for an error, where the framework caught it, unless the error carries its own stack |
| level | `error` for a handler's error, `fatal` for a panic |
| transaction | the method and the route template: `GET /orders/{id}`, not `GET /orders/42` |
| tags | `http.method`, `http.route`, `http.status_code`, `request_id` (the `X-Request-Id` the response carried), `nucleus.source` (`handler_error` or `panic`) |
| user | the id the framework attributes the request to — the bearer's subject, an API key's owner, or what a session bridge set with `auth.ContextWithClaims` — and nothing else |
| request | method, URL without the query, the query string and the headers, redacted (below) |

What the application answered on purpose is not reported: a `DomainError`
or an `HTTPError` (a 404, a 422, a 503 the handler chose), a request
timeout, and a handler that stops with `http.ErrAbortHandler`.

A panic is reported and then raised again, so the framework still answers
the 500 and logs the stack exactly as it does without the module. Put
`sentry` first in `http_interceptors` to see the panics of the interceptors
after it.

## What is redacted, and what never leaves

Before an event is sent:

- every header and query parameter whose name is on the framework's
  redaction list — the list log attributes are redacted with
  (`observe.DefaultRedactedKeys()`: `authorization`, `cookie`, `x-api-key`,
  `token`, `access_token`, `password`, `dsn`, … ) plus `redact_extra_keys` —
  is replaced with `[REDACTED]`, matched on the whole name in any case, as
  in the log;
- Sentry's own default denylist applies on top, and replaces what it
  matches with `[Filtered]` (it matches parts of names: `X-Session-Id`,
  `X-Forwarded-For`);
- cookies, request bodies and the client's IP address are not sent at all.

The error message is sent as the error reads. A message that embeds a
secret — a connection string, say — leaves with it, exactly as it is
written to the log; redaction is by name, and a message has none.

## Limits worth knowing

- **Handler errors need a framework newer than v1.31.0.** The module reaches
  the error behind a 500 through `interceptor.ErrorReporter`, which v1.31.0
  does not have. On v1.31.0 it builds, reports panics, and does not see
  handler errors.
- **Events are sent in the background.** The SDK sends on its own
  goroutine; an event still in flight when the process exits is lost.
- **Errors and panics, not statuses.** A handler that writes a 500 itself,
  without returning an error, is not reported: there is no error to send.

## Writing a reporter of your own

The module uses nothing private. An interceptor that wants the error behind
a 500 wraps the `http.ResponseWriter` it hands down in one that implements
`interceptor.ErrorReporter`:

```go
type reportingWriter struct {
    http.ResponseWriter
}

// ReportError is called before the 500 is written, on the handler's
// goroutine, with the request as the handler saw it.
func (w *reportingWriter) ReportError(r *http.Request, err error) {
    log.Printf("%s %s: %v", r.Method, router.RouteFromContext(r.Context()), err)
}

func New(cfg interceptor.Config) (interceptor.Interceptor, error) {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            next.ServeHTTP(&reportingWriter{ResponseWriter: w}, r)
        })
    }, nil
}
```

The framework notices the method on the writer the interceptor passes on
and keeps it in the request's context, so a buffered writer further down (a
route with its own `Timeout`) does not hide it. Every reporter in the chain
is told once; one that panics changes neither the 500 nor the reporters
after it. It is a method on the writer, the way `http.Flusher` is, so a
module that implements it builds against a release that predates it.
