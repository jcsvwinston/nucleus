# Iteration archive — 2026-05-21 website refresh + website-curator subagent

> Archived 2026-05-21 as part of the session-end `/handoff`. Landed as two
> commits on `main` (both pushed to `origin/main`):
> - `3ca91ce` — `docs(website): refresh public site to match shipped Nucleus + drift guard`
> - `5a79095` — `chore(agents): add website-curator subagent + wire into loop and commands`
>
> These commits are docs + tooling only; no shipped runtime surface changed.
> No CHANGELOG entry; no semver bump.

## Goal

Refresh the public Docusaurus site (`website/docs/`) so every page faithfully
reflects the shipped Nucleus behaviour (as of 2026-05-21), add a heuristic
drift guard to catch future regressions automatically, and introduce a
dedicated `website-curator` subagent so ownership of the public site is
explicit in the iteration loop and slash commands.

## Scope

### In

**Commit `3ca91ce` — website content + drift guard**

- `website/docs/cli/overview.md` — corrected nucleus shell description
  (SQL shell, not "Go REPL"); documented the full primary-command set;
  fixed `migrate steps` flag attribution.
- `website/docs/concepts/configuration.md` — documented the multi-file
  `FromConfigFile` merge engine (ADR-010 Phases 2b–2d).
- `website/docs/getting-started/quickstart.md` — replaced a v0.9.X
  placeholder with a real example using the shipped `pkg/nucleus` API.
- `website/docs/features/auth.md` — documented the AWS Secrets Manager
  `aws-sm:` resolver.
- `website/docs/concepts/routing.md` — canonical `nucleus.Router` entry
  point documented.
- `website/docs/concepts/models-and-database.md` — migrate flag fix.
- `pkg/nucleus/nucleus.go` — stale package-level godoc comment corrected
  (comment-only change; no symbol added, removed, or renamed — contract
  freeze test verified unaffected).
- `scripts/website/check-coverage.sh` — new portable bash 3.2 drift guard:
  scans for legacy/removed-API tokens, checks dangling `covers:` frontmatter
  refs against the freeze baseline, and validates manifest hygiene. Exits
  non-zero on any finding in `--strict` mode.
- `.github/workflows/ci.yml` — advisory `website-drift` job added; runs
  `check-coverage.sh --strict` on every push. Intentionally NOT a required
  gate (advisory only until manifests exist and the job has proven stable).

**Commit `5a79095` — website-curator subagent + wiring**

- `.claude/agents/website-curator.md` — new subagent owning
  `website/docs/**`, the `covers:`/`config_keys:` frontmatter manifest
  convention, the drift guard script, and the Docusaurus site build.
  Mirrors `examples-maintainer` in ownership model.
- `.claude/agents/doc-updater.md` — scope narrowed to internal docs +
  godoc; public site ownership explicitly handed off to website-curator.
  No overlapping ownership.
- `CLAUDE.md` — §4 iteration loop step 8 is now website-curator
  (renumbered; steps 7–10); §6 subagent index and commands table updated.
- `.claude/commands/iterate.md` and `.claude/commands/sync-docs.md` —
  website-curator wired in alongside doc-updater and examples-maintainer.
- `.claude/settings.local.json` (gitignored, local-only) — owner added a
  permission rule allowing the agent harness to modify `.claude/` config
  files (classifier was blocking self-modification of agent startup config).

### Out

- No runtime behaviour changes.
- No public API symbol additions, removals, or renames.
- No CLI surface changes.
- No CHANGELOG entry; no semver bump.
- `docs/` (root, internal) — not touched. The two-docs-tree rule is
  deliberately enforced: root `docs/` is internal/contract documentation
  (never published); `website/docs/` is the curated public Docusaurus site
  (no sync between the two trees).

## Acceptance criteria — all met

- [x] All six refreshed `website/docs/` pages describe shipped behaviour
  (no v0.9.X placeholders, no "Go REPL" language, correct flags, correct
  API entry points, documented ADR-010 merge engine and `aws-sm:` resolver).
- [x] `pkg/nucleus/nucleus.go` godoc corrected; `go build ./pkg/nucleus/...`
  passes; `bash scripts/ci/check_contract_freeze.sh` passes (no symbol
  change).
- [x] `scripts/website/check-coverage.sh --strict` exits 0: 0 legacy tokens,
  0 dangling `covers:` refs, manifest hygiene clean.
- [x] `npm run build` (Docusaurus) exits 0; no broken links.
- [x] `.github/workflows/ci.yml` is valid YAML; `website-drift` advisory job
  present and uses `check-coverage.sh --strict`.
- [x] `.claude/agents/website-curator.md` exists with clear ownership
  scope (public site, manifests, drift guard, build).
- [x] `doc-updater.md` narrowed to internal docs + godoc; no overlapping
  ownership with website-curator.
- [x] `CLAUDE.md` §4 step 8 = website-curator; §6 index and commands table
  updated.
- [x] `iterate.md` and `sync-docs.md` commands include website-curator.
- [x] No CHANGELOG entry and no semver bump (docs + tooling only, no
  user-facing runtime change).

## Status

### Done (2026-05-21 — two commits on `origin/main`)

All acceptance criteria met. The public site now reflects shipped Nucleus
behaviour. The drift guard is live (advisory CI). The website-curator subagent
is wired into the iteration loop and commands. The two-docs-tree rule is
codified in the subagent definitions and user memory.

Commit `3ca91ce` triggered `docs.yml` → GitHub Pages deploy (deploy workflow
fires on `website/**` push; verify Pages job went green in the Actions tab if
in doubt).

No CHANGELOG entry and no semver bump — deliberate: docs + internal tooling
only, zero user-facing runtime behaviour change.

### Iteration loop

Relevant subagent passes:

- **architect-reviewer** PASS — two-docs-tree rule confirmed correct layering;
  website-curator ownership model mirrors examples-maintainer precedent;
  no ADR needed (tooling-only change).
- **code-reviewer** PASS — `check-coverage.sh` bash 3.2 portable; no runtime
  Go code changed.
- **security-auditor** PASS — no auth/authz/injection surface; advisory CI
  job has no secrets.
- **contract-guardian** PASS — `pkg/nucleus/nucleus.go` change is comment-only;
  freeze test unaffected; no CLI/config/API symbol change.
- **test-runner** PASS — `go build ./pkg/nucleus/...` ok; `check_contract_freeze.sh`
  passes; `npm run build` PASS; `check-coverage.sh --strict` PASS.
- **examples-maintainer** PASS — no public API change; no example update
  needed.
- **doc-updater** PASS — internal docs untouched; scope narrowed correctly.
- **website-curator** PASS — pages faithful to shipped behaviour; drift guard
  clean; manifest hygiene noted as a follow-up (no manifests exist yet).
- **changelog-writer** NO-entry — deliberate; no user-facing change.
- **governance-checker** PASS — advisory CI job strengthens drift governance;
  no SLO/CI-matrix/release-checklist update needed.

### In progress

- (none)

### Blocked

- (none)

## Follow-ups opened by this iteration

1. **Add `covers:`/`config_keys:` frontmatter manifests to the 14
   `website/docs/` pages.** None exist yet; the drift guard's dangling-ref
   check (`check-coverage.sh`) has no signal until manifests are present.
   This is a `website-curator` task — medium effort, enables the guard's
   most useful check.

2. **Extract inline website code examples into `examples/*`.** Once the Phase 4
   reference apps land (ADR-010 Phase 4), pages should import via raw-loader
   instead of inlining code. Ties into ADR-010 Phase 4 / candidate #8 in the
   main queue.

3. **(Optional) Promote the advisory `website-drift` CI job to a required
   gate.** Once manifests exist and the job has proven stable over several
   pushes, flip it from advisory to required. Owner call.

## Two-docs-tree rule (reinforced this iteration)

Root `docs/` is internal/contract documentation; it is never published and has
no sync with the website. `website/docs/` is the curated public Docusaurus
source. The deploy workflow is `docs.yml` (not `deploy.yml`) and triggers only
on `website/**` pushes. This distinction is now codified in:

- `.claude/agents/website-curator.md` (owns `website/docs/**`)
- `.claude/agents/doc-updater.md` (explicitly excludes the public site)
- User memory file `docs_two_trees.md`

## Files of interest

- `.claude/agents/website-curator.md` — new subagent definition.
- `.claude/agents/doc-updater.md` — narrowed scope.
- `CLAUDE.md` — §4 step 8 (website-curator), §6 index/commands table.
- `.claude/commands/iterate.md`, `.claude/commands/sync-docs.md` — updated.
- `scripts/website/check-coverage.sh` — drift guard script.
- `.github/workflows/ci.yml` — advisory `website-drift` job.
- `website/docs/cli/overview.md`, `website/docs/concepts/configuration.md`,
  `website/docs/getting-started/quickstart.md`, `website/docs/features/auth.md`,
  `website/docs/concepts/routing.md`,
  `website/docs/concepts/models-and-database.md` — refreshed pages.
- `pkg/nucleus/nucleus.go` — corrected package-level godoc.

## Notes / decisions log

- 2026-05-21 — Website refresh + drift guard landed as `3ca91ce`; website-
  curator subagent wiring landed as `5a79095`. Both pushed to `origin/main`.
  The permission rule for `.claude/` self-modification lives in the gitignored
  `.claude/settings.local.json` (local-only; not committed). The `website-drift`
  CI job is advisory, not required — captures regressions; omissions are the
  website-curator subagent's job within the loop.
