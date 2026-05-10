---
description: Run the Session Start Protocol — read state, inspect git, brief the user before doing any work.
argument-hint: (no arguments)
---

You are starting (or restarting) a Claude Code session in the Nucleus /
GoFrame repository. Execute the **Session Start Protocol** defined in
`CLAUDE.md` §2.

Steps:

1. Delegate to the `session-curator` subagent with this prompt:

   > Mode A — Session Start. Read `.claude/state/HANDOFF.md`,
   > `.claude/state/CURRENT_ITERATION.md`, and the most recent file under
   > `docs/iterations/` if any. Run `git status --short`,
   > `git log --oneline -n 10`, and `git branch --show-current`. Return
   > the BRIEFING block exactly as specified in your agent definition.

2. Print the BRIEFING block to the user verbatim.

3. After the briefing, ask **one** focused question only if the user's
   intent is ambiguous. Otherwise wait for the user to confirm or
   redirect before doing any work.

Do **not** start coding, modifying, or running tests until the user has
acknowledged the briefing.
