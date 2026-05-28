# Iteration: Enable branch protection on `main`

> Archived: 2026-05-28.
> Status: COMPLETE — all acceptance criteria met.

## Goal

Apply Profile A branch protection to `main` so that red CI can no longer be
pushed directly and the solo-maintainer PR flow is validated end-to-end.

## Scope

1. Run `scripts/ci/configure_branch_protection.sh` against
   `jcsvwinston/nucleus` / `main` with `--approvals 0`.
2. Verify the applied settings via `gh api .../branches/main/protection`.
3. Document the applied status and corrected apply command in
   `docs/governance/CI_MATRIX.md`.
4. Demonstrate the solo-PR flow end-to-end with a real PR (PR #79).

## Acceptance criteria

- [x] `enforce_admins: true` confirmed via GitHub API.
- [x] Required status check `CI Required Gate` with `strict: true` confirmed
  via GitHub API.
- [x] `required_approving_review_count: 0` confirmed (deliberate — single-
  maintainer repo; 1 would lock the sole maintainer out with enforce_admins
  enabled, since no second reviewer exists).
- [x] `required_conversation_resolution: true` confirmed via GitHub API.
- [x] `main` previously had NO protection (HTTP 404 on the protection endpoint
  before this iteration).
- [x] Solo-PR flow demonstrated: PR #79 carried the CI_MATRIX doc update;
  CI Required Gate went green on all 10 lanes; self-merged (squash) with
  0 approvals as `a79c413`. Maintainer path confirmed NOT locked.
- [x] Direct `git push origin main` is now REJECTED by GitHub (branch
  protection active).

## What was done

### Branch protection applied

Command run:

```
bash scripts/ci/configure_branch_protection.sh \
  --repo jcsvwinston/nucleus \
  --branch main \
  --approvals 0
```

Settings verified via `gh api repos/jcsvwinston/nucleus/branches/main/protection`:

| Setting                             | Value            |
|-------------------------------------|------------------|
| `enforce_admins`                    | `true`           |
| Required status check               | `CI Required Gate` |
| `strict` (branch must be up-to-date)| `true`           |
| `required_approving_review_count`   | `0`              |
| `required_conversation_resolution`  | `true`           |

`--approvals 0` is deliberate: with `enforce_admins: true` and a single
maintainer, setting approvals to 1 would make merging impossible (no second
reviewer). Zero approvals lets the maintainer self-merge once the required
status check is green.

### CI_MATRIX.md updated

`docs/governance/CI_MATRIX.md` was updated to record the APPLIED status and
the corrected apply command (the script invocation above). Previously the
document described protection as planned/pending.

### End-to-end PR flow demonstrated (PR #79)

PR #79 carried the CI_MATRIX documentation update and the v0.8.0 state
archive. All 10 CI lanes (including live MSSQL and Oracle) passed the
`CI Required Gate`. The PR was self-merged (squash) with 0 approvals,
producing commit `a79c413` on `main`. This proved:

- The maintainer can land work without a second reviewer.
- The gate blocks a merge when CI is red (only green merges reach `main`).
- The governance gap (main was red 2026-05-24 → 2026-05-27 due to direct
  pushes bypassing the gate) is now closed.

## Key consequence for future sessions

**`main` is PR-only for everyone, including the maintainer.** Every
change — including `.claude/state/*` and `docs/*` — must follow this
workflow:

1. Create a feature branch off `main`.
2. Push the branch to `origin`.
3. `gh pr create`.
4. Wait for `CI Required Gate` to go green (~7–20 min; the full matrix
   including live MSSQL/Oracle runs on every PR; GitHub cannot path-exclude
   required checks).
5. Self-merge: `gh pr merge --squash --delete-branch`.
6. `git checkout main && git pull`.

Direct `git push origin main` is now REJECTED.

## Files of interest

- `docs/governance/CI_MATRIX.md` — documents the applied settings.
- `scripts/ci/configure_branch_protection.sh` — the script that was run.

## Notes / decisions log

- 2026-05-28 — `--approvals 0` chosen deliberately; see rationale above.
- 2026-05-28 — PR #79 squash-merged as `a79c413`; origin/main HEAD is now
  `a79c413`.
- 2026-05-28 — This closes the long-standing governance gap where main could
  receive direct pushes regardless of CI state.
