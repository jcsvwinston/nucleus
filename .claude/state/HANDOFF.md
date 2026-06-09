# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Remediation-queue sweep COMPLETE — all six items merged
              (#102–#107). No active iteration. CURRENT_ITERATION.md
              is EMPTY (empty template). Next iteration is the
              maintainer's choice; the only open strategic item is the
              parked v0.8.0 release.
BRANCH:       main (no open feature branch; all work landed via PRs)
LAST COMMIT:  807d50b  ci(docs): automate the §9 body-content fact-check (§9 CI) (#107)
STATUS:       done — entire prior remediation queue cleared; main is
              clean and green. State-file changes from THIS handoff
              still need a branch → commit → PR (main is PR-only).
NEXT STEP:    Open a branch for these state-file changes, push, create
              PR, merge; then decide whether to cut v0.8.0 (release-prep
              was parked until main CI green — it is now green and
              materially ahead). Run /release-prep and /resume before
              cutting.
BLOCKERS:     none
FILES OF INTEREST:
              scripts/website/bodycheck/main.go (+ check-coverage.sh §4),
              pkg/model/crud.go (SanitizeOrderBy — new exported symbol,
              additive baseline change in #103),
              internal/cli/new.go (scaffold go/toolchain directives +
              TestScaffoldGoDirectivesTrackGoMod in #104),
              docs/governance/* (refreshed dates/facts in #106),
              pkg/admin/handlers.go (authErrorToDomain fixed in #102)
NOTES:        cmd/ is cmd/nucleus; CLAUDE.md §directory-map still says
              cmd/goframe/ (F-13, P3 — fix opportunistically in any docs
              PR). The /handoff command protocol was relaxed in #101:
              it may create its own branch+commit+PR for state files
              (no tags/releases). The three prior LOWs
              (task_19c389c9, LOW-A, LOW-B) are RESOLVED via #102 and #103
              respectively — no open LOW follow-ups remain.

--- REMEDIATION QUEUE (severity order) ---

DONE (this session + all prior):
  SEC-1  (#97)  — corsAllowCredentials default → false. MERGED.
  F-4    (#98)  — firewall /vN resolver + casbin wrap + blessedLeaks. MERGED.
  F-4b   (#99)  — admin RBAC eft column. MERGED.
  F-4c   (#100) — admin /api/* authentication at router edge. MERGED.
  LOW    (#102) — admin 401 body no longer leaks raw err.Error(). MERGED.
  LOW-A/B(#103) — model column-tag validation + SanitizeOrderBy consolidated. MERGED.
  CLI-V2-1(#104)— scaffold go/toolchain tracks go.mod + freshness test. MERGED.
  DOC-1/2(#105) — stale rate-limit + multisite guides corrected. MERGED.
  WEB-1  (#105) — website storage page updated. MERGED.
  GOV-1  (#106) — governance docs: dates, Go-version falsehood, mssql/oracle state. MERGED.
  §9 CI  (#107) — bodycheck tool + CI lane wired into check-coverage.sh. MERGED.

REMAINING:    none — queue is empty.

--- OPEN STRATEGIC ITEMS ---

1. v0.8.0 release — release-prep was completed but parked until main
   CI was green. main is now green and materially ahead of that prep
   (PRs #97–#107 are new work). Re-validate via /release-prep before
   cutting the tag.

2. F-13 (P3, non-blocking) — CLAUDE.md §directory-map says
   cmd/goframe/; actual entry-point is cmd/nucleus/. Fix
   opportunistically in any docs PR.

--- DELETABLE ARTIFACT ---

docs/audits/2026-06-07-exhaustive-audit.md — superseded untracked draft.
Harmless; delete at maintainer's discretion (untracked, not in git).

*** CRITICAL — `main` is PR-only for EVERYONE including the maintainer ***
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-09
