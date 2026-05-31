# Iteration Archive — 2026-05-29 examples/ + CLAUDE.md directory-map reconciliation

> Archived by `session-curator` on 2026-05-29.
> Status at archival: **COMPLETE** — PR #86 squash-merged to `main`
> (commit `ebb3ca3`, "chore(claude.md): reconcile examples/ directory-map
> with reality (#86)"). All 12 CI checks passed.

---

## Goal

Reconcile the `examples/` directory map advertised in `CLAUDE.md` (and the
`examples-maintainer` agent) with the actual tracked state of the repository,
resolving audit item OTH-1 from
`docs/audits/2026-05-29-exhaustive-audit.md` (lines 267–275).

## Scope

- in: `CLAUDE.md` (directory-map row, Examples section, dispatch table),
  `.claude/agents/examples-maintainer.md` (description, "Current state"
  section, "Examples in scope" section, output-contract example).
- out: any `pkg/` changes, any `internal/` changes, new features, tests,
  contracts, CHANGELOG (internal operational doc + agent config only — no
  shipped user-visible behaviour change).

## Acceptance criteria

- [x] `CLAUDE.md` directory-map row for `examples/` reflects only
      `mvc_api` as the tracked reference app today.
- [x] `CLAUDE.md` Examples section and dispatch table note that
      `fleetmanager`, `ecommerce_dashboard`, and `plugins/*` are deferred
      to v0.9.X, and `showcase_demo` is permanently retired.
- [x] `.claude/agents/examples-maintainer.md` description, "Current state"
      section, and "Examples in scope" section mirror the same decisions.
- [x] ~305 MB / ~19,346 untracked local-only files removed from disk
      (frontend node_modules + runtime DBs + logs from
      `examples/{fleetmanager,ecommerce_dashboard,showcase_demo}`); zero
      tracked files deleted (those paths held no tracked content).
- [x] All 12 CI checks passed on PR #86 (no tracked Go code was changed;
      build matrix + contract freeze unaffected).
- [x] PR opened and merged via the protected-`main` flow — PR #86
      squash-merged 2026-05-29; `main` advanced `b379b61..ebb3ca3`.

## Status at archival: COMPLETE

### Done

- 2026-05-29 — `CLAUDE.md`: updated directory-map row, Examples section,
  and dispatch table to reflect that `examples/mvc_api` is the only tracked
  reference app (Phase 4 Slice 1, 2026-05-24); `fleetmanager`,
  `ecommerce_dashboard`, and `plugins/*` deferred to v0.9.X per ADR-010
  Phase 4; `showcase_demo` permanently removed (Quark dependency, per
  `NUCLEUS_RENAME_BRIEF.md`). +40/−27 net across 2 files.
- 2026-05-29 — `.claude/agents/examples-maintainer.md`: rewrote description,
  "Current state" section, and "Examples in scope" section to match the
  reconciled reality. Agent output-contract example updated accordingly.
- 2026-05-29 — On-disk cruft deletion: ~305 MB / ~19,346 untracked files
  removed from `examples/{fleetmanager,ecommerce_dashboard,showcase_demo}`
  (frontend node_modules, SQLite runtime DBs, logs). Zero tracked files were
  affected; `git status` remained clean throughout.
- 2026-05-29 — PR #86 squash-merged to `main`; `main` now at commit
  `ebb3ca3`.

## Notes / decisions log

- 2026-05-29 — Resolves audit OTH-1 (`docs/audits/2026-05-29-exhaustive-audit.md`
  lines 267–275): the CLAUDE.md directory map previously advertised four
  example apps (`mvc_api`, `fleetmanager`, `ecommerce_dashboard`,
  `showcase_demo`, `plugins/…`) while the repo only tracked `mvc_api`.
- 2026-05-29 — Decision: `mvc_api` is the sole tracked reference app for
  the active development phase. `fleetmanager`, `ecommerce_dashboard`, and
  `plugins/*` remain on the v0.9.X roadmap per ADR-010 Phase 4 /
  `docs/iterations/2026-05-16-adr010-phase1.md` (or nearest Phase 4 doc).
  `showcase_demo` is permanently retired due to the Quark dependency removed
  in the nucleus rename (see `NUCLEUS_RENAME_BRIEF.md` and
  `PLUGIN_SDK.md`).
- 2026-05-29 — Semver impact: **none**. This change touched only internal
  operational docs (`CLAUDE.md`) and a Claude Code agent config file
  (`.claude/agents/examples-maintainer.md`). No shipped user-visible
  behaviour changed; no CHANGELOG entry required.
- 2026-05-29 — All CI passed: 12/12 checks green on PR #86.
- 2026-05-29 — This iteration was scope-light and was not pre-written into
  `CURRENT_ITERATION.md`; this archive is the primary record.
