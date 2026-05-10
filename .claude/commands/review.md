---
description: Read-only review pass — architect + code + security. No tests, no doc edits, no commits. Use when you want a quality check without rerunning the full iteration loop.
argument-hint: [optional path or symbol to focus on]
---

Run a **read-only review** of the current change set. No subagent in
this command is allowed to modify files.

Steps:

1. Run `git diff --stat` against `main` (or against the starting point
   of the active iteration in `.claude/state/CURRENT_ITERATION.md`).
   If $ARGUMENTS is provided, narrow the diff to that path or symbol.

2. Delegate to `architect-reviewer`. Capture the report.

3. Delegate to `code-reviewer`. Capture the report.

4. Delegate to `security-auditor`. Capture the report.

5. Synthesize the three reports into a single review block:

   ```
   ## Combined Review

   **Verdicts:**
   - architect-reviewer : PASS | WARN | FAIL
   - code-reviewer      : PASS | NITS | CHANGES_REQUESTED
   - security-auditor   : PASS | WARN | FAIL

   ### Top blockers (≤ 5)
   1. …

   ### Recommended follow-ups (≤ 5)
   1. …
   ```

Do **not** run tests, edit docs, or touch state files. This command is
purely advisory.
