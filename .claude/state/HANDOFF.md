# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    F-4 firewall /vN + admin RBAC eft + admin /api/* authn —
              COMPLETE. Archived to
              docs/iterations/2026-06-09-f4-firewall-and-admin-authn.md.
              CURRENT_ITERATION.md is EMPTY — no active iteration. Next
              iteration is the maintainer's choice from the remediation queue
              below.
BRANCH:       main (no open feature branch; all work landed via PRs)
LAST COMMIT:  f39ff75  fix(admin): enforce authentication at the router edge for /api/* (ADR-016) (#100)
STATUS:       done — F-4 queue (#98/#99/#100) and SEC-1 (#97) fully merged;
              main is clean and green. State-file changes from this handoff
              still need a branch → commit → PR (main is PR-only).
NEXT STEP:    Open a branch for the state-file changes (this handoff + archive),
              push, create PR, then pick the next backlog item from the queue
              below and run /resume to seed CURRENT_ITERATION.md.
BLOCKERS:     none
FILES OF INTEREST:
              docs/adrs/ADR-015-firewall-vn-resolution-and-leak-dispositions.md,
              docs/adrs/ADR-016-admin-api-auth-enforcement.md,
              contracts/firewall_test.go (blessedLeaks + resolver),
              pkg/authz/enforcer.go (casbin wrap + 3 forwarders),
              pkg/admin/panel.go (mountAPIRoutes / warnAdminAuthDisabled),
              pkg/admin/rbac.go (eft column),
              pkg/admin/handlers.go (LOW follow-up: authErrorToDomain leaks err.Error())
NOTES:        cmd/ is cmd/nucleus; CLAUDE.md §directory-map still says
              cmd/goframe/ (F-13, P3 — fix opportunistically in any docs PR).
              Open LOW follow-up task_19c389c9: normalize admin 401 body so it
              does not leak raw err.Error() (pkg/admin/handlers.go
              authErrorToDomain). Non-blocking.

--- REMEDIATION QUEUE (severity order, one branch+PR each) ---

DONE (this session + immediately prior):
  SEC-1 (#97) — corsAllowCredentials default → false. MERGED.
  F-4   (#98) — firewall /vN resolver + casbin wrap + blessedLeaks. MERGED.
  F-4b  (#99) — admin RBAC eft column. MERGED.
  F-4c  (#100) — admin /api/* authentication at router edge. MERGED.

REMAINING:

1. DOC-1 — RATE_LIMITING guide rewrite (stale rate-limit API + AUTH config keys).
   DOC-2 — MULTISITE guide rewrite.
   WEB-1 — website: storage.Metadata→PutOptions on any storage pages.

2. CLI-V2-1 — scaffold toolchain directive derived from go.mod go-line + a
   freshness test so it can't drift again.

3. GOV-1 — COMPATIBILITY_SLO promotion update + reference-date sweep across
   governance docs.

4. §9 CI — encode docs-content-verifier discipline (Go symbols, YAML keys,
   Go version) into scripts/website/check-coverage.sh + a CI lane.

--- OPEN LOW FOLLOW-UPS (not blocking, record here) ---

- LOW task_19c389c9: pkg/admin/handlers.go authErrorToDomain — normalize 401
  body; currently leaks raw err.Error() strings to the client. LOW priority.
- LOW-A: pkg/model/meta.go ~L427 — parseDBTag does not validate the column:
  tag value via isValidIdentifierLike (developer-trust gap; no attacker path).
- LOW-B: pkg/admin/handlers.go ~L1160 — single-column sanitizeOrderBy is a
  duplicate of the model-layer allow-list; consolidate to prevent drift.

--- DELETABLE ARTIFACT ---

docs/audits/2026-06-07-exhaustive-audit.md — superseded untracked draft from
the prior audit branch. Harmless; delete at maintainer's discretion
(git rm does not apply since it is untracked).

*** CRITICAL — `main` is PR-only for EVERYONE including the maintainer ***
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-09
