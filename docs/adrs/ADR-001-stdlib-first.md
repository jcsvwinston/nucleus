# ADR-001: stdlib-First Runtime Design

**Status:** Accepted
**Date:** 2026-03-01
**Superseded:** No

## Context

Go's standard library provides robust, production-ready building blocks: `net/http` for HTTP, `database/sql` for database access, `log/slog` for structured logging, and `context` for request lifecycle. However, many Go web frameworks wrap or replace these with third-party alternatives, creating:

1. **Dependency lock-in**: Applications become tied to framework-specific abstractions.
2. **Upgrade friction**: Framework updates break when underlying stdlib changes.
3. **Learning curve**: Developers must learn framework-specific patterns instead of portable Go idioms.
4. **Debugging complexity**: Stack traces include framework layers that obscure root causes.

## Decision

Nucleus is designed **stdlib-first**:

- **HTTP**: Uses `net/http` directly with a custom lightweight router (`pkg/router`). No Chi, Gin, or Echo as runtime dependencies.
- **Database**: Uses `database/sql` directly with driver-specific connection strings. No ORM abstraction (no GORM, Bun, or universal SQL layer).
- **Logging**: Uses `log/slog` (Go 1.21+) with OpenTelemetry integration. No zap, zerolog, or logrus.
- **Context**: Uses `context.Context` throughout for request lifecycle, cancellation, and value passing.

Third-party libraries are used **only when stdlib doesn't provide equivalent functionality**:

| Need | stdlib Gap | Chosen Library |
|------|-----------|----------------|
| Configuration | No YAML/env parsing | `koanf/v2` |
| JWT | No JWT support | `jwt/v5` |
| Sessions | No session management | `scs/v2` |
| Authorization | No policy engine | `casbin/v2` |
| Input validation | No struct tag validation | `validator/v10` |
| Background jobs | No job queue | `hibiken/asynq` (Redis-backed) |
| OpenTelemetry | No OTLP exporter | `otel/*` SDK packages |
| Prometheus exposition | No OpenMetrics text format | `prometheus/client_golang` + `otel/exporters/prometheus` (ADR-012) |

## Consequences

### Positive

- **Portability**: Nucleus applications use idiomatic Go patterns transferable to any Go project.
- **Debuggability**: Stack traces are shallow; errors originate from stdlib or application code.
- **Upgrade safety**: Go version bumps don't break framework internals.
- **Smaller dependency tree**: Fewer transitive dependencies mean fewer security vulnerabilities and faster builds.
- **Predictable behavior**: stdlib behavior is stable and well-documented.

### Negative

- **More framework code**: Nucleus must implement routing, middleware chains, and request context helpers that other frameworks get from dependencies.
- **Reinvention risk**: Custom router/middleware must be thoroughly tested to match battle-tested alternatives.
- **Feature gap**: Some advanced features (e.g., automatic OpenAPI generation) may require third-party integration work.

## Compliance

All new code in Nucleus must:

1. Prefer stdlib packages over third-party alternatives when functionally equivalent.
2. Justify any new runtime dependency in the PR description.
3. Document the stdlib gap that the dependency fills.
