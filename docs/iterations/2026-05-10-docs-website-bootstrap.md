# Iteration — Docs / Website Bootstrap

> Archived: 2026-05-10
> Branch: codex/framework-hardening
> Status: COMPLETE (all code-side acceptance criteria met)

---

## Goal

Bootstrap the public documentation + landing site for Nucleus under `website/`,
built with Docusaurus 3 (TypeScript) and published to GitHub Pages at
`https://jcsvwinston.github.io/nucleus/`. The iteration does not migrate content
from the authoritative `docs/` tree; it seeds a full landing page + stub docs
tree to validate the publish pipeline end-to-end.

---

## Scope

### In
- `website/` — Docusaurus 3 Classic, TypeScript, custom theme (Inter / JetBrains
  Mono, teal/navy palette, Nucleus SVG logo).
- Landing page (`website/src/pages/index.tsx`): hero with eyebrow + gradient
  title + terminal install snippet, 6 feature cards, code showcase, 8-subsystem
  grid, final CTA. CSS in `website/src/css/custom.css` +
  `website/src/pages/index.module.css`.
- Docs tree under `website/docs/`: `intro.md` + Getting started (installation,
  quickstart, project-structure) + Concepts (application, configuration,
  routing, models-and-database) + Features (admin, auth, observability,
  storage-and-tasks) + Architecture (principles, compatibility) + CLI
  (overview), each directory with `_category_.json` sidebar metadata.
- `.github/workflows/docs.yml` — build-only on PRs; build + deploy to GitHub
  Pages on push to `main`; path-scoped to `website/**`; `cancel-in-progress:
  true`; least-privilege permissions (`pages: write`, `id-token: write`).
- `website/package.json` + `website/package-lock.json` — committed for `npm ci`
  reproducibility.
- `website/README.md` — local dev / build / deployment notes.
- `CHANGELOG.md` — Unreleased entry under Added, cross-referenced to ADR-003.
- `docs/governance/CI_MATRIX.md` — "Non-framework lanes" section added documenting
  `docs.yml` as non-blocking; reference date bumped to 2026-05-10.
- `.claude/launch.json` — preview config (added during visual inspection).

### Out
- Migration of `docs/guides/*` or `docs/reference/*` (deferred to a future
  iteration).
- Custom search (Algolia), versioning, i18n.
- Any changes to `pkg/`, `internal/`, `examples/`, `contracts/`.

---

## Acceptance criteria

- [x] `website/` builds locally with `npm ci && npm run build`.
- [x] `docusaurus.config.ts` targets `https://jcsvwinston.github.io/nucleus/`
      with `baseUrl: '/nucleus/'`.
- [x] `.github/workflows/docs.yml` in place with correct build + deploy steps.
- [ ] First deploy reachable at `https://jcsvwinston.github.io/nucleus/` —
      **operational blocker only**: requires owner to enable
      `Settings → Pages → Source: GitHub Actions` once. Not a code gap.
- [x] No files under `pkg/`, `internal/`, `examples/`, or `contracts/` were
      modified. `docs/governance/CI_MATRIX.md` updated only with the governance
      note for the new CI lane (in scope).
- [x] CHANGELOG entry under Unreleased added.

---

## Subagent verdicts (2026-05-10)

- **architect-reviewer**: WARN — flagged Nucleus-vs-GoFrame naming tension
  (site uses post-rename identity per ADR-003 while code rename is still
  pending). User has explicitly chosen this direction. Factual errors fixed
  (`/readyz` removed from landing + observability page; storage YAML annotated
  as illustrative, pointing to CONFIG_KEY_REGISTRY.md).
- **security-auditor**: WARN (accepted risks) — `serialize-javascript`
  build-time advisory chain (upstream Docusaurus issue, not resolvable without
  --force); Google Fonts third-party CSP/privacy trade-off (accepted pre-1.0).
  All other items PASS: workflow least-privilege permissions, no XSS, `npm ci`
  with lockfile integrity, no secrets, `target=_blank` rel hygiene.
  Concurrency tightened to `cancel-in-progress: true`.
- **governance-checker**: PASS with advisories — all applied (CI_MATRIX note +
  reference-date bump; CHANGELOG cross-ref to ADR-003).

---

## Deferred follow-ups

- **ADR-004** — document the `website/` vs `docs/` relationship and the
  aspirational-docs policy during the pre-rename / pre-1.0 window.
- **SHA-pin** the four `actions/*` references in `.github/workflows/docs.yml`
  (currently on `@v4` / `@v3` tags).
- **Self-host fonts** — move Inter + JetBrains Mono under `website/static/fonts/`
  and remove the Google Fonts `@import` to eliminate the third-party network
  dependency and satisfy a strict CSP.
- **Upstream tracking** — open or watch the `serialize-javascript` advisory in
  the Docusaurus repository.

---

## Notes / decisions log

- 2026-05-10 — `website/` chosen over `docs/` to avoid perturbing the
  authoritative docs tree. Docusaurus content will be promoted incrementally.
- 2026-05-10 — TypeScript chosen for the Docusaurus config (user preference).
- 2026-05-10 — GitHub Pages deploy uses the modern Actions-based flow
  (`actions/upload-pages-artifact` + `actions/deploy-pages`), not the legacy
  `gh-pages` branch. No extra branch to maintain.
- 2026-05-10 — Visual inspection via Claude Preview confirmed: landing page
  renders with hero + features grid; concepts page renders with sidebar +
  breadcrumb + TOC.
- 2026-05-10 — `npm run build` clean; `go test ./...` green (no Go changed).
