# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

ADR-010 §2 layer 5 — module-specific config binding/validation (the fifth and
final validator layer). Implemented and fully reviewed; PR pending.

Bind each mounted module's `modules.<name>.*` config subtree into its typed
`Module[C].Config`, apply `default:` struct tags, and enforce `validate:` tags
at boot — on both the FromConfigFile builder path and the direct-struct Run
path.

## Scope

- **in:** `pkg/nucleus/{config.go, nucleus.go, module.go,
  validate_module_config.go (new), validate_module_config_test.go (new)}`,
  `contracts/baseline/api_exported_symbols.txt` (+1 additive sentinel),
  `docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md`,
  `docs/reference/{API_CONTRACT_INVENTORY.md, CONFIG_KEY_REGISTRY.md,
  DEVELOPER_MANUAL.md}`, `website/docs/concepts/configuration.md`,
  `CHANGELOG.md`.
- **out:** env-layer override of `modules.*` (deferred); `NUCLEUS_MODULES__*`
  env vars.

## Acceptance criteria

- [x] `modules.<name>.*` namespace accepted by `FromConfigFile` (exempted from
  the layer-2 unknown-key guard via `stripModuleConfigKeys`), extracted into
  per-module sub-koanf retained on unexported `App.moduleConfigsRaw`.
- [x] `bindModuleConfigs` runs in `Run` (after `validateModuleRequires`, before
  `app.New`) — fail-fast, both paths (builder binds from file; direct-struct
  `Run` applies defaults+validate only).
- [x] `default:` tags applied by in-package reflection helper (`applyDefaults`)
  — no new dependency; scalars + `time.Duration` + nested structs; fills only
  zero fields.
- [x] `validate:` tags enforced via `pkg/validate`; failures surface new
  sentinel `ErrInvalidModuleConfig`.
- [x] Config for an unmounted module is a non-fatal WARN.
- [x] Security: module config excluded from the `/_/config` effective snapshot
  (no redaction contract); `moduleConfigsRaw` nilled after bind so cleartext
  module secrets are GC'd.
- [x] Full iteration loop GREEN: architect PASS, code-reviewer NITS (all
  addressed), security-auditor WARN (both SHOULDs fixed), contract-guardian
  PASS (freeze green, +1 additive `ErrInvalidModuleConfig`, `modules.*`
  deliberately NOT in `ContractConfigKeyPatterns`), test-runner (full
  `go test ./...` + `-race` on `pkg/nucleus` green), doc-updater UPDATED,
  website-curator UPDATED (build + drift guard clean), changelog-writer Added
  entry (semver minor).
- [ ] PR opened, CI green, merged via the protected-main flow.

## Status

### Done

- Implementation: `validate_module_config.go`, `applyDefaults` helper,
  `bindModuleConfigs` wired into both `Run` paths, `stripModuleConfigKeys`
  guard, `moduleConfigsRaw` GC nil-out.
- Full iteration loop: all eight review/test/doc/governance steps GREEN.
- Semver assessment: **minor** (additive only — new exported sentinel
  `ErrInvalidModuleConfig`; no removals or behaviour changes on existing
  surfaces).

### In progress

- PR open / CI gate / squash-merge via protected-main flow.

### Blocked

- (none)

## Files of interest

- `pkg/nucleus/validate_module_config.go` (new)
- `pkg/nucleus/validate_module_config_test.go` (new)
- `pkg/nucleus/config.go` (stripModuleConfigKeys + moduleConfigsRaw)
- `pkg/nucleus/nucleus.go` (bindModuleConfigs call in Run)
- `pkg/nucleus/module.go` (Module[C].Config bind target)
- `contracts/baseline/api_exported_symbols.txt` (+1 ErrInvalidModuleConfig)
- `docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md` (layer 5 section)
- `docs/reference/API_CONTRACT_INVENTORY.md`
- `docs/reference/CONFIG_KEY_REGISTRY.md`
- `docs/reference/DEVELOPER_MANUAL.md`
- `website/docs/concepts/configuration.md`
- `CHANGELOG.md` (Unreleased / Added)

## Notes / decisions log

- 2026-05-29 — Iteration "Remediate the 2026-05-29 exhaustive audit" COMPLETE.
  PR #82 squash-merged; `main` is at commit `64897f4`. Archived to
  `docs/iterations/2026-05-29-audit-remediation.md`.
- 2026-05-29 — Active iteration set to ADR-010 §2 layer 5 (module-specific
  config binding/validation). Branch: `feat/adr010-layer5-module-config`.
  Implementation + full iteration loop complete; awaiting PR merge.

---

## Backlog (carry-forward from 2026-05-29-audit-remediation)

### Pending on maintainer (Carlos)

- **Decide the `examples/` + `CLAUDE.md` directory-map question.** Only
  `examples/mvc_api` is a tracked Go app (in the root module, built/tested by
  CI). The other three example trees are local/untracked scaffolding that does
  not match what `CLAUDE.md`'s directory map advertises (`mvc_api`,
  `fleetmanager`, `ecommerce_dashboard`, `showcase_demo`, `plugins/…`). Decide
  whether to track them, drop them, or correct the directory map. NOTE: editing
  `CLAUDE.md` is a self-contained housekeeping change — route as its own branch
  + PR.
- **Block 8 leftovers** from the audit roadmap
  (`docs/audits/2026-05-29-exhaustive-audit.md`) remain unstarted — schedule
  as a follow-up iteration.

### Framework / ADR follow-ups

- Cloud Secrets Provider plugin extraction (AWS/GCP/Azure/Vault out of core).
- SchemaDrift column-type comparison + `docs/guides/MODELING_MULTI_DATABASE.md`.
- `go mod tidy` unblock — resolve the `admin/proto` replace-directive.
- `tasks.Manager` struct→interface DEP (optional DEP-2026-004).
- Audit §7 minors: 503 path test for `/healthz`; endpoints-parity doc-parsing;
  `pkg/health/{db,redis,storage}.go` tests.

### Deferred carry-forwards (not blocking)

_Oracle multi-block AutoMigrate (2026-05-24):_
- Route admin-bootstrap PL/SQL through `db.ExecScript`. (architect NIT.)
- Oracle DDL auto-commit vs the Migrator transaction.

_Oracle identifier-casing (2026-05-23):_
- CI governance reconciliation (mssql + oracle): required vs exploratory.
- Oracle reserved-word + dotted-identifier hardening.

_Phase 3b / observability (2026-05-22/23):_
- GCS credential-redaction forward-compat.
- Reverse-proxy hardening note for `/_/config`.
- Relocate `pkg/observability` to `internal/` post-v1.0.
- `Runtime.AutoMigrate` production guard.

_ADR-010 Phase 1 (remaining):_
- Service-shutdown timeout — `nucleus.Run`'s `wg.Wait()` has no deadline.
  (NOTE: the app-level `Lifecycle.OnShutdown` deadline shipped as FW-6 in
  2026-05-29-audit-remediation; the `wg.Wait()` service-shutdown bound is the
  still-open sibling.)

_Internal-docs (low-priority):_
- `DETAILED_TUTORIAL.md` flat-handler style predates `nucleus.Module` pattern.
- `DEVELOPER_MANUAL.md §5.3` references `internal/contracts`.

---

> **IMPORTANT — `main` is PR-only (branch protection active since 2026-05-28).**
> Every change — including `.claude/state/*` and `docs/*` — must go through:
> create branch → push → `gh pr create` → wait for `CI Required Gate` green
> (~7–20 min, full matrix incl. live MSSQL/Oracle) → self-merge
> (`gh pr merge --squash --delete-branch`) → `git checkout main && git pull`.
> Direct `git push origin main` is REJECTED.
> Settings: `enforce_admins=true`, required check "CI Required Gate"
> `strict=true`, `required_approving_review_count=0`,
> `required_conversation_resolution=true`.
