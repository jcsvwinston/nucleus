# Critical Dependency Review — v0.8.0

**Date:** 2026-05-27
**Reviewer:** maintainer (`@jcsvwinston`), assisted by the `dependency-impact` agent. *(Drafted during `/release-prep`; final accept-and-ship is the maintainer's sign-off.)*
**Baseline:** `v0.7.0` (`ed5689b`) → **HEAD:** `510c375` (v0.8.0 candidate).
**Trigger:** `scripts/release/generate_dependency_impact_report.sh --enforce-critical-review` exited 2 — two critical-set dependencies changed (see `docs/reports/dependency_impact_2026-05-27.md`).

## Scope

Two dependencies in the critical set (`docs/governance/COMPATIBILITY_SLO.md`) moved between `v0.7.0` and the v0.8.0 candidate. Both are OpenTelemetry-family. This note records the review that `--enforce-critical-review` mandates.

## 1. `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` — v1.35.0 → v1.43.0

**Verdict: ACCEPT (safe to ship).**

- **Change class:** minor bump across eight releases, same major (v1). No breaking changes published in the range; the consumed surface (`New`, `Option`, `WithEndpoint`, `WithInsecure`) is unchanged. The bump also eliminates a split-tree inconsistency — the rest of the otel tree (`otel`, `otel/metric`, `otel/trace`, `otel/sdk`, `otlptracehttp`) was already at v1.43.0 while this exporter was stranded at v1.35.0.
- **CVE:** clears **GO-2026-4985** (oversized OTLP HTTP response bodies → memory exhaustion). This was a `govulncheck`-flagged, *called* vulnerability reaching `pkg/observability/bus.go`; `govulncheck ./...` is now clean.
- **Blast radius:** single import site, `pkg/observe/otel.go`. `otlpmetrichttp.Option`/exporter values are constructed and consumed inside private helpers; no otel type appears in any exported `pkg/*` signature. `contracts/firewall_test.go` lists `otlpmetrichttp` in `forbiddenThirdParty` and passes.
- **License:** Apache-2.0, unchanged, compatible.

## 2. `go.opentelemetry.io/otel/exporters/prometheus` — added at v0.65.0

**Verdict: ACCEPT (safe to ship).**

- **Change class:** new direct dependency, introduced this cycle by **ADR-012 (Prometheus metrics exporter)**. The architecture decision was reviewed and accepted (architect-reviewer + contract-guardian PASS) when ADR-012 landed (`b829855`).
- **Blast radius:** confined to `pkg/observe`. The Prometheus registry/handler stays internal and is exposed only via the `http.Handler` that `observe.SetupOpenTelemetry` returns — no Prometheus type appears in an exported `pkg/*` signature. The firewall (`contracts/firewall_test.go`) was hardened in the same ADR with forbidden-import entries for `go.opentelemetry.io/otel/exporters/prometheus`, `github.com/prometheus/client_golang/prometheus`, and `.../promhttp`, and passes.
- **License:** Apache-2.0, compatible.

## Decision

Both critical dependency changes are **reviewed and ACCEPTED** for v0.8.0. No additional ADR is required (the prometheus exporter is governed by ADR-012; the otlpmetrichttp bump is a security patch within an existing major). The acceptance is recorded in `CHANGELOG.md` (the dep-CVE Security entry and ADR-012).

## Other (non-critical) dependency changes since v0.7.0

For completeness — these did not trigger the critical-review gate and are recorded only for the audit trail:

| Module | Old → New | Note |
|--------|-----------|------|
| `github.com/prometheus/client_golang` | added v1.23.2 | transitive of the otel prometheus exporter (ADR-012); stays internal |
| `go.yaml.in/yaml/v3` | added v3.0.4 | ADR-010 Phase 3.1 — YAML `file:line` provenance via `yaml.Node`; confined to unexported config helpers |
| `github.com/knadh/koanf/parsers/{json,toml/v2}`, `providers/rawbytes` | added | ADR-010 Phase 2b multi-format config loader |
| `github.com/aws/aws-sdk-go-v2/{config,service/secretsmanager}` | added | AWS Secrets Manager resolver (ADR-005); confined behind the internal `secretsManagerAPI` interface, firewall-guarded |
| `golang.org/x/net` | v0.53.0 → v0.55.0 | security maintenance — clears GO-2026-5026 |
| `github.com/go-jose/go-jose/v4` | v4.1.3 → v4.1.4 | indirect; clears GO-2026-4945 (JWE decryption panic) |
