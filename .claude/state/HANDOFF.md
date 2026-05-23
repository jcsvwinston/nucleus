# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    ADR-010 Phase 3.1 — env-layer attribution + file:line provenance — COMPLETE, UNCOMMITTED (owner must commit before starting new work).
BRANCH:       main
LAST COMMIT:  6871f56 chore(state): close ADR-010 Phase 3b iteration  [Phase 3.1 change set is NOT yet committed — all changes are in the working tree]
STATUS:       done — all acceptance criteria met, full iteration loop green (architect/code/security/dependency/contract/test/examples/docs/website/changelog), state files archived. The working tree contains the complete Phase 3.1 diff ready for the owner to commit in the two-commit sequence below.
NEXT STEP:    OWNER MUST COMMIT. The Phase 3.1 implementation is complete and verified but DELIBERATELY UNCOMMITTED. Execute the two-commit sequence:

  COMMIT 1 (feature):
    git add pkg/nucleus/config.go pkg/nucleus/env_layer_test.go pkg/nucleus/provenance_line_test.go internal/cli/configcommands.go internal/cli/configcommands_test.go go.mod go.sum contracts/baseline/api_exported_symbols.txt CHANGELOG.md docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md docs/reference/API_CONTRACT_INVENTORY.md docs/reference/CLI_CONTRACT_MATRIX.md docs/reference/CONFIG_KEY_REGISTRY.md docs/reference/DEVELOPER_MANUAL.md docs/guides/AUTH_GUIDE.md docs/guides/DEPLOYMENT_GUIDE.md website/docs/concepts/configuration.md website/docs/cli/overview.md website/docs/features/observability.md
    git commit -m "feat(nucleus): env layer + file:line config provenance (ADR-010 Phase 3.1)

Apply the NUCLEUS_-prefixed env layer in the fluent FromConfigFile->Run
path (defaults < files < env), closing a gap where env overrides never
took effect via the builder; attribute env keys as [env:NAME] and reject
an empty value on a non-nullable security key. Add additive
ConfigSource.Line: YAML file sources report their 1-based source line
(go.yaml.in/yaml/v3 promoted to a direct dep, used only in unexported
helpers); TOML/JSON report kind+path. CLI renders kind:path:line."

  COMMIT 2 (state):
    git add .claude/state/CURRENT_ITERATION.md .claude/state/HANDOFF.md docs/iterations/2026-05-23-adr010-phase3.1-env-and-fileline.md
    git commit -m "chore(state): close ADR-010 Phase 3.1 iteration"

BLOCKERS:     none.
FILES OF INTEREST:
  - pkg/nucleus/config.go — modified: applyEnvLayer wired into loadMerged after file loop; ConfigSource.Line int added (UNCOMMITTED).
  - pkg/nucleus/env_layer_test.go — new: env-layer attribution + ErrSecurityKeyNotNullable tests (UNCOMMITTED).
  - pkg/nucleus/provenance_line_test.go — new: file:line provenance YAML tests (UNCOMMITTED).
  - internal/cli/configcommands.go — modified: CLI renders kind:path:line for sources with Line > 0 (UNCOMMITTED).
  - internal/cli/configcommands_test.go — modified: updated for new rendering (UNCOMMITTED).
  - go.mod / go.sum — modified: go.yaml.in/yaml/v3 promoted indirect→direct (UNCOMMITTED).
  - contracts/baseline/api_exported_symbols.txt — modified: additive rebaseline +1 (ConfigSource.Line) (UNCOMMITTED).
  - CHANGELOG.md — modified: minor-bump entry under Unreleased (UNCOMMITTED).
  - docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md — modified: Phase 3.1 as-built note (UNCOMMITTED).
  - docs/reference/API_CONTRACT_INVENTORY.md, CLI_CONTRACT_MATRIX.md, CONFIG_KEY_REGISTRY.md, DEVELOPER_MANUAL.md — modified (UNCOMMITTED).
  - docs/guides/AUTH_GUIDE.md, docs/guides/DEPLOYMENT_GUIDE.md — modified: env-var bug fixes (single-underscore examples, session_cookie_samesite key) (UNCOMMITTED).
  - website/docs/concepts/configuration.md, website/docs/cli/overview.md, website/docs/features/observability.md — modified; drift guard 0/0/0, build clean (UNCOMMITTED).
  - docs/iterations/2026-05-23-adr010-phase3.1-env-and-fileline.md — new archive (UNCOMMITTED, commit 2 above).
  - .claude/state/CURRENT_ITERATION.md — reset to awaiting-direction stub with carry-forwards (UNCOMMITTED, commit 2 above).

NOTES:
  - Env layer: applyEnvLayer called in loadMerged after the file loop; same env.Provider/__→. transform as app.LoadConfig; only schema-recognised keys applied (unknown NUCLEUS_* env vars silently ignored); empty value on a non-nullable security key (e.g. NUCLEUS_JWT_SECRET=) is a boot error (ErrSecurityKeyNotNullable). Behavioural: env now applies via the fluent builder (previously the FromConfigFile→Run path ignored env entirely — this closes the ADR-010 §4 precedence gap).
  - file:line: additive ConfigSource.Line int (freeze baseline rebaselined +1 symbol); YAML-only (via go.yaml.in/yaml/v3 node walk in unexported helpers); TOML/JSON no line (documented). CLI renders kind:path:line. Known limitations: _append/_remove-derived keys and anchor/merge-key-reached keys carry no line number.
  - go.yaml.in/yaml/v3 promoted indirect→direct dep (no ADR needed — dependency-impact ACCEPT: minor, confined to unexported helpers, no consumer API change).
  - Doc sweep carry-forward (closed this session): AUTH_GUIDE and DEPLOYMENT_GUIDE had pre-existing env-var doc bugs (wrong single-underscore NUCLEUS_* examples, wrong session_cookie_samesite key) — fixed as a side-effect of Phase 3.1 doc pass.
  - Next iteration: owner selects from the prioritised candidate list in CURRENT_ITERATION.md. Top candidates: Oracle model-scaffold identifier-casing (PR #78 follow-up, #1), Oracle multi-block AutoMigrate (#2), session_cookie_secure default flip (#3).

Updated: 2026-05-23
