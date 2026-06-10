# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    v0.9.0 release cut — COMPLETE (archived to
              docs/iterations/2026-06-09-cut-v0.9.0.md).
              No active iteration. CURRENT_ITERATION.md is EMPTY (empty template).
BRANCH:       main (no open feature branch; all work landed via PRs)
LAST COMMIT:  f546f52  chore(cli): pin scaffold to the published v0.9.0 release (#110)
STATUS:       done — v0.9.0 tagged + published (latest); scaffold pins v0.9.0;
              main clean & green; real-user-path smoke verified end-to-end
              against the published module. State-file changes from THIS
              handoff still need a branch → commit → PR (main is PR-only).
NEXT STEP:    Open a branch for these state-file changes, push, create PR,
              merge; then begin the strategic prototype phase: prototype #1
              pinned to v0.9.0 — distilled prototypes become ADR-010 Phase 4
              Slice 2/3 reference apps + harness fixture profiles (v0.9.X range).
BLOCKERS:     none
FILES OF INTEREST:
              CHANGELOG.md ([0.9.0] section + fresh [Unreleased] pin entry),
              internal/cli/new.go (defaultPinnedFrameworkVersion=v0.9.0),
              docs/reports/compatibility_report_2026-06-09.md,
              docs/reports/dependency_impact_2026-06-09.md,
              docs/iterations/2026-06-09-cut-v0.9.0.md
NOTES:        Prototype-phase cautions:
              - Module.Jobs / Module.Webhooks are reserved shape (boot-WARN,
                not executed); use pkg/tasks directly for background jobs.
              - outbox.NewKafkaBridge is deliberately unfinished.
              F-13 (P3): CLAUDE.md §directory-map still says cmd/goframe/ —
              fix opportunistically in any docs PR.
              Deletable untracked artifact: docs/audits/2026-06-07-exhaustive-audit.md
              (superseded draft, not in git; delete at maintainer's discretion).
              ADR-010 Phase 4 Slices 2/3 target the v0.9.X range (decision
              recorded: cut v0.9.0 now, slices land during v0.9.X).
              Prior handoff stale claim ("v0.8.0 parked") corrected: v0.8.0
              was tagged 2026-05-28; this session cut v0.9.0.

--- OPEN STRATEGIC ITEMS ---

1. Prototype phase — prototype #1 pinned to v0.9.0 per agreed plan.
   ADR-010 Phase 4 Slices 2/3 return in v0.9.X.

2. F-13 (P3, non-blocking) — CLAUDE.md §directory-map says cmd/goframe/;
   actual entry-point is cmd/nucleus/. Fix opportunistically.

3. P2 framework bug — Router.Resource("") under a module Prefix panics at
   startup. pkg/nucleus/router.go joinPath should yield "/" not "" when
   prefix + path are both empty.

--- DELETABLE ARTIFACT ---

docs/audits/2026-06-07-exhaustive-audit.md — superseded untracked draft.
Harmless; delete at maintainer's discretion (untracked, not in git).

*** CRITICAL — `main` is PR-only for EVERYONE including the maintainer ***
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-09
