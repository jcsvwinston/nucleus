# ADR-012: Prometheus Metrics Exposition via the OTel SDK (`pkg/observe`)

**Status:** Accepted (retrospective)
**Date:** 2026-05-26
**Superseded:** No

## Context

The `/metrics` Prometheus endpoint feature shipped in commit `9df1abe`
("feat(observe,app): /metrics Prometheus endpoint via OTel exporter (P1) (#43)")
during the v0.6.0 → v0.7.0 development window. It introduced two new
**direct** third-party dependencies into `pkg/observe`:

- `github.com/prometheus/client_golang` (the `prometheus` and
  `prometheus/promhttp` packages)
- `go.opentelemetry.io/otel/exporters/prometheus`

At the time, the ADR index only ran to ADR-004, and `CLAUDE.md` §3 —
"new third-party deps require an ADR (`docs/adrs/`) and a
`dependency-impact` review" — was not honoured for this addition. The
`dependency-impact` review surfaced the gap during the v0.8.0
`/release-prep` pass (the two modules were promoted from `// indirect`
to direct in the v0.7.0 → v0.8.0 window, which is when the report first
flagged them). This ADR is written **retrospectively** to record the
decision and close the §3 gap before v0.8.0 is tagged. It documents an
addition that has been live and tested since v0.7.0; it does not change
behaviour.

The hard constraint is the dependency firewall
(`contracts/firewall_test.go`): no third-party type may appear in an
exported `pkg/*` signature. `pkg/observe` is a firewalled package, and
`go.opentelemetry.io/otel` is already on its forbidden-import list.

The relevant non-negotiable principle is stdlib-first (ADR-001 / SPEC
§2.1). Prometheus pull-based exposition has no standard-library
equivalent, and OpenTelemetry was already the chosen observability SDK
(its OTLP exporters were direct dependencies at v0.7.0). The Prometheus
exporter is a member of that same already-accepted SDK family.

## Decision

`pkg/observe` exposes **pull-based Prometheus metrics** through the
OpenTelemetry SDK's Prometheus exporter, behind a single exported
function whose signature names only standard-library types:

```go
func SetupOpenTelemetry(
    ctx context.Context,
    cfg TelemetryConfig,
    logger *slog.Logger,
) (shutdown func(context.Context) error, metricsHandler http.Handler, err error)
```

- When `TelemetryConfig.PrometheusEnabled` is set, `SetupOpenTelemetry`
  constructs a `prometheus.NewRegistry()`, attaches it to the OTel
  `MeterProvider` via `otelprom.New(otelprom.WithRegisterer(registry))`,
  and returns `promhttp.HandlerFor(registry, …)` as the
  `metricsHandler`. The application layer mounts that handler at
  `/metrics`.
- `TelemetryConfig` carries only primitive fields (`ServiceName string`,
  `OTLPEndpoint string`, `PrometheusEnabled bool`).
- The `prometheus.Registry`, the `otelprom` reader, and the `promhttp`
  handler are **local variables inside `SetupOpenTelemetry`**. No
  exported symbol in `pkg/observe` (or anywhere in `pkg/*`) names a
  Prometheus or OTel-exporter type. The returned `metricsHandler` is the
  stdlib `http.Handler` interface.

The two metrics-exposition paths are independent and complementary:
OTLP push (`otlpmetrichttp`, already a v0.7.0 dependency) serves
collectors that pull over OTLP; the Prometheus reader serves operators
who scrape a `/metrics` endpoint. The Prometheus reader is attached to
the `MeterProvider` regardless of whether OTLP is configured.

To keep the firewall enforcing this confinement going forward, the
exact import paths are added to `forbiddenThirdParty` in
`contracts/firewall_test.go`:
`github.com/prometheus/client_golang/prometheus`,
`github.com/prometheus/client_golang/prometheus/promhttp`, and
`go.opentelemetry.io/otel/exporters/prometheus`. (The pre-existing
`go.opentelemetry.io/otel` entry is an exact-match key and did not cover
the exporter submodule.)

## Consequences

### Positive

- Operators get a standard `/metrics` scrape target without running an
  OTLP collector — the dominant deployment model for Prometheus shops.
- The dependency firewall stays intact: the `dependency-impact` review
  (v0.8.0) and `contracts/firewall_test.go` confirm no Prometheus or
  OTel-exporter type appears in a stable `pkg/*` signature.
- The §3 ADR gap is closed; future additions to the OTel exporter family
  now have a precedent decision to extend rather than re-litigate.

### Negative

- **Two third-party dependencies** in `pkg/observe`:
  `github.com/prometheus/client_golang` and
  `go.opentelemetry.io/otel/exporters/prometheus`, plus their
  transitive set (`prometheus/client_model`, `prometheus/common`,
  `prometheus/procfs`, `golang/protobuf`/`google.golang.org/protobuf`,
  most of which were already in the module graph via the OTel SDK). This
  is a deliberate, gated exception to stdlib-first (ADR-001): there is no
  stdlib metrics-exposition format, and re-implementing the OpenMetrics
  text protocol by hand would be more risk than vendoring the canonical
  client.
- The `dependency_impact_report.sh` `is_critical_module()` check
  prefix-matches the entire `go.opentelemetry.io/otel` family, so the
  exporter will trip "CRITICAL REVIEW REQUIRED" on every future
  release-prep. That is intended — the family is high-surface and any
  addition to it should be reviewed.

### Neutral

- The Prometheus reader is always attached when `PrometheusEnabled` is
  set, independent of OTLP. Operators who use neither pay nothing — the
  exporter is only constructed when the config flag is set.
- This ADR is retrospective. The feature, its tests
  (`TestEndpointsDocParity_DocumentedEndpointsRespond`, `/metrics` case),
  and the firewall guarantee have been in effect since v0.7.0; nothing in
  behaviour changes with this acceptance.

## Compliance

After this ADR is accepted:

1. `pkg/observe.SetupOpenTelemetry` returns the Prometheus scrape handler
   as a stdlib `http.Handler`; no Prometheus/OTel-exporter type is
   exported.
2. `contracts/firewall_test.go` lists
   `github.com/prometheus/client_golang/prometheus`,
   `github.com/prometheus/client_golang/prometheus/promhttp`, and
   `go.opentelemetry.io/otel/exporters/prometheus` in
   `forbiddenThirdParty`, and the firewall test passes.
3. A `dependency-impact` review for the two modules is recorded for the
   v0.8.0 release (`dist/reports/dependency_impact_report.md`).
4. `go list` confirms the Prometheus exporter is imported only by
   `pkg/observe`.

## Related

- [`pkg/observe/otel.go`](../../pkg/observe/otel.go) — `SetupOpenTelemetry`
  and the Prometheus reader.
- [`contracts/firewall_test.go`](../../contracts/firewall_test.go) —
  the dependency firewall this ADR keeps green.
- ADR-001: stdlib-first runtime — the principle this addition is a
  deliberate, gated exception to.
- ADR-005: ES256 + AWS Secrets Manager — the prior dependency ADR whose
  firewall-confinement pattern this one mirrors.
- `CLAUDE.md` §3 — the rule requiring an ADR + `dependency-impact`
  review for new third-party dependencies.
