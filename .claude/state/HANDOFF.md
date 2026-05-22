# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Website coverage manifests — COMPLETE (committed + archived). No active iteration.
BRANCH:       main (clean after the close commit).
LAST COMMIT:  bbc7d60 docs(website): add covers/config_keys manifests to docs pages (feature) + the follow-up chore(state) close commit.
STATUS:       Added covers:/config_keys: frontmatter to the 14 website/docs pages that lacked one (frontmatter only, no body edits), via the website-curator subagent. covers: lists only baseline-stable symbols (the drift guard's check #2 validates every pkg/<pkg>.<Symbol> body token against contracts/baseline/api_exported_symbols.txt), so experimental/transitional surfaces (observability, openapi, admin, outbox, providers) are deliberately excluded. config_keys: kept honest against CONFIG_KEY_REGISTRY.md (not guard-validated). Verified independently of the subagent: check-coverage.sh --strict = 0/0/0, frontmatter-only diff across exactly 14 pages, covers/config-keys spot-checked vs baseline+registry, npm run build clean. Website-only — no CHANGELOG/contracts/code change.
NEXT STEP:    This session's two commits were pushed to origin/main. Owner picks the next iteration. Top picks: (a) Oracle model-scaffold identifier-casing (PR #78 follow-up — quoted-lowercase vs unquoted-uppercase; likely an ADR; unblocks the deferred Oracle AutoMigrate_Exploratory CI lane); (b) ADR-010 Phase 3 — /_/config + nucleus config print --effective (needs per-key source tracking); (c) optional: promote the advisory website-drift CI job to a required gate now that manifests give the dangling-ref check steady signal.
BLOCKERS:     none.
FILES OF INTEREST:
  - website/docs/**/*.md — 14 pages now carry covers:/config_keys: frontmatter; concepts/models-and-database.md was the pre-existing format template.
  - scripts/website/check-coverage.sh — the drift guard; check #2 (dangling covers) scans full page body for pkg/<pkg>.<Symbol> and validates vs the freeze baseline; run with --strict to fail on drift.
  - docs/iterations/2026-05-22-website-coverage-manifests.md — this iteration's archive.

NOTES:
  - FOUR iterations landed this session: 6e6a075 (shared registry), 1233bf4 (nested coverage), 9227e7d (observability lifecycle), bbc7d60 (website manifests). The contract-registry + public-docs coverage arc is now complete: every public pkg/* package has an inventory-backed lifecycle, scanner coverage is machine-visible, and every public docs page declares the stable symbols it documents.
  - Drift-guard reminder: covers: entries must be STABLE (in the freeze baseline) or `check-coverage.sh --strict` fails. When a stable symbol is removed/renamed, the guard flags the documenting page as DANGLING — reconcile via the website-curator subagent.
  - Carry-forward follow-ups (low priority): relocate pkg/observability to internal/ post-v1.0 (don't promote to stable); discoverPublicPackages double-reads each dir; the dead *ast.InterfaceType unexported-skip branch in checkTypeSpecForLeaks.

Updated: 2026-05-22
