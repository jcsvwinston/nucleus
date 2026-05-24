---
description: Sync internal docs, the public website, examples and godoc with current shipped behaviour. Runs doc-updater, website-curator and examples-maintainer.
argument-hint: optional scope (e.g. "pkg/auth" or "docs/guides/AUTH_GUIDE.md")
---

Run a **docs-only** synchronisation pass. This does not run tests, does not review code, does not check contracts. It only aligns documentation with the implementation as shipped today.

## Scope

- If `$ARGUMENTS` is provided, focus the sync on that scope.
- Otherwise, infer from `git status --short` and `git diff --name-only HEAD` what surface changed and sync the docs that cover it.

## Steps

1. **Examples first.** Delegate to `examples-maintainer` to ensure `examples/*` reflects the current public API. Any example whose code no longer compiles against the current `pkg/*` is updated. The examples are the canonical source for code blocks in website docs.

2. **Internal docs sync.** Delegate to `doc-updater` to align (in this order):
   - `pkg/**/*.go` godoc on exported symbols.
   - `README.md` and `docs/QUICKSTART.md`.
   - `docs/guides/*` and `docs/reference/*`.

3. **Public website sync.** Delegate to `website-curator` to align the public
   Docusaurus site (`website/docs/**`) with shipped behaviour:
   - Update affected pages and their `covers:` / `config_keys:` manifests.
   - Run `scripts/website/check-coverage.sh` and surface findings (legacy/
     removed-API tokens, dangling `covers:` refs, pages missing a manifest).
   - Validate the site builds (`cd website && npm run build`).

4. **Body-content verification (mandatory).** After steps 2 and 3 have
   produced their drafts, delegate to `docs-content-verifier` with the
   full list of pages touched (internal + public). It validates every
   Go symbol in `go` code blocks against
   `contracts/baseline/api_exported_symbols.txt`, every YAML/TOML key
   in config code blocks against `docs/reference/CONFIG_KEY_REGISTRY.md`,
   and every `Go 1.XX` mention against `go.mod`. If it returns `FAIL`,
   stop and route fixes back to `doc-updater` / `website-curator`
   before continuing. This is the post-2026-05-24-audit checkpoint
   that prevents body-content drift (CLAUDE.md §9).

5. **Report.** Produce a consolidated diff with one section per area touched. Ask the user whether to commit as a single `docs:` commit or to split per area (e.g. one commit for godoc + reference, one for website).

## What this command does NOT do

- It does not touch `docs/governance/*` without explicit user approval (the `doc-updater` subagent enforces this).
- It does not run `architect-reviewer`, `code-reviewer` or `security-auditor` — those belong to `/iterate` or `/review`.
- It does not run tests. If a doc change implies the code is wrong, surface that as a finding rather than fixing the code here.
