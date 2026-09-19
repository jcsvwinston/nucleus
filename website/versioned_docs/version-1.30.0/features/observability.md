---
sidebar_position: 3
title: Observability
covers:
  - pkg/observe.NewLogger
  - pkg/observe.NewLoggerWithRedaction
  - pkg/observe.SetupOpenTelemetry
  - pkg/observe.TelemetryConfig
  - pkg/observe.RedactionConfig
  - pkg/observe.DefaultRedactedKeys
  - pkg/observe.WithContext
  - pkg/app.LivenessResponse
  - pkg/app.ReadinessResponse
  - pkg/app.BeginDraining
  - pkg/app.Draining
  - pkg/app.HealthzResponse
  - pkg/app.HealthzCheck
  - pkg/app.App.RegisterHealthProbe
  - pkg/app.Config.ProfilingEnabled
  - pkg/app.Config.TimeoutExemptPaths
  - pkg/app.WithOpenAuthz
  - pkg/circuit.New
  - pkg/circuit.Breaker
  - pkg/circuit.Breaker.Do
  - pkg/circuit.Breaker.State
  - pkg/circuit.Breaker.Opens
  - pkg/circuit.Config
  - pkg/circuit.ErrOpen
  - pkg/health.Run
  - pkg/health.NewDBProbe
  - pkg/health.NewRedisProbe
  - pkg/health.NewStorageProbe
  - pkg/health.NewMailProbe
  - pkg/health.SupportsMailProbe
  - pkg/mail.HealthChecker
config_keys:
  - log_level
  - log_format
  - log_redact_extra_keys[]
  - otlp_endpoint
  - metrics_path
  - metrics_public
  - sql_driver_instrumentation
  - profiling_enabled
  - request_timeout
  - timeout_exempt_paths[]
  - mail_circuit_breaker.enabled
  - storage.circuit_breaker.enabled
---

# Observability

`pkg/observe` is Nucleus's logging and tracing layer. `app.New(cfg)` wires it
by default, so there is nothing to set up before you get structured logs.

It is built on two choices:

- **`log/slog`** for structured logging.
- **OpenTelemetry** for distributed traces and metrics.

This page covers logging, tracing, the probe and metrics endpoints the runtime
mounts, the profiler, the live SQL feed, and the circuit breaker for external
dependencies.

## Logging

There is no pre-bound logger on the request. What the request carries is a set
of correlation IDs on its `context.Context` — `request_id` on every request,
`user_id` once a bearer token or an API key has been decoded, `tenant_id` when
multi-tenancy resolves one, and `trace_id` when OpenTelemetry is on — and
`observe.WithContext` is how you get them onto a logger:

```go
import (
    "net/http"

    "github.com/jcsvwinston/nucleus/pkg/observe"
    "github.com/jcsvwinston/nucleus/pkg/router"
)

application.Router.Post("/articles", func(c *router.Context) error {
    log := observe.WithContext(c.Request.Context(), application.Logger)
    log.Info("article.created",
        "article_id", id,
        "author_id",  authorID,
    )
    return c.JSON(http.StatusCreated, article)
})
```

`WithContext` adds only the IDs that are present, so a call outside a request
returns the logger unchanged. Use `application.Logger` (or any `*slog.Logger`
you built with `observe.NewLogger`) as the base: the framework does not call
`slog.SetDefault`, so a bare `slog.Info` / `slog.InfoContext` goes to the
stdlib default logger and carries neither the correlation IDs nor the
redaction described below.

Separately, the router's `RequestLogger` middleware writes one `http_request`
line per request with `method`, `path`, `status`, `duration_ms`, `request_id`,
`remote_addr`, `user_agent` and `bytes_written`. That is the access log; it is
not the logger your handler gets.

The format is configurable:

```yaml
log_level: info      # debug | info | warn | error
log_format: json     # text | json
```

`json` is the default. Override per-environment.

### Redaction, and where it stops

Every logger `app.New` builds goes through `observe.NewLoggerWithRedaction`:
attribute values whose key is in `observe.DefaultRedactedKeys` (authorization,
cookie, password, token, …) are replaced before they reach the output.
`log_redact_extra_keys` adds your own keys to that set; there is no
configuration key that turns redaction off.

Know exactly what it covers, because the gaps are the ones that leak:

- Matching is on the **attribute key**, case-insensitive and **exact**. A
  secret interpolated into the message — `fmt.Sprintf("token=%s", t)` — is
  written verbatim, because the message string is never scanned.
- A key that merely contains a listed word is not matched: `token` and
  `api_key` are redacted, `user_token` and `stripe_api_key` are not. The exact
  match is deliberate — a suffix rule would also blank `page_token` and
  `cache_key` — so put your own compound names in `log_redact_extra_keys`.
- A struct logged under one `slog.Any` key is not walked. Only `slog.Group`
  attributes are expanded and matched.

So pass secrets as their own named attributes, or do not log them. Redaction
is a second line, not the first one.

## Exporters are separate modules

The framework carries the OpenTelemetry machinery — spans, meters,
propagation — and none of the exporters. Add the one you use with a `go get`
and a blank import, or let the CLI do both:

```bash
nucleus add otlp        # push to a collector
nucleus add prometheus  # be scraped
```

```go
import _ "github.com/jcsvwinston/nucleus/exporters/otlp"
import _ "github.com/jcsvwinston/nucleus/exporters/prometheus"
```

**Your configuration does not change.** `otlp_endpoint` and `metrics_path`
mean exactly what they meant before; the import is the only new part. If you
set an endpoint without the module, startup stops and the error prints the
lines above rather than running quietly and exporting nothing.

They live outside the framework because an exporter nobody uses is not free.
The OTLP exporter pulls gRPC even in its HTTP flavour, and the Prometheus one
pulls protobuf through `client_golang`: 137 packages that every application
carried, scraped or not.

:::note If you already scrape `/metrics`

`metrics_path` has a default, so metrics were on for everyone. Which branch an
application takes is decided by comparing the value, not by who wrote it:
`metrics_path` still equal to the default `/metrics` is the tolerant case —
startup logs an INFO line naming the module to add and the application boots
with no `/metrics` route at all, so the path answers 404. (An application that
writes `metrics_path: /metrics` by hand is indistinguishable from the default
and takes the same branch.) Any other **non-empty** value stops startup with
the same recipe, because a path somebody chose and that answers 404 is worse
than a failure you can see. The empty string goes the other way: it switches
metrics off, so nothing is built and no module is asked for. Wherever
`metrics_path` is non-empty, adding `exporters/prometheus` brings the endpoint
back unchanged.

Note the level: it is INFO, not a warning. Grepping the boot log for `WARN`
will not find it.

:::

## Tracing

OpenTelemetry export is opt-in: set `otlp_endpoint` to an OTLP-HTTP
collector and the MeterProvider/TracerProvider start pushing to it.

```yaml
otlp_endpoint: http://otel-collector:4318
```

Two subsystems open spans, and only two. `pkg/router` wraps every HTTP request
in a server span named after the matched route, and the Redis (Asynq) jobs
provider opens one on each side of the queue: a producer span,
`task.enqueue <type>`, inside whatever ran the enqueue, and a consumer span,
`task.process <type>`, in the worker. The worker continues the trace from the
`traceparent` the enqueue span wrote into the payload, so a task's parent is
the enqueue, not the request that happened to be running.

**SQL statements are measured, not traced.** `pkg/db` creates metric
instruments only (see [Metrics](#metrics)); it opens no span, so a query does
not appear as a child of the request that ran it. Neither do the other
subsystems — the whole tree holds exactly two tracers.

## Framework-mounted runtime endpoints

The runtime mounts these endpoints automatically. None of them needs
application code to register it, and all four are seeded into the anonymous
bootstrap allow-list, because the orchestrator and the scraper that call them
have no session and cannot be given one. `/metrics` leaves that allow-list
only under `metrics_public: false`.

| Endpoint | Answers | Mounted |
| --- | --- | --- |
| `GET /livez` | is this **process** alive? | always |
| `GET /readyz` | should this instance **receive traffic**? | always |
| `GET /healthz` | how is **each dependency** doing? | always |
| `GET /metrics` | the Prometheus scrape, see [Metrics](#metrics) | `metrics_path` non-empty **and** `exporters/prometheus` imported |

The `/metrics` row has two conditions, not one: `app.New` mounts the route
only when the telemetry setup handed it a handler, and it does that only when
the Prometheus exporter module is linked into the binary. With the shipped
default (`metrics_path: /metrics`, no exporter imported) the route is not
registered and the path answers 404 — see
[Exporters are separate modules](#exporters-are-separate-modules).

### `/livez` — the process, and nothing else

`/livez` answers `200` with `pkg/app.LivenessResponse`, always:

```json
{"status": "alive", "uptime_seconds": 3421}
```

`uptime_seconds` counts from when `app.New` built the application, not from
the first request. It is the one fact a liveness answer can honestly report
about itself, which is why it is the only field besides `status`.

**`/livez` checks no dependency on purpose** — not the database, not Redis,
not object storage, not mail. A liveness probe is what an orchestrator
restarts a container over, and a process whose database is unreachable is not
broken: it is a working process waiting on something else. Restarting it does
not reach the database, and because every replica shares that database, a
liveness probe wired to it restarts *every replica at once* — a dependency's
bad minute becomes an outage of your own making. The question "can I serve
right now?" has its own endpoint, below.

### `/readyz` — whether to send traffic here

`/readyz` runs the same dependency probes as `/healthz` and answers with
`pkg/app.ReadinessResponse`. `200` with `"status": "ready"` when every probe
is healthy, `503` with `"status": "not ready"` as soon as one is not:

```json
{
  "status": "ready",
  "checked_at": "2026-09-19T09:41:12Z",
  "checks": [
    {"name": "db:default", "status": "healthy", "latency_ms": 1},
    {"name": "redis",      "status": "healthy", "latency_ms": 3},
    {"name": "storage",    "status": "healthy", "latency_ms": 12}
  ]
}
```

This is the endpoint a load balancer polls. An instance that cannot reach its
database drops out of the pool and stops receiving requests it would only fail,
while the process keeps running and `/livez` keeps answering `200` — so nothing
restarts it, and it rejoins the pool by itself the moment the probe passes
again.

`reason` is `omitempty`: it appears only while the instance is draining — see
below.

### Draining, and what a rolling deploy needs

`app.BeginDraining()` marks the application as shutting down. From that call
on, `/readyz` answers `503` without running any probe:

```json
{
  "status": "draining",
  "checked_at": "2026-09-19T09:41:12Z",
  "reason": "the process is shutting down",
  "checks": []
}
```

`app.Draining()` reports the current state. `/livez` is unaffected and keeps
answering `200`, which is the point: the load balancer takes the instance out
of rotation while the orchestrator leaves the process alone to finish the
requests it already accepted.

In a rolling deploy that sequence is the whole difference between a clean
replacement and dropped requests. A load balancer notices a readiness failure
one health-check interval after it happens; a process that starts refusing
connections the instant it is signalled is still in the pool during that
interval. Draining buys exactly that window.

**The framework never calls `BeginDraining` for you.** `App.Run` installs its
own `SIGTERM` handler and goes straight to `App.Shutdown`, so the drain has to
start *before* the signal — which is what a Kubernetes `preStop` hook is for.
Give it something to call:

```go
import (
    "net/http"

    "github.com/jcsvwinston/nucleus/pkg/app"
    "github.com/jcsvwinston/nucleus/pkg/router"
)

// Called by the deployment — a preStop hook, a deploy script — before the
// process is signalled, then followed by a sleep long enough for the load
// balancer to poll /readyz at least once.
application.Router.Post("/internal/drain", func(c *router.Context) error {
    app.BeginDraining()
    return c.JSON(http.StatusAccepted, map[string]string{"status": "draining"})
})
```

That route is yours, not the framework's: it sits outside the bootstrap
allow-list and falls under your own policy like any other route, so grant it to
whatever the hook authenticates as, or keep it on an interface only the
orchestrator reaches.

Two limits to know. The flag is **process-wide** — it is not per-`App`, so in a
process running more than one application it drains all of them. And it is
**one-way**: nothing exported clears it, so an instance that has begun draining
answers `503` at `/readyz` for the rest of its life. Both are the right shape
for shutdown and the wrong shape for a "pause this instance" switch; do not
reach for it as one.

### `/healthz`

`/healthz` is the per-dependency report, for uptime monitors and for a human
reading the response. The body is `pkg/app.HealthzResponse`, a list of
`pkg/app.HealthzCheck`:

```json
{
  "status": "healthy",
  "checked_at": "2026-09-19T09:41:12Z",
  "checks": [
    {"name": "db:default", "status": "healthy", "latency_ms": 1},
    {"name": "redis",      "status": "healthy", "latency_ms": 3},
    {"name": "storage",    "status": "healthy", "latency_ms": 12}
  ]
}
```

`status` is `healthy` or `unhealthy`. The HTTP status is `200` when every
probed dependency is healthy and `503` otherwise — external probes only need
to consume the status code.

A failing probe puts the error text in `message`, which is what makes this
endpoint worth reading by hand:

```json
{
  "status": "unhealthy",
  "checked_at": "2026-09-19T09:41:12Z",
  "checks": [
    {
      "name": "db:default",
      "status": "unhealthy",
      "message": "dial tcp 10.0.0.5:5432: connect: connection refused",
      "latency_ms": 2001
    },
    {"name": "redis", "status": "healthy", "latency_ms": 3}
  ]
}
```

Both `message` and `latency_ms` are `omitempty`: a healthy probe carries no
`message`, and a probe that returned in under a millisecond carries no
`latency_ms` key at all.

The set of probes is derived from current app state on every request:

| Probe          | Registered when                                       | Underlying call                                          |
| -------------- | ----------------------------------------------------- | -------------------------------------------------------- |
| `db:<alias>`   | one per entry in `databases:`                         | `db.DB.Health` → `sql.DB.PingContext`                    |
| `redis`        | `redis_url` is non-empty                              | `redis.Client.Ping` against a short-lived client          |
| `storage`      | a `storage.Store` is attached (default subsystems)    | `storage.Store.List` with `_nucleus_healthz/` prefix, limit 1 |
| `mail`         | the configured `mail.Sender` implements `HealthChecker` | `mail.HealthChecker.Healthy` (TCP dial + HELO + QUIT for SMTP) |

Each probe runs concurrently with a 2-second per-probe budget; total
wall time is bounded by the slowest probe. `App.RegisterHealthProbe` adds one
of your own under a name you choose — call it before the server starts
serving, since registration is not synchronized with in-flight requests. What
you register there shows up in `/readyz` too, because both endpoints run the
same set.

The mail probe is opt-in by provider: a `Sender` is probed only if it
implements the optional `mail.HealthChecker` interface. SMTP implements it
natively — no auth and no message sent, just a dial, HELO and QUIT.

The `noop` provider and external plugin senders do not implement it, so
deployments on those drivers see no `mail` row in the `/healthz` response.
Probing external plugins needs a new call on the plugin protocol; until that
lands, each plugin owns its own health surface.

## Metrics

The runtime records these instruments through OpenTelemetry. They are created
against the global MeterProvider the first time the subsystem does the work
they measure, so a deployment sees only the families it actually exercises.

**HTTP** (`pkg/router`):

| Instrument | Kind | Attributes |
| --- | --- | --- |
| `http.server.requests` | counter | `http.method`, `http.route`, `http.status_code` |
| `http.server.request.duration.ms` | histogram | `http.method`, `http.route`, `http.status_code` |
| `http.server.in_flight` | up-down counter | `http.method` |

In-flight carries the method alone on purpose: the route is not known until
the mux has matched, and an attribute that differs between the `+1` and the
`-1` never balances back to zero. `http.route` is the matched pattern, not the
URL path, so ids do not become label values.

**SQL** (`pkg/db`): `db.client.query.total`, `db.client.query.errors` and
`db.client.query.duration.ms` for statements, plus the pool, observed on
collection: `db.client.pool.connections.open`,
`db.client.pool.connections.idle`, `db.client.pool.connections.in_use`,
`db.client.pool.wait.count` and `db.client.pool.wait.duration.ms`.

**Background jobs** (`pkg/tasks`). On the memory and SQL providers these are
attributed by `provider`, `queue` and `type`:

| Instrument | Records |
| --- | --- |
| `jobs.enqueue.total` | a job accepted into the queue |
| `jobs.process.started` | a job picked up by a worker |
| `jobs.process.succeeded` | a handler that returned no error |
| `jobs.process.retried` | a failure that will be attempted again |
| `jobs.process.failed` | a job that has run out of attempts |
| `jobs.process.held` | a job kept because no handler for its type is registered here (`reason: no_handler`) |
| `jobs.process.duration.ms` | histogram, recorded on success and on retry |

`jobs.process.held` is the series that tells you a producer was deployed ahead
of its worker. Read its name narrowly: `no_handler` is the only value the
`reason` attribute ever takes. The memory provider also holds jobs whose
retries ran out, jobs stopped by shutdown and jobs still queued when the
manager closed, and none of those increment this counter — they are visible
only in the provider's own hold stores, not in the metric.

### The Redis provider's instruments are not the same set

The `jobs.*` names are the tasks subsystem's rather than one backend's, which
is why the memory and SQL providers publish them at all. But the Redis
(Asynq) provider builds its own instruments, and a dashboard does not carry
over unchanged. Three differences:

- **`jobs.process.held` does not exist on Redis.** Only the memory and SQL
  providers record it, so the "producer ahead of its worker" alert above
  produces no series on a Redis-backed deployment.
- **`jobs.enqueue.errors` exists only on Redis.** The other two providers do
  not record it.
- **The attribute names differ.** Redis uses `task.type` and `task.queue`
  (plus `job.outcome` on the histogram) where the others use `provider`,
  `queue` and `type`. A query that groups by `provider` returns nothing there.

`jobs.process.duration.ms` also differs in when it fires: the memory and SQL
providers record it on success and on retry, the Redis provider on every
outcome including terminal failure, tagged with `job.outcome`.

Where the numbers go:

| Endpoint     | When mounted                                  | Format                                    |
| ------------ | --------------------------------------------- | ----------------------------------------- |
| `GET /metrics` | `metrics_path` is non-empty (default `/metrics`) **and** `exporters/prometheus` is imported | OpenMetrics / Prometheus exposition       |
| OTLP push    | `otlp_endpoint` is set (needs `exporters/otlp`) | OTLP-HTTP                                 |

The two paths coexist: the MeterProvider attaches both readers when
both are configured, so a deployment can scrape locally **and** push
to an OTel collector without double-instrumenting code.

To keep the endpoint off even after adding `exporters/prometheus`, set
`metrics_path: ""` in `nucleus.yml`: with an empty path the exporter is never
built and no route is registered.

:::warning[No built-in auth on /metrics once it is mounted]
Once the endpoint is mounted — `metrics_path` non-empty **and**
`exporters/prometheus` imported — it answers without authentication of its
own. With `metrics_public: true` (the default, the historical posture) the
bootstrap RBAC allow-list grants `/metrics` to the anonymous subject, so the
scraper needs no credentials. If you keep that default, restrict access at the
network or reverse-proxy layer (allow-list your Prometheus scraper). To gate
it in-process instead, set `metrics_public: false` — `/metrics` then falls
under the default-deny RBAC enforcer like any user route, and your scraper
needs an explicit policy grant (e.g. `p, metrics-scraper, /metrics, *, allow`
plus JWT auth). Policy rows carry four columns; one without the `eft` column
matches neither `allow` nor `deny` and grants nothing.

The grant is for the **literal path `/metrics`**, not for whatever
`metrics_path` holds. Move the endpoint — `metrics_path: /internal/metrics` —
and the route exists but is not in the allow-list, so it falls under
default-deny and your scraper gets `403` even with `metrics_public: true`.
Either keep the default path or grant the new one yourself
(`p, anonymous, /internal/metrics, *, allow`).
:::

## The profiler

`net/http/pprof` is **not mounted by default**, and an application that has
not asked for it answers `404` under `/debug/pprof`. Turn it on for one
deployment with:

```yaml
# nucleus.yml
profiling_enabled: true
```

That mounts the index and `cmdline`, `profile`, `symbol` and `trace` under
`/debug/pprof`, plus the named profiles the Go runtime provides on the same
prefix — `heap`, `goroutine`, `allocs`, `block`, `mutex`, `threadcreate`. All
of them answer `GET`. Startup logs a warning naming the prefix, so the boot log
says the profiler is on rather than leaving you to discover it.

```bash
go tool pprof http://127.0.0.1:8080/debug/pprof/heap
```

**The profiler sits behind the router's request timeout like any other
route**, and nothing exempts `/debug/pprof`. `request_timeout` defaults to 30
seconds — the same length as a default CPU profile, so
`/debug/pprof/profile` and any `trace?seconds=` at or above the deadline
return `503` with a `{"error":{"code":"TIMEOUT"}}` body instead of a profile.
The timeout handler also buffers the whole response in memory. Exempt the
prefix when you turn the profiler on:

```yaml
# nucleus.yml
profiling_enabled: true
timeout_exempt_paths:
  - /debug/pprof
```

Only the two duration-based endpoints are affected. The snapshot profiles —
`heap`, `goroutine`, `allocs`, `block`, `mutex`, `threadcreate` — return at
once and fit inside the deadline either way.

The reason it is a flag rather than an import is that `net/http/pprof`
registers itself on `http.DefaultServeMux` at import time. A framework that
imported it unguarded would put heap and goroutine dumps on every
application's public surface. A dump is not a summary: it carries live process
memory — request payloads, tokens, whatever was resident when you asked.

**These routes are never in the bootstrap allow-list.** `/healthz`, `/livez`,
`/readyz` and (by default) `/metrics` are granted to the anonymous subject so
an orchestrator can reach them; `/debug/pprof` is not, and there is no
configuration key that adds it — removing that exclusion would take a code
change. Concretely, on an application exposed to the internet with
`profiling_enabled: true`, an unauthenticated request to `/debug/pprof/` gets
`403`, and it stays `403` until one of your own policies names a subject that
may read it. Grant it to your on-call role, not to `anonymous`.

That `403` comes from the default-deny middleware, so it holds only under the
default authorization posture. An application built with `app.WithOpenAuthz()`
mounts no enforcement at all: with the profiler on, `/debug/pprof` is then
readable by anyone who can reach the port, heap dump included. The two
switches have to be considered together.

## Seeing every SQL statement, not just the ORM's

The live SQL feed is fed by the CRUD layer, so by default it shows only the
statements that went through models. Anything talking to the database
directly never appears: `db.QueryContext` / `db.ExecContext`, raw SQL,
migrations, the transactional outbox dispatcher, SQL-backed session stores.

On a busy app that is exactly the traffic you most want to see when something
is slow. Set `sql_driver_instrumentation` to wrap the `database/sql` driver
itself, so those statements land on the same feed:

```yaml
# nucleus.yml
sql_driver_instrumentation: true
```

What changes when you turn it on:

- **Direct statements appear on the feed**, with their operation
  (`select`, `insert`, …), the SQL text, the duration, and any error.
- **Writes report `rows_affected`** — the row count the driver itself
  reports. Queries report `0`, as do drivers that do not supply a count.
- **The model column is empty** for these statements. The driver only sees
  SQL text, so it cannot know which model produced it — that is the honest
  answer, not a gap. ORM traffic keeps its model name because CRUD keeps
  emitting it.
- **CRUD statements are not recorded twice.** CRUD marks the context it
  hands to `database/sql`, and the driver wrapper skips anything carrying
  that mark. What is left is, by definition, the bypass traffic.

### Argument values

Statement arguments are **never published verbatim**. Before an event is
emitted, each argument is replaced by a redacted, type-tagged form:

| Argument type          | What the feed shows      |
| ---------------------- | ------------------------ |
| `string`               | `string(12):***` — type and length only, never the content |
| `[]byte`               | `bytes(12):***` — same, under its own tag |
| numbers                | the value                |
| `bool`                 | `bool:true` / `bool:false` |
| `nil`                  | `null`                   |
| `time.Time`            | `time:2026-09-19T09:41:12Z` — converted to UTC |
| any other type         | `<redacted>`             |

Only the first 16 arguments are recorded; the rest collapse into a
`...(+N more)` marker. So the feed will tell you a query bound a 24-byte
string, never *which* string. The statement text is the gap: it is published
as written (whitespace collapsed, truncated at 640 bytes), so a secret written
into the SQL as a literal instead of bound as an argument appears verbatim —
the same rule as log messages above. Booleans, numbers and timestamps also
appear in full, which is what makes the feed readable; if one of those is
itself sensitive, the feed is not the place to look at it.

### What the flag changes on the query path

Off (the default), the driver is not wrapped: the query path is the stock
`database/sql` one, with nothing added.

On, each *direct* statement runs a fixed piece of extra work — two clock
reads, an operation classification, and a copy of the argument slice. The
redaction and the event build are gated separately: they run only while a
subscriber is attached to the feed.

## Circuit breakers for external dependencies

`pkg/circuit` is a breaker primitive following the standard
`closed → open → half-open` state machine.

**Mail and object storage are already wrapped.** `app.New` puts a breaker
around `mail.Sender.Send` and around the remote `storage.Store` operations
(`Put`, `Get`, `Delete`, `Exists`, `List`, `Copy`, `SignedURL`); the `noop`
mailer and the local storage provider are never wrapped. Both are on by
default and tuned through `mail_circuit_breaker.*` and
`storage.circuit_breaker.*`, or switched off with their `enabled: false`. Do
not hand-wrap those calls — you would stack a second breaker on a breakered
one.

`circuit.New` is for the calls the framework does not make for you: plugin
bridges, third-party APIs, anything whose unavailability should not cascade.

```go
import (
    "context"
    "errors"
    "time"

    "github.com/jcsvwinston/nucleus/pkg/circuit"
)

cb := circuit.New(circuit.Config{
    FailureThreshold:      5,
    Cooldown:              30 * time.Second,
    HalfOpenMaxConcurrent: 1,
})

err := cb.Do(ctx, func(ctx context.Context) error {
    return payments.Charge(ctx, order)
})
if errors.Is(err, circuit.ErrOpen) {
    // dependency is in cooldown — fall back to a queue, return
    // 503, log and move on, etc.
}
```

The package is deliberately small: no event bus, no metrics surface, no
per-call timeout. What it does expose is `Config.OnStateChange`, a callback
per transition, and `Breaker.Opens()`, a count of how many times the breaker
has tripped. Feed those into `pkg/observe` for logging and into your own OTel
instruments for counters.

## What you do not have to do

- No correlation plumbing to thread through every call — the IDs travel on
  the request `context.Context`, and `observe.WithContext` puts them on
  whatever logger you hand it.
- No bespoke tracing API — you use `go.opentelemetry.io/otel/trace`
  directly.
- No opinionated metrics SDK — you emit via OTel and pick your backend
  at deploy time.
- No health endpoint to write. `/livez`, `/readyz` and `/healthz` are mounted
  before your first route, and `App.RegisterHealthProbe` is how your own
  dependencies join them.
