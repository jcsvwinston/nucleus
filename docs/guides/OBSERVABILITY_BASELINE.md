# Observability Baseline

Reference date: 2026-04-07.
Status: Current.

This document defines the recommended minimum dashboards and alerts for Nucleus services in production.

## Scope

- HTTP telemetry from `pkg/router` middleware
- DB telemetry from `pkg/db` runtime hooks
- Job telemetry from `pkg/tasks` enqueue + worker middleware

## Metric Signals (Current)

## Database

- `db.client.query.total`
- `db.client.query.errors`
- `db.client.query.duration.ms`
- `db.client.pool.connections.open`
- `db.client.pool.connections.idle`
- `db.client.pool.connections.in_use`
- `db.client.pool.wait.count`
- `db.client.pool.wait.duration.ms`

Key attributes:

- `db.system` (`sqlite`, `postgresql`, `mysql`, `unknown`)
- `db.engine` (`sql`)
- `db.operation` (`select`, `insert`, `update`, etc.)

## Jobs

- `jobs.enqueue.total`
- `jobs.enqueue.errors`
- `jobs.process.started`
- `jobs.process.succeeded`
- `jobs.process.retried`
- `jobs.process.failed`
- `jobs.process.duration.ms`

Key attributes:

- `task.type`
- `task.queue`
- `job.outcome` (`success`, `retry`, `failure`)

## Tracing and Correlation

- HTTP requests emit server spans in `pkg/router`.
- Job enqueue emits producer spans in `pkg/tasks` (`Manager.EnqueueJSONCtx`).
- Job processing emits consumer spans in `pkg/tasks` worker middleware.
- Context correlation across request -> job is available when enqueueing with:
  - `Manager.EnqueueJSONCtx(ctx, taskType, payload, ...)`
- Correlation metadata propagated:
  - `request_id`
  - `user_id`
  - `trace_id`
  - `traceparent`

## Dashboard Baseline

## 1) DB Reliability

Panels:

- Query error rate: `db.client.query.errors / db.client.query.total` by `db.system`, `db.operation`
- P95/P99 query duration from `db.client.query.duration.ms`
- Pool saturation: `in_use / open`
- Pool queue pressure: `db.client.pool.wait.count` and `db.client.pool.wait.duration.ms` trend

## 2) Job Throughput and Health

Panels:

- Enqueue throughput (`jobs.enqueue.total`) and enqueue error rate (`jobs.enqueue.errors`)
- Processing outcomes (`succeeded`, `retried`, `failed`) by queue and task type
- P95/P99 processing duration from `jobs.process.duration.ms`
- Retry ratio: `jobs.process.retried / jobs.process.started`

## 3) Request-to-Job Correlation

Panels:

- Trace view filtered by spans:
  - `task.enqueue *`
  - `task.process *`
- Log correlation by `request_id` and `trace_id` (when carried through `EnqueueJSONCtx`)

## Alert Baseline

## Database

- High DB error rate:
  - condition: `db.client.query.errors / db.client.query.total > 0.05` for 5m
- Query latency regression:
  - condition: P95 `db.client.query.duration.ms` > agreed SLO for 10m
- Pool pressure sustained:
  - condition: `db.client.pool.connections.in_use / db.client.pool.connections.open > 0.9` for 10m

## Jobs

- Enqueue failures:
  - condition: `jobs.enqueue.errors > 0` for 5m (or error ratio threshold)
- Failure spike:
  - condition: `jobs.process.failed / jobs.process.started > 0.1` for 10m
- Retry storm:
  - condition: `jobs.process.retried / jobs.process.started > 0.2` for 10m
- Slow consumers:
  - condition: P95 `jobs.process.duration.ms` above queue-specific SLO for 10m

## Operational Notes

- Tune thresholds per service SLO and queue criticality.
- Prefer queue-specific alerts for high-priority workloads.
- MSSQL / Oracle now run as required CI jobs (promoted 2026-05-12 — see `docs/reports/mssql_oracle_stability_report.md`); alerts can treat them like any other engine.

## HTTP Probes

The runtime exposes two unauthenticated HTTP endpoints intended for external monitors and Kubernetes-style probes. Both are mounted by `App.New` and require no application-level wiring.

### `GET /healthz`

Aggregated liveness check across every configured dependency. Returns HTTP `200` with a JSON body when every probe is healthy; `503` with the same shape when any probe fails. Per-probe budget is 2 seconds; probes run concurrently so total wall time is bounded by the slowest probe.

The probe set is derived from current app state on every request:

| Probe          | Registered when                                       | Underlying call                                            |
| -------------- | ----------------------------------------------------- | ---------------------------------------------------------- |
| `db:<alias>`   | one per entry in `databases:`                         | `db.DB.Health` → `sql.DB.PingContext`                      |
| `redis`        | `redis_url` is non-empty                              | `redis.Client.Ping` against a short-lived client            |
| `storage`      | a `storage.Store` is attached                         | `storage.Store.List` with `_nucleus_healthz/` prefix, limit 1 |
| `mail`         | the configured `mail.Sender` implements `HealthChecker` | `mail.HealthChecker.Healthy` (TCP dial + HELO + QUIT for SMTP) |

Mail is opt-in by provider. SMTP implements `HealthChecker` natively; `noop`, `sendgrid` and external plugin senders do not, so deployments using those drivers will not see a `mail` row in the response.

Sample response body:

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

### `GET /metrics`

Prometheus / OpenMetrics exposition of the OTel MeterProvider's measurements. Mounted at the path configured by `metrics_path` (default `/metrics`); set `metrics_path: ""` in `nucleus.yml` to disable. Content-Type is `application/openmetrics-text`.

OTLP push (via `otlp_endpoint`) and Prometheus pull coexist on the same MeterProvider — instrumentation code is unchanged, and a deployment can scrape locally **and** push to an OTel collector without double-instrumenting.

## Circuit Breakers for External Dependencies

`pkg/circuit` provides a standalone breaker primitive for wrapping calls to mail, object storage, plugin bridges, or any third-party API whose unavailability should not cascade into a full outage. Standard `closed → open → half-open` state machine, configurable failure threshold / cooldown / half-open probe budget.

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
    // dependency is in cooldown — fall back to queue, return 503, etc.
}
```

The breaker is intentionally minimal — no event bus, no metrics surface, no per-call timeout. Compose those with `pkg/observe` (logging) and the `/metrics` MeterProvider (counters). It is **not** auto-wired into mail / storage / plugins; operators opt in where it makes sense.
