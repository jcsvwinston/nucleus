# Iteration archive — 2026-05-22 website coverage manifests

> Archived 2026-05-22 as part of the session-end `/handoff`. Landed as the
> feature commit `bbc7d60` (`docs(website): add covers/config_keys manifests
> to docs pages`) on `main`, followed by the usual `chore(state): close`
> state commit. Candidate #1 from the prior queue (surfaced by
> website-curator 2026-05-21).

## Goal

Add `covers:`/`config_keys:` frontmatter manifests to the 14 `website/docs/`
pages that lacked one, so each page declares the stable symbols and config
keys it documents and the drift guard can flag a page when a documented
symbol is later removed from the freeze baseline.

## Scope

### In

- `covers:`/`config_keys:` frontmatter added to 14 pages (frontmatter only,
  no body edits): `intro.md`, `architecture/{compatibility,principles}.md`,
  `cli/overview.md`, `concepts/{application,configuration,routing}.md`,
  `features/{admin,auth,observability,storage-and-tasks}.md`,
  `getting-started/{installation,project-structure,quickstart}.md`.
  (`concepts/models-and-database.md` already had a manifest — the format
  template.)
- Authored via the `website-curator` subagent.

### Out

- No page-body edits, no new pages, no internal-docs / examples / godoc
  changes.

## Key constraint discovered

`scripts/website/check-coverage.sh` check #2 scans the FULL page body (not
just frontmatter) for `pkg/<pkg>.<Symbol>` tokens and validates each against
`contracts/baseline/api_exported_symbols.txt` (stable symbols). Therefore
`covers:` may list ONLY stable symbols — experimental/transitional surfaces
(`pkg/observability`, `pkg/openapi`, `pkg/admin`, `pkg/outbox`,
`pkg/auth/secrets`, `pkg/tasks/providers/*`) are deliberately excluded; the
observability/storage pages cover the stable parts they touch
(`pkg/observe`, `pkg/circuit`, `pkg/health`, `pkg/storage`, `pkg/tasks`)
instead. `config_keys:` is NOT guard-validated but was kept honest against
`docs/reference/CONFIG_KEY_REGISTRY.md`. Pages with no stable API surface
(`architecture/compatibility.md`, `cli/overview.md`,
`getting-started/installation.md`) use `covers: []`.

## Acceptance criteria — all met

- [x] `scripts/website/check-coverage.sh --strict` → 0 legacy, 0 dangling,
  0 no-manifest.
- [x] `covers:` entries verified present in the freeze baseline (incl.
  interface methods, e.g. `pkg/app.Extension.Attach`); `config_keys:` verified
  against the registry (e.g. `rate_limit_*` are real `stable` keys).
- [x] Frontmatter-only diff across exactly 14 pages; Docusaurus
  `npm run build` succeeds.

## Outcome

Landed as feature commit `bbc7d60`. The drift guard's dangling-`covers:` check
now has real per-page signal: removing a stable symbol a page documents will
surface as a DANGLING finding under `--strict`. Verification was independent
of the subagent's self-report (guard re-run, diff-scope check, baseline +
registry spot-checks, build).

## Follow-ups

- (Optional, candidate list) Promote the advisory `website-drift` CI job to a
  required gate now that manifests exist and give the dangling-ref check
  steady signal.
