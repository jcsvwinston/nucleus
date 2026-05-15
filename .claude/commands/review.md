---
description: Read-only review pass — architect-reviewer + code-reviewer + security-auditor. No edits, just findings.
argument-hint: optional file or scope to focus on (e.g. "pkg/storage" or "PR #57")
---

Run a **read-only** review pass on the current change set or on the scope provided in `$ARGUMENTS`.

This is **not** an iteration. **Do not edit any file**, do not run tests, do not regenerate docs. Only invoke the review subagents and consolidate their findings.

## Scope

- If `$ARGUMENTS` is provided, focus all three reviewers on that scope (file, package, PR number, or symbol).
- Otherwise, infer from `git status --short` and `git diff --name-only HEAD`.
- Announce the chosen scope before delegating.

## Delegation

Run the three reviewers **in parallel** via the Task tool:

1. **`architect-reviewer`** — `SPEC.md` and ADR consistency, layering, extension points, public-surface implications.
2. **`code-reviewer`** — Go quality, error handling, concurrency, allocations, edge cases, nil-safety, idiomatic style.
3. **`security-auditor`** — AuthN/Z, input validation, secrets handling, injection vectors (SQL, template, command), CSRF/CORS, transport security, secure defaults.

## Consolidated output

Produce a single deliverable with:

1. **Executive summary** at the top: `GREEN` / `AMBER` / `RED`, plus the single biggest issue if not green.
2. **Findings**, severity-ordered (`Critical` → `High` → `Medium` → `Low` → `Nit`), one line per finding, with the originating agent in brackets:

   ```
   [security-auditor] Critical: JWT verification accepts unsigned tokens when key is empty (pkg/auth/jwt.go:142)
   [architect-reviewer] High: new direct dep on pkg/internal/store violates layering (pkg/router/cache.go:8)
   [code-reviewer] Medium: handler ignores ctx cancellation in long loop (pkg/admin/export.go:201)
   ```

3. **No editing recommendations are auto-applied.** The user decides what to act on. Suggest which command to follow up with: `/iterate` to fix and re-validate, or specific subagent delegation for narrow follow-ups.

## What this command does NOT do

- It does not write files.
- It does not run tests (use `test-runner` directly or `/iterate` if you need that).
- It does not update docs (use `/sync-docs` for that).
- It does not check the freeze tests or contracts (use `contract-guardian` directly or `/iterate` for that).
