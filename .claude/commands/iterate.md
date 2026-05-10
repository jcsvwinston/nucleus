---
description: Run the full iteration loop on the current change set — implement, then sequentially delegate to the review/test/docs subagents per CLAUDE.md §4.
argument-hint: [optional short description of the change]
---

You are running the **Iteration Loop** defined in `CLAUDE.md` §4 for the
Nucleus / GoFrame repository.

Pre-conditions:

- The user has committed (or is about to commit) a slice of work.
- `.claude/state/CURRENT_ITERATION.md` describes the iteration goal.

Steps (in order, stop on the first FAIL/CHANGES_REQUESTED unless the
user explicitly says "continue"):

1. **Diff scoping** — run `git diff --stat` against the iteration's
   starting point. Decide which subagents are required using the matrix
   in `CLAUDE.md` §4.

2. **Architect review** — delegate to `architect-reviewer`. If FAIL,
   stop and surface the report.

3. **Code review** — delegate to `code-reviewer`. If
   `CHANGES_REQUESTED`, stop and surface the report.

4. **Security audit** — delegate to `security-auditor` unless the diff
   is purely docs/tests. Any [HIGH] finding stops the loop.

5. **Contract guardian** — delegate to `contract-guardian` if files
   under `pkg/`, `internal/cli/`, `contracts/`, or the config schema
   changed. If a deliberate breaking change, also delegate to
   `migration-assistant`.

6. **Dependency impact** — delegate to `dependency-impact` if `go.mod`
   or `go.sum` changed.

7. **Tests** — delegate to `test-runner`. Always required.

8. **Examples** — delegate to `examples-maintainer` if any public
   surface changed.

9. **Docs** — delegate to `doc-updater` if any user-facing behaviour or
   default changed.

10. **Performance** — delegate to `performance-bench` if a hot path was
    touched (router, db, model, admin, JSON helpers).

11. **Changelog** — delegate to `changelog-writer` if user-facing
    behaviour changed.

12. **Governance (light)** — delegate to `governance-checker` in
    light mode. Promote to release-prep mode only via `/release-prep`.

13. **State update** — delegate to `session-curator` (Mode B) to
    refresh `.claude/state/CURRENT_ITERATION.md` with the new "Done /
    In progress / Blocked" status. Do **not** write `HANDOFF.md` here —
    that belongs to `/handoff`.

Final output:

- A short summary of each subagent's verdict (one line each).
- Any FAIL/WARN that requires user action.
- A proposed commit message and (if applicable) CHANGELOG line.

If $ARGUMENTS is provided, treat it as the iteration's short
description and pass it to the subagents that need it.
