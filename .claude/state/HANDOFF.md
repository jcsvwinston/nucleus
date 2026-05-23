# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    ADR-010 Phase 3b — auth-gated GET /_/config endpoint — COMPLETE, UNCOMMITTED (owner must commit).
BRANCH:       main
LAST COMMIT:  a6d0557 chore(state): close ADR-010 Phase 3a iteration  [Phase 3b change set is NOT yet committed — all changes are in the working tree]
STATUS:       done — all acceptance criteria met, full iteration loop green, state files archived. The working tree contains the complete Phase 3b diff ready for the owner to commit in the two-commit pattern described below.
NEXT STEP:    Owner commits the Phase 3b work. Proposed two-commit sequence:
  1. git add pkg/nucleus/config_endpoint.go pkg/nucleus/config_endpoint_test.go pkg/nucleus/nucleus.go pkg/observe/redact.go docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md docs/reference/API_CONTRACT_INVENTORY.md docs/reference/CLI_CONTRACT_MATRIX.md docs/guides/AUTH_GUIDE.md docs/guides/OBSERVABILITY_BASELINE.md docs/reference/DEVELOPER_MANUAL.md CHANGELOG.md website/docs/concepts/configuration.md website/docs/features/admin.md website/docs/features/observability.md
     git commit -m "feat(nucleus): auth-gated /_/config endpoint (ADR-010 Phase 3b)

Mount GET /_/config from the nucleus layer when the admin subsystem is
active. Gated by admin-session auth (403 anon / 200 admin) behind the
ADR-004 Casbin default-deny, exempted via a bootstrap-subject policy added
in Run. Threads the redacted effective-config snapshot builder→Run; the
direct-struct path falls back to a \"runtime\"-kind snapshot. Extends the
canonical redaction set with the AWS access-key-ID pair so the S3 credential
is fully covered in both /_/config and logs."
  2. git add .claude/state/CURRENT_ITERATION.md .claude/state/HANDOFF.md docs/iterations/2026-05-23-adr010-phase3b-config-endpoint.md
     git commit -m "chore(state): close ADR-010 Phase 3b iteration"
BLOCKERS:     none.
FILES OF INTEREST:
  - pkg/nucleus/config_endpoint.go — new: mountConfigEndpoint + admin-session gate + effectiveFromConfig runtime fallback (UNCOMMITTED).
  - pkg/nucleus/config_endpoint_test.go — new: 7 tests covering 403 anon / 200 admin / 404 WithoutDefaults / redaction / runtime fallback (UNCOMMITTED).
  - pkg/nucleus/nucleus.go — modified: unexported App.effective field; captured in FromConfigFile; mounted in Run; Casbin exemption AddPolicy at Run-time (UNCOMMITTED).
  - pkg/observe/redact.go — modified: access_key_id + aws_access_key_id added to canonical set (closes S3 access-key-ID cleartext leak) (UNCOMMITTED).
  - docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md — modified: status → landed 2026-05-23, Phase 3b as-built note (UNCOMMITTED).
  - docs/reference/API_CONTRACT_INVENTORY.md — modified: runtime ConfigSource.Kind + /_/config endpoint row (UNCOMMITTED).
  - docs/reference/CLI_CONTRACT_MATRIX.md, docs/guides/AUTH_GUIDE.md, docs/guides/OBSERVABILITY_BASELINE.md, docs/reference/DEVELOPER_MANUAL.md — modified (UNCOMMITTED).
  - CHANGELOG.md — modified: Added + Security entries under Unreleased; semver minor (UNCOMMITTED).
  - website/docs/concepts/configuration.md, website/docs/features/admin.md, website/docs/features/observability.md — modified; drift guard 0/0/0, build clean (UNCOMMITTED).
  - docs/iterations/2026-05-23-adr010-phase3b-config-endpoint.md — new archive (UNCOMMITTED, commit 2 above).
  - .claude/state/CURRENT_ITERATION.md — reset to empty stub with carry-forwards (UNCOMMITTED, commit 2 above).

NOTES:
  - Three defence-in-depth layers on /_/config: (1) mount gate (core.Admin != nil); (2) Casbin default-deny exemption added via core.Authorizer.AddPolicy(authz.BootstrapSubject, "/_/config", "*") at Run-time — no edit to stable pkg/authz.BootstrapAllowList; (3) admin.NewDatabaseAdminAuth session check (anon 403 / admin 200, Cache-Control: no-store).
  - Snapshot threading: App.effective captured in FromConfigFile with same load opts + LogRedactExtraKeys; direct-struct Run(App{}) path falls back to effectiveFromConfig (flattens live core.Config into a "runtime"-kind EffectiveConfig).
  - Redaction deliberate non-inclusion: AWS access-key IDs ARE redacted; public identifiers (account_name, smtp_user, admin bootstrap username/email) are deliberately NOT — documented in pkg/observe/redact.go.
  - No new exported pkg/* symbol introduced; contract baseline untouched (Phase 3a already shipped LoadEffective/EffectiveConfig).
  - Carry-forward low-priority items for next session: GCS credential redaction forward-compat; reverse-proxy hardening note for /_/config in DEPLOYMENT_GUIDE.md. See CURRENT_ITERATION.md for full prioritised candidate list.

Updated: 2026-05-23
