---
sidebar_position: 3
title: Observability
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
observability:
  log_level:  info     # debug | info | warn | error
  log_format: json     # text | json
```

`text` is the default in `development`; `json` is the default in
`production`. Override per-environment.

## Tracing

OpenTelemetry is opt-in:

```yaml
observability:
  otel_enabled: true
  otel_endpoint: http://otel-collector:4318
  otel_service_name: myapp
```

When enabled, every HTTP request is wrapped in a span, every SQL query
emits a child span, and the admin panel surfaces a "live traffic" view
that streams the same data without leaving the binary.

## Health endpoints

The runtime mounts a deterministic health endpoint:

| Endpoint           | What it reports                                          |
| ------------------ | -------------------------------------------------------- |
| `GET /healthz`     | Liveness + per-dependency probes (DB, Redis, storage).   |

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
The `noop` provider, the `sendgrid` provider, and external plugin
senders do not implement `HealthChecker` today; deployments using
those drivers will not see a `mail` row in the `/healthz` response.
SendGrid will gain a probe once the HTTP user-profile endpoint is
wired through the provider; external-plugin probes need a new RPC on
the plugin protocol and are deferred.

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
