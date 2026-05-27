# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Get main CI green, then cut v0.8.0 — CI half DONE (main is green); v0.8.0 release DEFERRED to next session.
BRANCH:       main (clean, in sync with origin/main).
LAST COMMIT:  0eed39a fix(model): declare MySQL indexes inline so AutoMigrate is idempotent.
STATUS:       main CI is GREEN for the first time since ~2026-05-24 — run 26533028754 (commit 0eed39a) concluded `success`, including the CI Required Gate (all lanes: Test And Smoke, mysql, mssql, postgres, oracle, contract-freeze, compat, observability, website). Cleared four blockers this session (simplest→hardest): (1) govulncheck CVEs via dep bumps `544f39a`; (2) MySQL Error 1170 — key-bound string columns → VARCHAR(255) `47dfce4`; (3) MSSQL invalid-key-column — same pattern → NVARCHAR(255) `47dfce4`; (4) MySQL Error 1061 idempotency — indexes declared INLINE in CREATE TABLE `0eed39a`. Earlier in the session: fixed 4 stale cmd/nucleus tests (`bf7b881`) + a real app.New empty-templates panic (`d5c6203`). All code-reviewed, regression-tested, CHANGELOG updated.
NEXT STEP:    Cut v0.8.0 (iteration scope #4), now unblocked. Concrete sequence: (a) `/release-prep` validation pass; (b) promote CHANGELOG `[Unreleased] → [0.8.0] - <date>` + add a `### Compatibility statement`; (c) regenerate `docs/reports/`; (d) annotated `git tag v0.8.0` (match the v0.7.0 / ed5689b convention) + push → triggers `release.yml`. SHOW the owner the promoted CHANGELOG + tag message BEFORE pushing the tag (irreversible-ish; do not auto-tag/push the release).
BLOCKERS:     none.
FILES OF INTEREST:
  - CHANGELOG.md — has the full Unreleased set to promote into [0.8.0]; many entries this cycle (ADR-010 Phase 3a/3b/3.1 + layer-4, Oracle ADR-011, session_cookie_secure, the dep-CVE Security entry, the AutoMigrate string-key Fixed entry).
  - .claude/state/CURRENT_ITERATION.md — full scope/acceptance (CI lanes now all [x]) + carry-forward backlog.
  - pkg/model/migration_scaffold_{mysql,mssql}.go — the key-bound-string + inline-index DDL fixes.
NOTES:
  - Verification caveat: MySQL/MSSQL live behaviour is CI-only (no local DB containers); the DDL fixes were unit-tested for SQL shape and confirmed green by CI run 26533028754.
  - #2 (MySQL) and #3 (MSSQL) were the same bug class — a Go string field used as PK/index mapped to an un-indexable type (TEXT / NVARCHAR(MAX)). Postgres/SQLite already index TEXT; Oracle already used VARCHAR2.
  - Governance follow-up still open (pre-existing): enable branch protection / required gate on main so red CI cannot be pushed (main had been red since ~2026-05-24 because direct pushes bypass the gate).
  - Post-v0.8.0 carry-forward: P1 WithoutDefaults() admin-bootstrap leak (pkg/app/app.go:~272); P2 Resource("") panic (pkg/nucleus/router.go); ADR-010 §2 layer 5 (module-specific config validation — last validator layer); nested admin/{agent,proto,server} modules not covered by root govulncheck (security hygiene).

Updated: 2026-05-27
