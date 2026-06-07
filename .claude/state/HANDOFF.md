# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    F-3 CRUD placeholder portability + SEC OrderBy injection fix —
              COMPLETE. Archived to
              docs/iterations/2026-06-07-f3-crud-placeholders-and-orderby-sec.md.
              CURRENT_ITERATION.md is EMPTY — no active iteration. Next
              iteration is the maintainer's choice from the remediation queue
              below.
BRANCH:       main (no open feature branch; all work landed via PRs)
LAST COMMIT:  d0d041f  fix(model): sanitize ORDER BY to close SQL injection in FindAll
STATUS:       done — F-3 and SEC fully merged; main is clean.
NEXT STEP:    Pick the next item from the remediation queue (see below),
              open a branch, and run /resume to seed CURRENT_ITERATION.md.
              Recommended: SEC-1 (small, self-contained CORS default fix) or
              F-4 (needs contract-guardian + migration-assistant pre-work).
BLOCKERS:     none
FILES OF INTEREST:
              pkg/model/crud.go (rebind + sanitizeOrderBy landed here),
              pkg/app/app.go (SEC-1 target: corsAllowCredentials default),
              contracts/firewall_test.go (F-4 target),
              pkg/admin/handlers.go ~L1160 (LOW: duplicate sanitizeOrderBy),
              pkg/model/meta.go ~L427 (LOW: parseDBTag column-tag validation),
              docs/audits/2026-06-07-exhaustive-audit-v2.md (full finding list)
NOTES:        cmd/ is cmd/nucleus; CLAUDE.md §directory-map still says
              cmd/goframe/ (F-13, P3 — fix opportunistically in any docs PR).

--- REMEDIATION QUEUE (severity order, one branch+PR each) ---

1. F-4 — firewall blind to /vN module-path imports + casbin/jwt embedded in
   frozen types. Needs contract-guardian + migration-assistant pre-work;
   probably an ADR. Highest functional correctness risk.

2. SEC-1 — corsAllowCredentials default → false in pkg/router (or pkg/app/app.go);
   fix the misleading R4 comment in pkg/app/app.go. Small, isolated.

3. DOC-1 — RATE_LIMITING guide rewrite (stale rate-limit API + AUTH config keys).
   DOC-2 — MULTISITE guide rewrite.
   WEB-1 — website: storage.Metadata→PutOptions on any storage pages.

4. CLI-V2-1 — scaffold toolchain directive derived from go.mod go-line + a
   freshness test so it can't drift again.

5. GOV-1 — COMPATIBILITY_SLO promotion update + reference-date sweep across
   governance docs.

6. §9 CI — encode docs-content-verifier discipline (Go symbols, YAML keys,
   Go version) into scripts/website/check-coverage.sh + a CI lane.

--- TWO LOW FOLLOW-UPS (not blocking, but record here) ---

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

Updated: 2026-06-07
