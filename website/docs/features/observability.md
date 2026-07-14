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
  - pkg/circuit.New
  - pkg/circuit.Breaker
  - pkg/circuit.Breaker.Do
  - pkg/circuit.Breaker.State
  - pkg/circuit.Config
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
  - otlp_endpoint
  - metrics_path
  - sql_driver_instrumentation
---

# Observability

`pkg/observe` is Nucleus's logging and tracing layer. It is wired by
default in `app.New(cfg)` and is designed around two stdlib choices:

- **`log/slog`** for structured logging.
- **OpenTelemetry** for distributed traces and metrics.

## Logging

Every request gets a logger pre-bound with:

- `request_id`
- `method`, `path`, `status`, `latency`
- the resolved tenant / site (when multi-tenant is enabled)
- the trace and span IDs (when OTel is on)

```go
slog.InfoContext(ctx, "article.created",
    "article_id", id,
    "author_id",  authorID,
)
```

The format is configurable:

```yaml
log_level: info      # debug | info | warn | error
log_format: json     # text | json
```

`json` is the default. Override per-environment.

## Tracing

OpenTelemetry export is opt-in: set `otlp_endpoint` to an OTLP-HTTP
collector and the MeterProvider/TracerProvider start pushing to it.

```yaml
otlp_endpoint: http://otel-collector:4318
```

When set, every HTTP request is wrapped in a span and every SQL query
emits a child span.

## Framework-mounted runtime endpoints

The runtime mounts several internal endpoints automatically. None require
application code to register.

| Endpoint           | Auth required          | What it reports / does                                              |
| ------------------ | ---------------------- | ------------------------------------------------------------------- |
| `GET /healthz`     | None (public)          | Liveness + per-dependency probes (DB, Redis, storage).              |

### `/healthz`

The response is a deterministic JSON shape suitable for Kubernetes
probes and external uptime monitors:

```json
{
  "status": "healthy",
  "checked_at": "2026-05-13T00:00:00Z",
  "checks": [
    {"name": "db:default", "status": "healthy", "latency_ms": 1},
    {"name": "redis",      "status": "healthy", "latency_ms": 3},
    {"name": "storage",    "status": "healthy", "latency_ms": 12}
  ]
}
```

`status` is `healthy` or `unhealthy`. The HTTP status is `200` when
every probed dependency is healthy and `503` otherwise — external
probes only need to consume the status code.

The set of probes is derived from current app state on every request:

| Probe          | Registered when                                       | Underlying call                                          |
| -------------- | ----------------------------------------------------- | -------------------------------------------------------- |
| `db:<alias>`   | one per entry in `databases:`                         | `db.DB.Health` → `sql.DB.PingContext`                    |
| `redis`        | `redis_url` is non-empty                              | `redis.Client.Ping` against a short-lived client          |
| `storage`      | a `storage.Store` is attached (default subsystems)    | `storage.Store.List` with `_nucleus_healthz/` prefix, limit 1 |
| `mail`         | the configured `mail.Sender` implements `HealthChecker` | `mail.HealthChecker.Healthy` (TCP dial + HELO + QUIT for SMTP) |

Each probe runs concurrently with a 2-second per-probe budget; total
wall time is bounded by the slowest probe.

The mail probe is opt-in by provider: a `Sender` must implement the
optional `mail.HealthChecker` interface to be probed. SMTP implements
it natively (no auth, no message sent — just a dial + HELO + QUIT).
The `noop` provider and external plugin senders do not implement
`HealthChecker` today; deployments using those drivers will not see
a `mail` row in the `/healthz` response. External-plugin probes need
a new RPC on the plugin protocol and are deferred — each plugin owns
its own health surface until that RPC lands.

## Metrics

The runtime exports the following metrics through OpenTelemetry:

- HTTP request count and latency histograms,
- SQL pool stats (in use, idle, wait time),
- session store hit / miss / eviction counters,
- background-task queue depth and latency (when `pkg/tasks` is wired).

| Endpoint     | When mounted                                  | Format                                    |
| ------------ | --------------------------------------------- | ----------------------------------------- |
| `GET /metrics` | `metrics_path` is non-empty (default `/metrics`) | OpenMetrics / Prometheus exposition       |
| OTLP push    | `otlp_endpoint` is set                        | OTLP-HTTP                                 |

The two paths coexist: the MeterProvider attaches both readers when
both are configured, so a deployment can scrape locally **and** push
to an OTel collector without double-instrumenting code.

To disable the `/metrics` endpoint, set `metrics_path: ""` in
`nucleus.yml`.

:::warning[No built-in auth on /metrics by default]
By default the scrape endpoint answers without authentication
(`metrics_public: true`, the historical behaviour): the bootstrap RBAC
allow-list grants anonymous access so Prometheus can scrape out of the box.
If you keep that default, restrict access at the network or reverse-proxy
layer (allow-list your Prometheus scraper). To gate it in-process instead,
set `metrics_public: false` — `/metrics` then falls under the default-deny
RBAC enforcer like any user route, and your scraper needs an explicit
policy grant (e.g. `p, metrics-scraper, /metrics, *` plus JWT auth).
:::

## Seeing every SQL statement, not just the ORM's

The live SQL feed is fed by the CRUD layer, so by default it shows the
statements that went through models. Anything that talks to the database
directly — `db.QueryContext` / `db.ExecContext`, raw SQL, migrations, the
transactional outbox dispatcher, SQL-backed session stores — bypasses CRUD
and therefore never appears. On a busy app that is exactly the traffic you
most want to see when something is slow.

Set `sql_driver_instrumentation` to wrap the `database/sql` driver itself,
so those statements land on the same feed:

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
| `string`, `[]byte`     | `string(12):***` — type and length only, never the content |
| numbers                | the value                |
| `time.Time`            | RFC 3339 timestamp       |
| anything else          | `<redacted>`             |

Only the first 16 arguments are recorded; the rest collapse into a
`...(+N more)` marker. So the feed will tell you a query bound a 24-byte
string, never *which* string — passwords, tokens and personal data do not
leak into it.

### What it costs

Off (the default), the driver is not wrapped at all: the hot path is the
stock `database/sql` path, byte for byte, and this feature costs nothing.

On, each *direct* statement pays a small fixed cost (two clock reads, an
operation classification, a copy of the argument slice). The expensive part
— redaction and building the event — still only runs when someone is
actually watching the feed. Leaving it on in production is reasonable;
leaving it off costs you nothing.

## Circuit breakers for external dependencies

`pkg/circuit` provides a small standalone breaker primitive for
wrapping calls to mail, object storage, plugin bridges, and other
third-party APIs whose unavailability should not cascade into a full
outage. The breaker follows the standard `closed → open → half-open`
state machine.

```go
import "github.com/jcsvwinston/nucleus/pkg/circuit"

cb := circuit.New(circuit.Config{
    FailureThreshold:      5,
    Cooldown:              30 * time.Second,
    HalfOpenMaxConcurrent: 1,
})

err := cb.Do(ctx, func(ctx context.Context) error {
    return mailer.Send(ctx, msg)
})
if errors.Is(err, circuit.ErrOpen) {
    // dependency is in cooldown — fall back to a queue, return
    // 503, log and move on, etc.
}
```

The package is intentionally minimal: no event bus, no metrics
surface, no per-call timeout. Compose those with `pkg/observe` for
logging and the `/metrics` MeterProvider for counters.

## What you do not have to do

- No "logger" object to thread through every constructor — the request
  logger lives on the request `context.Context`.
- No bespoke tracing API — you use `go.opentelemetry.io/otel/trace`
  directly.
- No opinionated metrics SDK — you emit via OTel and pick your backend
  at deploy time.
