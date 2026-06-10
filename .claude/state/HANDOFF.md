# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    none active — v0.9.0 release cut archived 2026-06-09
              (docs/iterations/2026-06-09-cut-v0.9.0.md).
              This session (2026-06-10) was orientation-only; no code
              changes, no PRs, no active iteration.
BRANCH:       main (no open feature branch; all work landed via PRs)
LAST COMMIT:  7cb3f45  chore(state): handoff after v0.9.0 release (#109–#110) (#111)
STATUS:       done — v0.9.0 tagged + published (latest); scaffold pins v0.9.0;
              main clean & green; state fully integrated (#111 merged after
              a Docker Hub pull flake on the postgresql lane was diagnosed
              from the job log and resolved via rerun — content-unrelated).
NEXT STEP:    Begin the strategic prototype phase — prototype #1 pinned to
              v0.9.0. Maintainer picks prototype #1's domain/shape, then
              seeds CURRENT_ITERATION.md. Distilled prototypes later become
              ADR-010 Phase 4 Slice 2/3 reference apps + harness fixture
              profiles (v0.9.X range).
BLOCKERS:     none
FILES OF INTEREST:
              docs/iterations/2026-06-09-cut-v0.9.0.md,
              CHANGELOG.md ([0.9.0] section + fresh [Unreleased] pin entry),
              internal/cli/new.go (defaultPinnedFrameworkVersion=v0.9.0)
NOTES:        Prototype-phase cautions:
              - Module.Jobs / Module.Webhooks are reserved shape (boot-WARN,
                not executed); use pkg/tasks directly for background jobs.
              - outbox.NewKafkaBridge is deliberately unfinished.
              F-13 (P3): CLAUDE.md §directory-map still says cmd/goframe/ —
              fix opportunistically in any docs PR.
              Deletable untracked artifact: docs/audits/2026-06-07-exhaustive-audit.md
              (superseded draft, not in git; delete at maintainer's discretion).
              CORRECTION RECORD (2026-06-10): the previous handoff listed an
              "open P2 Router.Resource(\"\") panic" as item #3. This was a
              stale resurrection — the bug was fixed and shipped in v0.9.0.
              Verified: regression test pkg/nucleus/router_resource_empty_test.go
              exists and pins the joinPath floor-to-"/" fix; CHANGELOG [0.9.0]
              records the fix. Item removed. Do NOT re-add.

--- OPEN STRATEGIC ITEMS ---

1. Prototype phase — prototype #1 pinned to v0.9.0 per agreed plan.
   ADR-010 Phase 4 Slices 2/3 return in v0.9.X.

2. F-13 (P3, non-blocking) — CLAUDE.md §directory-map says cmd/goframe/;
   actual entry-point is cmd/nucleus/. Fix opportunistically.

--- DELETABLE ARTIFACT ---

docs/audits/2026-06-07-exhaustive-audit.md — superseded untracked draft.
Harmless; delete at maintainer's discretion (untracked, not in git).

*** CRITICAL — `main` is PR-only for EVERYONE including the maintainer ***
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-10
