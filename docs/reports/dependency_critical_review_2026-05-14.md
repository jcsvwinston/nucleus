# Critical Dependency Review — v0.7.0

**Date:** 2026-05-14
**Reviewer:** maintainer (`@jcsvwinston`), assisted by the `dependency-impact` agent.
**Baseline:** `v0.6.0` → **HEAD:** `fce7c57` (v0.7.0 candidate).
**Trigger:** `scripts/release/generate_dependency_impact_report.sh --enforce-critical-review` exited non-zero — two critical-set dependencies changed.

## Scope

Two dependencies in the critical set (`docs/governance/COMPATIBILITY_SLO.md`) moved between `v0.6.0` and the v0.7.0 candidate. Both are **direct** dependencies. This note records the review that `--enforce-critical-review` mandates.

## 1. `github.com/microsoft/go-mssqldb` — v1.8.2 → v1.10.0

**Verdict: ACCEPT (safe to ship).**

- **Change class:** minor bump. Changelog across v1.9.x–v1.10.0 is entirely additive — new connection-string parameters (`FailoverPartnerSPN`, `serverCertificate`), `NewConnectorWithProcessQueryText`, nullable civil types — plus bug fixes (bulkcopy column escaping, TLS handshake flush, `XACT_ABORT` auto-commit detection, ARM64 named-pipe fix). No removed or renamed public symbols.
- **CVEs:** none introduced. v1.9.6/v1.9.8 were explicit dependency advances to clear `govulncheck` findings in the transitive graph (`golang.org/x/crypto` advanced to v0.50.0 by v1.10.0). The chain moved *away* from known-vulnerable versions.
- **Blast radius:** confined to `pkg/db/driver_mssql.go` — a single blank `database/sql` driver import behind the `mssql` build tag. No `go-mssqldb` type appears in any exported `pkg/*` signature; leakage is structurally impossible for a blank import. `contracts/firewall_test.go` passes.
- **Functional evidence:** the 2026-05-14 MSSQL/Oracle CI stability drill on `fce7c57` (which already carries v1.10.0) passed 10/10 — the strongest possible runtime signal for a driver-registration-only dependency.
- **License:** BSD-3-Clause, unchanged, compatible.

## 2. `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` — v1.35.0 → v1.43.0

**Verdict: ACCEPT (safe to ship).**

- **Change class:** minor bump across eight releases. No breaking changes published in the range — the `otlptracehttp` public surface (`New`, `Option`, `WithEndpoint`, `WithInsecure`, `WithHTTPClient`, …) is unchanged. Notable additions: `WithHTTPClient` (v1.36.0), experimental exporter self-observability metrics (v1.39.0), W3C Trace Context Level 2 random-trace-ID flag support (v1.43.0). v1.42.0 dropped Go 1.24 support; Nucleus targets Go 1.25+, so no impact.
- **CVEs:** none in the range.
- **Blast radius:** single import site at `pkg/observe/otel.go`. `otlptracehttp.Option` values are constructed and passed to `otlptracehttp.New()` inside private helpers; neither the option type nor the exporter type appears in any exported function or struct in `pkg/observe`. The public surface (`TelemetryConfig`, `SetupOpenTelemetry`) uses stdlib types only. `contracts/firewall_test.go` lists `go.opentelemetry.io/otel` in `forbiddenThirdParty` and passes.
- **License:** Apache-2.0, unchanged, compatible.

## Decision

Both critical dependency changes are **reviewed and ACCEPTED** for v0.7.0. No ADR is required — neither dependency adds a third-party type to a stable `pkg/*` signature. The acceptance is also recorded in `CHANGELOG.md` under the v0.7.0 `### Dependencies` section.

## Other (non-critical) dependency changes since v0.6.0

For completeness — these did not trigger the critical-review gate and are recorded here only for the audit trail:

| Module | Old → New | Note |
|--------|-----------|------|
| `github.com/bradfitz/gomemcache` | added | memcached session store backend |
| `github.com/robfig/cron/v3` | added | scheduled-task cron expressions |
| `go.uber.org/goleak` | added | test-only — goroutine leak detection |
| `golang.org/x/crypto` | v0.49.0 → v0.50.0 | routine security maintenance |
| `golang.org/x/net` | v0.52.0 → v0.53.0 | routine security maintenance |
