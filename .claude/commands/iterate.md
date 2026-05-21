---
description: Run the full iteration loop on the current change set — architect → code → security → contract → tests → examples → docs → website → changelog → governance.
argument-hint: optional scope filter (e.g. "pkg/auth" or "files changed since HEAD~1")
---

Run the **Iteration Loop** from `CLAUDE.md` §4 on the current change set.

## Scope

- If `$ARGUMENTS` is provided, use it as the scope.
- Otherwise, infer the scope from `git status --short` (uncommitted) and `git diff --name-only HEAD` (since last commit). State the inferred scope in your first message before delegating to any subagent.

## Steps

Delegate to each subagent **in order** via the Task tool. **Stop the loop** on any blocker and surface the agent's full report to the user before continuing.

1. **`architect-reviewer`** — Is the change consistent with `SPEC.md` and the relevant ADRs? Layering, extension points, public-surface implications.
2. **`code-reviewer`** — Go-idiomatic review: error handling, concurrency, allocations, edge cases, race / N+1 / nil-deref risks.
3. **`security-auditor`** — AuthN/Z, input validation, secrets, SQL/template injection, CSRF/CORS, secure defaults. *Skip only for pure docs/tests changes.*
4. **`contract-guardian`** — Did we mutate a stable API/CLI/config surface? If yes, freeze tests pass + deprecation path documented. *Skip only when no files under `pkg/`, `internal/cli/`, `contracts/`, or the schema of `nucleus.yml` were touched.*
5. **`test-runner`** — `go test ./...` with appropriate `-run` filters; add `-race` when concurrent code changed; compatibility harness on contract-touching changes.
6. **`examples-maintainer`** — Reflect public-API changes in `examples/*`. *Mandatory whenever public behaviour changes.*
7. **`doc-updater`** — Internal docs (`docs/*`), godoc, README, QUICKSTART. *Mandatory whenever public behaviour changes.*
8. **`website-curator`** — Public Docusaurus site (`website/docs/*`): keep it a faithful reflection of shipped behaviour, run `scripts/website/check-coverage.sh`, validate the build. *Mandatory whenever a reader-visible surface (public API, CLI, config keys, defaults, headline features) changes.*
9. **`changelog-writer`** — `CHANGELOG.md` under `Unreleased`; propose semver bump hint.
10. **`governance-checker`** — Light-touch cross-check of SLO / CI matrix / release checklist consistency. *Full-strength variant is reserved for `/release-prep`.*

## After the loop

When the loop completes without blockers:

- Update `.claude/state/CURRENT_ITERATION.md` with what is **done**, what is **in progress**, what is **blocked**.
- Propose a commit message in conventional-commits style (e.g. `feat(auth): rotate JWT signing key with config opt-in`).
- Offer to run `/handoff` if the iteration is complete.

## When a blocker fires

Stop the loop. Show the subagent's report verbatim. Ask the user how to proceed (fix and re-run from this step, skip with justification, or abort). Do not silently move past blockers — every one is the system telling you something.
