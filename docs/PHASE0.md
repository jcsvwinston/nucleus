# Phase 0 - Contract and Technical Direction

Reference date: 2026-04-05.
Status: Closed.

## Objective

Establish the non-negotiable architecture contract for GoFrame before implementation scaling.

## Decisions Locked in Phase 0

- SQL runtime path: Bun-first
- HTTP routing: Chi
- Worker runtime: Asynq
- Observability baseline: OpenTelemetry
- Security baseline: structured guardrails and production checks
- Framework extensibility via explicit interfaces and plugin-like command hooks

## Outcomes

- Core architecture direction documented in `SPEC.md`
- Initial package boundaries agreed
- Development roadmap split into incremental phases
