# Dependency Impact Report

- Generated at (UTC): 2026-05-14T10:22:53Z
- Branch: `claude/interesting-ishizaka-d51a45`
- Commit: `fce7c57`
- Baseline ref: `v0.6.0`
- Baseline status: available

## Summary

- Direct dependency changes: 7
- Critical dependency changes: 2
- go.mod changed vs baseline: yes
- go.sum changed vs baseline: yes
- Decision: CRITICAL REVIEW REQUIRED

## Changed Direct Dependencies

| Type | Module | Old | New | Critical |
| --- | --- | --- | --- | --- |
| added | `github.com/bradfitz/gomemcache` | `-` | `v0.0.0-20260422231931-4d751bb6e37c` | no |
| changed | `github.com/microsoft/go-mssqldb` | `v1.8.2` | `v1.10.0` | yes |
| added | `github.com/robfig/cron/v3` | `-` | `v3.0.1` | no |
| changed | `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` | `v1.35.0` | `v1.43.0` | yes |
| added | `go.uber.org/goleak` | `-` | `v1.3.0` | no |
| changed | `golang.org/x/crypto` | `v0.49.0` | `v0.50.0` | no |
| changed | `golang.org/x/net` | `v0.52.0` | `v0.53.0` | no |

## Critical Dependency Set

- `github.com/casbin/casbin/v2`
- `github.com/go-sql-driver/mysql`
- `github.com/jackc/pgx/v5`
- `github.com/microsoft/go-mssqldb`
- `github.com/redis/go-redis/v9`
- `github.com/sijms/go-ora/v2`
- `github.com/hibiken/asynq`
- `modernc.org/sqlite`
- `go.opentelemetry.io/otel` (and submodules)
