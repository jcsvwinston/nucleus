# Enterprise Roadmap (Current Status)

Reference date: 2026-04-05.
Status: Current.

This roadmap summarizes the enterprise-alignment track for GoFrame.

Execution plan for the next major iteration:

- `docs/V0.6.0_ROADMAP.md`

## Strategic Goal

Deliver a Django-inspired framework baseline for Go teams that is:

- secure by default
- observable in production
- extensible without core rewrites
- operationally friendly through a strong CLI

## Completed Baseline

- Bun-first SQL runtime and migration lifecycle
- Asynq-based background job layer (`pkg/tasks`) and worker scaffold
- OpenTelemetry bootstrap and HTTP telemetry middleware
- configurable rate limiting and deploy checks
- broad CLI parity track with Django-inspired command set
- mail provider architecture with capability-based plugin runtime (`goframe-plugin-<driver>`) and legacy fallback (`goframe-mail-<driver>`)

## Current Gaps to Keep Improving

1. deeper DB/job telemetry semantics
2. advanced rate-limit policy dimensions (burst, route, role)
3. further security hardening in sensitive input/output paths
4. incremental UX and docs polish for external adopters

## Exit Criteria for Each Iteration

- backward compatibility preserved unless explicitly declared
- tests and release rehearsal green
- docs and changelog updated in the same change set
