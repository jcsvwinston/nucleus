# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    None active. Last completed: ADR-010 Phase 4 Slice 1 — `examples/mvc_api` reference app — COMPLETE, committed + pushed. Finished for the day.
BRANCH:       main (clean working tree once the state commit lands; in sync with origin/main).
LAST COMMIT:  9e27243 feat(examples): mvc_api reference app (ADR-010 Phase 4, Slice 1)  (followed by the `chore(state): close Phase 4 Slice 1 iteration` commit carrying this file).
STATUS:       No active iteration. `examples/mvc_api` (the first post-Phase-1 reference app) is committed (9e27243) and pushed; written POST-commit so the next /resume sees the shipped state. The example was VERIFIED END-TO-END (server runs, full CRUD 200/201/.../404) — and running it (which unit tests + 3 reviews had not) caught + fixed a startup panic and wrong doc commands. It RUNS but uses documented workarounds for framework gaps; no `pkg/` was changed this slice. The mvc_api dev server is STOPPED and its examples_mvc_api.db removed.
NEXT STEP:    Owner selects the next iteration. Recommended (and architect-flagged as a BLOCKER before the website imports the example):
  1. Gap-1 — pass a `nucleus.Runtime` handle into `ModuleSpec.OnStart`/`OnShutdown` so modules use `rt.DB()`/`AutoMigrate` instead of opening their own connection; then simplify `examples/mvc_api`. Pre-v1.0 `Module[C]`/`ModuleSpec` signature change (no external consumers ⇒ no deprecation) + ADR-010 amendment + freeze rebaseline. Gates Phase 4 Slice 2 (website include-from-source).
  2. P1 — `WithoutDefaults()` doesn't suppress the admin bootstrap user (`pkg/app/app.go:~272` `EnsureBootstrapAdminUser` is before the `!o.skipDefaults` guard); a small, near-term, `pkg/app`-only fix. (Panel itself IS correctly 404'd under WithoutDefaults — verified; this is a leaked-orphaned-user bug, not an exposed-portal bug.)
  3. ADR-010 §2 layer 4 (referential validation); or ADR-010 Phase 4 Slice 2 (website include-from-source, gated on Gap-1).
BLOCKERS:     none.
FILES OF INTEREST:
  - .claude/state/CURRENT_ITERATION.md — full prioritised candidate list + carry-forward follow-ups (incl. P1, P2, Gap-1, Module.Models→admin-registry, smtp_port referential, Oracle items).
  - examples/mvc_api/ — the reference app; README documents the known limitations/workarounds.
  - docs/iterations/2026-05-24-adr010-phase4-slice1-mvc-api.md — this iteration's archive.
NOTES:
  - Framework bugs/gaps surfaced by mvc_api (ALL unfixed — recorded as follow-ups, no pkg/ touched): **P1** WithoutDefaults leaks admin bootstrap user; **P2** `Router.Resource("")` under a `Prefix` panics (`pkg/nucleus/router.go`); **Gap-1** modules can't reach the managed `*sql.DB`/AutoMigrate (`OnStart` gets `*nucleus.App`); **Module.Models** not wired to the admin model registry; **Gap-2** Routes-before-OnStart ordering (documented, lazy-accessor workaround). The example WORKS by routing around these.
  - Process lesson (recorded in the archive): reference apps need an end-to-end run (migrate → start → curl), not just unit tests + review — that's what caught the startup panic.
  - Recent shipped arc on origin/main: ADR-010 Phase 3a/3b/3.1, Oracle identifier-casing (ADR-011), session_cookie_secure, Oracle multi-block (db.ExecScript), ADR-010 §2 layer-3 validation, website-audit + docs-content-verifier, and now Phase 4 Slice 1 (examples/mvc_api).

Updated: 2026-05-24
