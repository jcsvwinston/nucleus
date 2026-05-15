---
description: Sync internal docs, website docs, examples and godoc with current shipped behaviour. Runs doc-updater and examples-maintainer.
argument-hint: optional scope (e.g. "pkg/auth" or "docs/guides/AUTH_GUIDE.md")
---

Run a **docs-only** synchronisation pass. This does not run tests, does not review code, does not check contracts. It only aligns documentation with the implementation as shipped today.

## Scope

- If `$ARGUMENTS` is provided, focus the sync on that scope.
- Otherwise, infer from `git status --short` and `git diff --name-only HEAD` what surface changed and sync the docs that cover it.

## Steps

1. **Examples first.** Delegate to `examples-maintainer` to ensure `examples/*` reflects the current public API. Any example whose code no longer compiles against the current `pkg/*` is updated. The examples are the canonical source for code blocks in website docs.

2. **Docs sync.** Delegate to `doc-updater` to align (in this order):
   - `pkg/**/*.go` godoc on exported symbols.
   - `README.md` and `docs/QUICKSTART.md`.
   - `docs/guides/*` and `docs/reference/*`.
   - `website/docs/**/*.md(x)` — use the frontmatter `covers:` and `config_keys:` manifest to reverse-lookup affected pages.
   - Verify code-block imports in `website/docs/*` still resolve to existing files under `examples/*`.

3. **Coverage check.** If `scripts/website/check-coverage.sh` exists, run it and surface findings:
   - Undocumented public symbols (exported in `pkg/*` but with zero `covers:` entries across `website/docs/*`).
   - Dangling references (`covers:` entries pointing at symbols that no longer exist).
   - Inline code blocks that should be imported from `examples/*` instead.

4. **Report.** Produce a consolidated diff with one section per area touched. Ask the user whether to commit as a single `docs:` commit or to split per area (e.g. one commit for godoc + reference, one for website).

## What this command does NOT do

- It does not touch `docs/governance/*` without explicit user approval (the `doc-updater` subagent enforces this).
- It does not run `architect-reviewer`, `code-reviewer` or `security-auditor` — those belong to `/iterate` or `/review`.
- It does not run tests. If a doc change implies the code is wrong, surface that as a finding rather than fixing the code here.
