# Observability Baseline

Reference date: 2026-04-05.
Status: Current.

This document defines the recommended minimum dashboards and alerts for GoFrame services in production.

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
- `db.engine` (`bun`, `gorm`)
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
- Keep exploratory DB engines (`mssql`, `oracle`) out of hard alert gates until first-class support is shipped.
