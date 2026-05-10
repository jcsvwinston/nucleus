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
| `GET /healthz`     | Liveness + dependency status (DB, Redis, mail).          |

The response is a deterministic JSON shape suitable for Kubernetes
probes and external uptime monitors. The set of dependencies that gate
the response is determined by the config — there is no probe magic.

## Metrics

When OTel is enabled, the runtime exports:

- HTTP request count and latency histograms,
- SQL pool stats (in use, idle, wait time),
- session store hit / miss / eviction counters,
- background-task queue depth and latency (when `pkg/tasks` is wired).

Metrics flow through the OTel exporter you configure — there is no
separate Prometheus exposition path. If you need Prometheus, point
your collector at the OTel endpoint and let it relay.

## What you do not have to do

- No "logger" object to thread through every constructor — the request
  logger lives on the request `context.Context`.
- No bespoke tracing API — you use `go.opentelemetry.io/otel/trace`
  directly.
- No opinionated metrics SDK — you emit via OTel and pick your backend
  at deploy time.
