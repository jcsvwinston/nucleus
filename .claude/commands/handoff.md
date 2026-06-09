---
description: Run the Session End Protocol — update state files, archive a completed iteration, then commit and PR the state changes (no tags/releases).
---

Run the **Session End Protocol** from `CLAUDE.md` §5 before the user closes the session or before a long pause.

## Steps

1. **Verify the working tree is describable.** Run `git status --short`. If there are scattered uncommitted changes that you cannot summarise in one paragraph, surface them to the user and ask whether to (a) commit them as part of this session, (b) stash them and document the stash in `HANDOFF.md`, or (c) abort the handoff and let the user clean up first.

2. **Update `.claude/state/CURRENT_ITERATION.md`** with:
   - What is **done** this session (with PRs, commits, or file references).
   - What is **in progress** (with the next concrete step).
   - What is **blocked** and why (with the blocker's owner if known).

   Delegate the formatting to the `session-curator` subagent — it owns the canonical structure of state files.

3. **Overwrite `.claude/state/HANDOFF.md`** with a machine-friendly note containing:
   - `ITERATION:` title and status.
   - `BRANCH:` and `LAST COMMIT:` hash + message.
   - `STATUS:` one-line summary.
   - `NEXT STEP:` the concrete first action for the next session.
   - `BLOCKERS:` list, or `none`.
   - `FILES OF INTEREST:` paths the next session should open first.
   - `NOTES:` anything else worth surfacing.

4. **If the iteration is complete**, archive a copy:
   - Copy `CURRENT_ITERATION.md` to `docs/iterations/YYYY-MM-DD-<slug>.md` (translate any relative dates to absolute dates).
   - Reset `CURRENT_ITERATION.md` to the empty template at `.claude/state/templates/CURRENT_ITERATION.template.md` if it exists; otherwise to a minimal "awaiting owner direction" stub modelled on the template.

5. **Write a commit message** in conventional-commits style (e.g. `chore(state): archive <slug> iteration + reset CURRENT_ITERATION`). If the change is user-visible, also add a one-line `CHANGELOG.md` entry under `Unreleased` (state-file-only handoffs are not user-visible and need no CHANGELOG entry).

6. **Integrate the state changes.** Following the PR-only workflow for `main`, create a `chore/state-*` branch, stage **only** the handoff's own files (`.claude/state/*` and the new `docs/iterations/*` archive — plus any in-scope `.claude/` process edit this handoff makes), commit with the step-5 message, push, open a PR with `gh pr create`, wait for CI to go green, then `gh pr merge --squash --delete-branch`. Report the PR link and the final commit hash. Do NOT sweep unrelated uncommitted work into this PR — surface it per step 1 instead.

## Hard rules

- **Do not delete** completed iteration files — archive them. The chronological log under `docs/iterations/` is part of the project's history.
- **Do not modify** `docs/iterations/*` except to add a new archived file. Existing entries are immutable history.
- **You may commit, push, and merge a PR** for the prepared state changes, following the repo's PR-only workflow for `main` (branch → push → `gh pr create` → wait for CI green → `gh pr merge --squash --delete-branch`). Keep that PR scoped to the handoff's own state/iteration/process files.
- **Never auto-tag or auto-release.** Creating git tags and cutting releases stay with the human — that is `/release-prep`'s domain, not the handoff's.
