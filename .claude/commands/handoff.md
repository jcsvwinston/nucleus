---
description: Run the Session End Protocol — persist iteration state and a clean handoff for the next session.
argument-hint: (no arguments)
---

You are closing the current Claude Code session in the Nucleus /
GoFrame repository. Execute the **Session End Protocol** in
`CLAUDE.md` §5.

Steps:

1. Delegate to the `session-curator` subagent with this prompt:

   > Mode B — Session End. Reconcile `git status` with the active
   > iteration. Update `.claude/state/CURRENT_ITERATION.md` so its
   > Done / In progress / Blocked sections reflect the current state.
   > Overwrite `.claude/state/HANDOFF.md` with a fresh briefing block.
   > If the iteration's acceptance criteria are all met, archive it to
   > `docs/iterations/YYYY-MM-DD-<slug>.md` (use absolute date) and
   > replace `CURRENT_ITERATION.md` with the empty template. Suggest a
   > commit message and a CHANGELOG line if user-facing behaviour
   > changed.

2. Print the curator's `HANDOFF SUMMARY` block to the user.

3. Suggest (do not execute) the commit command and tag/push if
   applicable.

Do **not** modify code files in this command. State files only.
