# Dependency Impact Report

- Generated at (UTC): 2026-05-28T14:24:38Z
- Branch: `main`
- Commit: `510c375`
- Baseline ref: `v0.7.0`
- Baseline status: available

## Summary

- Direct dependency changes: 11
- Critical dependency changes: 2
- go.mod changed vs baseline: yes
- go.sum changed vs baseline: yes
- Decision: CRITICAL REVIEW REQUIRED

## Changed Direct Dependencies

| Type | Module | Old | New | Critical |
| --- | --- | --- | --- | --- |
| added | `github.com/aws/aws-sdk-go-v2/config` | `-` | `v1.32.17` | no |
| added | `github.com/aws/aws-sdk-go-v2/service/secretsmanager` | `-` | `v1.41.7` | no |
| added | `github.com/knadh/koanf/parsers/json` | `-` | `v1.0.0` | no |
| added | `github.com/knadh/koanf/parsers/toml/v2` | `-` | `v2.2.1` | no |
| added | `github.com/knadh/koanf/providers/rawbytes` | `-` | `v1.0.0` | no |
| added | `github.com/prometheus/client_golang` | `-` | `v1.23.2` | no |
| changed | `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp` | `v1.35.0` | `v1.43.0` | yes |
| added | `go.opentelemetry.io/otel/exporters/prometheus` | `-` | `v0.65.0` | yes |
| added | `go.yaml.in/yaml/v3` | `-` | `v3.0.4` | no |
| changed | `golang.org/x/crypto` | `v0.50.0` | `v0.51.0` | no |
| changed | `golang.org/x/net` | `v0.53.0` | `v0.55.0` | no |

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
