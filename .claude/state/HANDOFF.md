# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    `session_cookie_secure` secure-by-default (Phase 2b MED-1) — COMPLETE, UNCOMMITTED (owner must commit).
BRANCH:       main
LAST COMMIT:  8e03618 chore(state): correct stale post-commit handoff (Oracle work shipped)  [the session_cookie_secure change set is NOT yet committed — all changes are in the working tree]
STATUS:       done — all acceptance criteria met, full iteration loop green (security-auditor PASS / architect PASS / doc-updater + website-curator UPDATED / test-runner PASS / freeze PASS). State archived. Working tree contains ONLY the session_cookie_secure change set (1-line default flip + test + docs + CHANGELOG + ADR-008 cross-ref + website + state).
NEXT STEP:    OWNER MUST COMMIT. Two-commit sequence:

  COMMIT 1 (fix):
    git add pkg/app/config.go pkg/app/config_test.go docs/reference/CONFIG_KEY_REGISTRY.md docs/guides/AUTH_GUIDE.md docs/guides/DEPLOYMENT_GUIDE.md docs/adrs/ADR-008-csrf-followups.md CHANGELOG.md website/docs/features/auth.md website/docs/concepts/configuration.md website/docs/architecture/principles.md
    git commit -m "fix(app): session cookie Secure by default (Phase 2b MED-1)

Flip session_cookie_secure's default from false to true so the session
cookie refuses to ride over plain HTTP; dev/HTTP opts out with
session_cookie_secure: false. Matches the CSRF secure-cookie posture
(ADR-008). BREAKING (operational) for plain-HTTP deployments."

  COMMIT 2 (state):
    git add .claude/state/CURRENT_ITERATION.md .claude/state/HANDOFF.md docs/iterations/2026-05-23-session-cookie-secure-default.md
    git commit -m "chore(state): close session_cookie_secure iteration"

  (Reconcile reminder: when committing the state files, this HANDOFF describes the PRE-COMMIT state; the next /resume must trust `git log` over this note. After both commits, last commit is the COMMIT 2 hash, not 8e03618.)

BLOCKERS:     none.
FILES OF INTEREST (session_cookie_secure change set — UNCOMMITTED):
  - pkg/app/config.go — defaults() sets SessionCookieSecure: true; field godoc updated.
  - pkg/app/config_test.go — TestLoadConfig_Defaults asserts the new default.
  - docs/reference/CONFIG_KEY_REGISTRY.md — default true + dev opt-out note.
  - docs/guides/AUTH_GUIDE.md, docs/guides/DEPLOYMENT_GUIDE.md — prod checklists reframed; stale X-Forwarded-Proto + "Secure (in production)"/"SameSite=Strict" lines fixed.
  - docs/adrs/ADR-008-csrf-followups.md — Neutral consequence cross-reference (session cookie followed the CSRF secure-by-default pattern; no new ADR).
  - CHANGELOG.md — BREAKING (operational) Security entry under Unreleased.
  - website/docs/{features/auth,concepts/configuration,architecture/principles}.md — secure-by-default reframing; drift guard 0/0/0, build clean.
  - docs/iterations/2026-05-23-session-cookie-secure-default.md — this iteration's archive (commit 2).
  - .claude/state/CURRENT_ITERATION.md — reset to awaiting-direction stub (commit 2).

NOTES:
  - Design: hard flip + explicit opt-out (`session_cookie_secure: false`), mirroring ADR-008's CSRF `Secure: !InsecureCookie`. A config `null` reverts to the secure default (bool + struct-provider seeding), so the gap can't silently re-open — the non-nullable set was redundant. HttpOnly is always-on; SameSite default is `lax`. Scope limited to the Secure attribute.
  - No exported-symbol change and no config-key-pattern change (only the default VALUE changed) → contract freeze PASS, baseline untouched.
  - One follow-up recorded in CURRENT_ITERATION.md: startup validation that SameSite=None requires Secure=true (security-auditor LOW; pre-existing, low blast radius).
  - Two earlier follow-ups still open: CI required-vs-exploratory reconciliation (mssql+oracle); Oracle reserved-word + dotted-identifier hardening.
  - Next iteration: owner selects from the prioritised candidate list. Top candidates now: #1 Oracle multi-block AutoMigrate execution, #2 ADR-010 §2 layer-3 field-semantic validation, #3 ADR-010 Phase 4 (docs/website/examples).
  - Recent shipped arc (all on origin/main): ADR-010 Phase 3a/3b/3.1, Oracle identifier-casing (ADR-011). The session_cookie_secure work above is the only uncommitted iteration.

Updated: 2026-05-23
