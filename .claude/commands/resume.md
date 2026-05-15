---
description: Run the Session Start Protocol — read state, brief the user, confirm direction before any work.
---

You are about to start a working session on the Nucleus / GoFrame repository. Execute the **Session Start Protocol** from `CLAUDE.md` §2 before doing any work.

## Steps

1. **Read session state**, in this order. Any file may be missing on a fresh clone — that is fine, note it and continue:
   - `.claude/state/HANDOFF.md` — the previous session's closing notes.
   - `.claude/state/CURRENT_ITERATION.md` — the active iteration goal, scope, acceptance criteria, blockers.
   - The most recent file under `docs/iterations/` for additional context (use `ls -lt docs/iterations/ | head` to find it).

2. **Inspect the working tree**:
   - `git status --short` — uncommitted changes.
   - `git log --oneline -n 10` — recent commits.
   - `git branch --show-current` — release branches (e.g. `codex/v0.6.0-roadmap`) carry extra constraints.

3. **Reconcile** the user's current message with the state files:
   - If the message **extends or modifies** the active iteration, proceed.
   - If it **starts a new iteration**, ask whether to archive the current `CURRENT_ITERATION.md` into `docs/iterations/YYYY-MM-DD-<slug>.md` and replace it with a fresh one.
   - If it **conflicts silently** with the handoff (e.g. the user says "continue" but the handoff is empty), surface the gap and ask before guessing.

4. **Brief the user** in one paragraph before doing any work. Cover:
   - What was open at the end of the previous session.
   - What you understand the next step to be.
   - What is blocked, if anything.

   Do this even if the user did not explicitly ask for a briefing — it gives them a chance to redirect before you commit time to the wrong thing.

## Delegation

For non-trivial state synthesis, delegate the briefing to the `session-curator` subagent via the Task tool. `session-curator` owns the format of `.claude/state/*` files and produces the canonical paragraph.

## What this command does NOT do

- It does not run tests, reviewers, or doc updates. Those belong to `/iterate`, `/review`, or `/sync-docs`.
- It does not modify code. It is a read-only orientation pass.
- It does not close the previous iteration. That belongs to `/handoff`.
