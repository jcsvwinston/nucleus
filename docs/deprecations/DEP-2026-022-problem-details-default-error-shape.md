# Behaviour-Change Notice: RFC 9457 problem details become the default error shape at v2.0.0

- ID: `DEP-2026-022`
- Status: `completed` (applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it)
- Announced in: the next minor after `v1.32.0` (A12 `M0-N`; decision D3 of the owner, 2026-10-16)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-17 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc (QADR-0010). This is a
  **default change of the application's error shape**: nothing is removed, no
  `// Deprecated:` marker carries it.
- Scope: `api`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

Every error an application answers — a handler's error, a binding failure,
the router's 404 and 405, a timeout, the refusals of the CSRF middleware, the
rate limiter, the authorizer and the bearer middleware — is written in the
framework's envelope:

```json
{"error": {"code": "NOT_FOUND", "message": "no route serves GET /api/nope"}}
```

A client that prefers `application/problem+json` in its `Accept` header gets
RFC 9457 problem details instead, and an application can make problem details
the shape for every client with `WithProblemDetails()` (A10). The envelope is
a shape of this framework; problem details are the standard one, and the
derived OpenAPI document already describes both. The owner decided that the
standard shape becomes the default at the major.

What changes and when:

- **This release:** the way to keep the envelope is added — `WithErrorEnvelope()`
  (`app.WithErrorEnvelope`, `nucleus.WithErrorEnvelope`,
  `AppBuilder.WithErrorEnvelope`), the opposite of `WithProblemDetails()`.
  It changes nothing today: the envelope is the default either way, and
  `App.ErrorEnvelope()` reports the choice. Passing both options is refused
  by `app.New`.
- **v2.0.0:** an application that passes neither option answers every error
  as `application/problem+json`. An application built with
  `WithErrorEnvelope()` keeps the envelope; a client of it that negotiates
  problem details through `Accept` still gets them, as today.

## Affected Surfaces

- Every error response of an application built without `WithProblemDetails()`
  or `WithErrorEnvelope()`.
- Clients that parse `{"error": {...}}` — including the Orbit admin panel,
  whose `ui/src/services/api.ts` reads the envelope only and learns problem
  details in its 1.x migration (`M0-O` of the A12 arc).
- Not affected: applications that already pass `WithProblemDetails()`; the
  success envelope and `ok`-shaped responses, which this notice does not
  touch; `errors.WriteError` callers, which follow the application's choice.

## Migration Path

- Replacement: choose the shape in code, before the upgrade.
  - Keep today's shape: `nucleus.New().WithErrorEnvelope()` (or
    `app.New(cfg, app.WithErrorEnvelope())`).
  - Adopt the standard shape early: `nucleus.New().WithProblemDetails()`.
  - Clients that must work with either: read `Content-Type` and parse
    `application/problem+json` (`type`, `title`, `status`, `detail`,
    `instance`, plus `code` and `details` as extension members) beside the
    envelope.
- Behavior differences: at v2.0.0 an application that chose nothing answers
  problem details; status codes do not change; `code` is still present, as an
  extension member.
- Required app changes: one builder call, or none to accept the new default.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-022-error-shape-choose-in-code.md`
- Detection rule: a composition root with neither `WithProblemDetails()` nor
  `WithErrorEnvelope()`. At run time, before v2.0.0, `App.ProblemDetails()`
  and `App.ErrorEnvelope()` both false; since v2.0.0 `App.ProblemDetails()`
  reports the effective shape — true for an application that chose nothing
  or passed `WithProblemDetails()`, false only under `WithErrorEnvelope()` —
  and `App.ErrorEnvelope()` reports the opt-out, so an application that
  chose nothing reads `ProblemDetails() == true`.
- Suggested rewrite: manual — one option.

## Validation

- Compatibility tests updated: `yes` — `pkg/app/error_shape_default_test.go`:
  `TestErrorShape_EnvelopeIsTheDefaultInV1` pinned the v1 default (the 2.0
  PR turned it into `TestErrorShape_ProblemDetailsAreTheDefault`),
  `TestErrorShape_WithErrorEnvelopeKeepsTheEnvelope` the option and the
  negotiation, `TestErrorShape_WithProblemDetailsFlipsIt` the opt-in and
  `TestErrorShape_BothOptionsAreRefused` the contradiction. The 2.0 PR
  changes the first in the same commit as the default. The additions are
  listed in `contracts/baseline/api_exported_symbols.txt` (additions only).
- Release note updated: through the squash commit.
- Rollback plan documented: `yes` — before v2.0.0 nothing to roll back; after
  it, `WithErrorEnvelope()` restores today's shape.

## Timeline

- Announcement date: `2026-10-16`
- Review checkpoint: `2027-01-17`, when the window opens.
- Behaviour flip decision date: the `v2.0.0` major.
- Applied: `dry-run/2.0`, 2026-10-23 — every error of an application that chose nothing is an RFC 9457 problem details document; `WithErrorEnvelope()` keeps the envelope; `App.ProblemDetails()` reports the effective shape (see Notes).

## Notes

The umbrella guard reads this notice through its `Status` and «no earlier
than» lines and the suite's list of the major
(`quantum/docs/planes/quantum-2.0.md` §3.0, row 24).

Applied on `dry-run/2.0` (2026-10-23). `App.ProblemDetails()` reports the
effective shape (true for an application that chose nothing), because the
derived OpenAPI document reads it to pick the error schema; the detection
rule above says so. The notice's "answers every error as
`application/problem+json`" holds for a client that sends no `Accept` or
`*/*`; a client whose `Accept` names only `application/json` gets the RFC
9457 document labelled `application/json` (the existing
`httpneg.ProblemContentType` rule of the opt-in mode, now the default),
pinned by `TestErrorShape_ProblemDetailsAreTheDefault` and the API-bench
control HT-06. The handlers `nucleus generate resource|module` write answer
their own `{"error": "<text>"}` bodies through `c.JSON`; they are outside
the framework's error shape and this notice.
