---
description: Refresh docs and examples to match shipped behaviour. Use after changes to public API, CLI, config keys, or defaults that have already passed code review.
argument-hint: (no arguments)
---

Synchronize documentation and examples with the current implementation.

Steps:

1. Identify the changed public surface using `git diff --stat` and
   filter by:
   - `pkg/**/*.go` exported symbols,
   - `internal/cli/**/*.go` flags and command shapes,
   - `goframe.yaml` schema (config keys).

2. Delegate to `examples-maintainer` to update `examples/*` accordingly.
   Capture its report.

3. Delegate to `doc-updater` to update `README.md`, `docs/QUICKSTART.md`,
   relevant `docs/guides/*`, `docs/reference/*`, and godoc comments.
   Capture its report.

4. Delegate to `changelog-writer` to add the appropriate entries under
   `## [Unreleased]`.

5. Final synthesis:

   ```
   ## Sync Docs Summary
   - examples-maintainer  : UPDATED | NO_CHANGE_NEEDED | BLOCKED
   - doc-updater          : UPDATED | NO_CHANGE_NEEDED | BLOCKED
   - changelog-writer     : added <N> entries (semver: patch|minor|major)

   Files written: <count>
   Suggested commit message: …
   ```

Do not run the test suite here — `/iterate` covers that. This command is
deliberately scoped to docs hygiene.
