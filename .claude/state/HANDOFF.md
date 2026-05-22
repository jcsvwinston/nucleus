# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    ADR-010 Phase 3a — effective-config inspection tooling — COMPLETE (committed + archived). No active iteration.
BRANCH:       main (clean after the close commit).
LAST COMMIT:  7a416ce feat(nucleus): effective-config inspection (ADR-010 Phase 3a) (feature) + the follow-up chore(state) close commit.
STATUS:       Shipped the CLI/API half of ADR-010 Phase 3. New stable pkg/nucleus API LoadEffective(paths, extraKeys...) + ConfigSource/EffectiveValue/EffectiveConfig: merges configured files with per-key provenance (source-kind + path; snapshot-and-diff in the new loadMerged that loadFromFiles now wraps). New CLI `nucleus config print --effective` (repeatable --config, --json; text `key = value [kind:path]`). Redaction reuses observe.DefaultRedactedKeys() + a parent-aware databases.*.url/.dsn rule. Security fix from the loop: extended the canonical observe set with the framework's compound secret keys (jwt_secret, admin_bootstrap_password, admin_cluster_token, session_redis_url, admin_cluster_redis_url, secret_access_key, account_key) — they previously printed/logged in cleartext. Freeze baseline rebaselined additively (+11). Docs all updated (ADR-010, CHANGELOG, CLI_CONTRACT_MATRIX config=transitional, API_CONTRACT_INVENTORY, CLI_BEST_PRACTICES, website cli/overview+configuration). Loop: architect/contract-guardian WARN→addressed, code-reviewer NITS, security WARN→FIXED, test-runner PASS. `go test ./...` green, freeze + drift guard pass.
NEXT STEP:    This session's two commits were pushed to origin/main. Owner picks the next iteration. New candidate #1: ADR-010 Phase 3b — the auth-gated /_/config endpoint (direct follow-on; reuses LoadEffective + the redaction helper). #2: ADR-010 Phase 3.1 — env-layer attribution + file:line provenance. #3: Oracle scaffold identifier-casing (PR #78). Also still open: ADR-010 Phase 4 (examples + site), and a /release-prep + tag v0.8.0 pass (HEAD is well past v0.7.0).
BLOCKERS:     none.
FILES OF INTEREST:
  - pkg/nucleus/config.go — loadMerged (provenance) + LoadEffective + redactionSet/shouldRedactKey. Phase 3b threads the effective snapshot from the builder into Run from here.
  - internal/cli/configcommands.go — config print --effective.
  - pkg/observe/redact.go — canonical redaction set (extended with framework secret keys).
  - docs/iterations/2026-05-22-adr010-phase3a-effective-config.md — this iteration's archive.
  - docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md — §Phase 3 records the 3a/3b/3.1 split + as-built decisions.

NOTES:
  - Phase 3b design (recorded in ADR-010 §Phase 3 + candidate #1): mount GET /_/config from the nucleus layer onto App.Router, gate on App.Admin != nil, wrap with admin.NewDatabaseAdminAuth(App.DefaultDB(), App.Session, App.Config.AdminPrefix) over the app-wide Casbin default-deny. The ADR's WithAdmin() gate does NOT exist. Needs the effective snapshot threaded builder→Run (new App field) + a story for direct-struct Run(App{}) (no file paths). Security-sensitive — integration-test 403 anon / 200 admin session / absent under WithoutDefaults.
  - `config` is documented `transitional` and deliberately NOT in the cli_primary_commands.txt freeze baseline (freeze it once 3b stabilises the surface — same frozen⟺stable principle used across this session's contract work).
  - Five iterations landed this session: 6e6a075, 1233bf4, 9227e7d, bbc7d60 (contract-registry + docs arc) and 7a416ce (ADR-010 Phase 3a).

Updated: 2026-05-22
