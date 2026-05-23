# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

**ADR-010 Phase 3b — auth-gated `GET /_/config` endpoint** (started
2026-05-23). The runtime counterpart to Phase 3a's `nucleus config print
--effective`: a session-admin-gated HTTP endpoint that returns the effective
merged configuration (with secrets redacted), mounted from the nucleus layer.

## Scope

- Mount `GET /_/config` from `pkg/nucleus.Run` onto `App.Router`, **only when
  the admin subsystem is active** (`core.Admin != nil`, i.e. not under
  `WithoutDefaults()`). The ADR's `WithAdmin()` toggle does not exist.
- Gate with `admin.NewDatabaseAdminAuth(core.DefaultDB(), core.Session,
  core.Config.AdminPrefix)` — session-based; anon → 403, admin session → 200.
- Coexist with the app-wide Casbin default-deny (ADR-004): the enforcer only
  understands JWT claims, so a session-admin resolves to the `anonymous`
  subject there. Exempt `/_/config` via `core.Authorizer.AddPolicy(
  authz.BootstrapSubject, "/_/config", "*")` in the nucleus layer (same
  precedent as `/admin/*` in `BootstrapAllowList`) — **without** editing the
  stable `pkg/authz` package.
- Thread the redacted effective-config snapshot from the builder into `Run`
  (new unexported `App.effective *EffectiveConfig`, computed in
  `FromConfigFile` with the same load opts + `LogRedactExtraKeys`). Direct-
  struct `Run(App{})` (no file paths) falls back to a `runtime`-kind snapshot
  flattened from the live `core.Config`.
- Redaction reuses Phase 3a's `LoadEffective` + canonical
  `observe.DefaultRedactedKeys()`, extended by the app's `LogRedactExtraKeys`
  (the endpoint is app-aware; the CLI today is not — minor known asymmetry).

## Acceptance criteria

- Anon `GET /_/config` → 403 (behind default-deny + admin auth).
- Admin-session `GET /_/config` → 200, JSON `EffectiveConfig`, secrets redacted.
- `WithoutDefaults()` apps do **not** expose `/_/config` (404).
- No new exported `pkg/*` symbol → contract baseline untouched (Phase 3a
  already shipped `LoadEffective`/`EffectiveConfig`).
- `go test ./...` green; iteration loop clean; CHANGELOG + website + ADR status
  updated.

## Status

### In progress (this iteration)

- (none)

### Done (this iteration)

- **ADR-010 Phase 3b — auth-gated `GET /_/config` endpoint** (2026-05-23,
  pending commit). New `pkg/nucleus/config_endpoint.go`
  (`mountConfigEndpoint` + admin-session gate + `effectiveFromConfig` runtime
  fallback); `nucleus.go` threads the redacted snapshot via unexported
  `App.effective` (captured in `FromConfigFile`) and mounts the endpoint in
  `Run`. Security hardening: `pkg/observe` canonical set gained
  `access_key_id`/`aws_access_key_id` (closes a real S3 access-key-ID leak in
  both logs and `/_/config`). Loop verdicts: architect WARN→addressed (docs),
  code-reviewer NITS→addressed (pointer receiver, DefaultDB nil-warn, cloneApp
  comment, +2 tests), security FAIL→FIXED→re-audit PASS, contract-guardian
  PASS (no exported-symbol change; baselines untouched), test-runner PASS
  (full suite + race + freeze green). Docs: ADR-010 status + Phase 3b note,
  API_CONTRACT_INVENTORY (`runtime` kind + endpoint), CLI_CONTRACT_MATRIX,
  AUTH_GUIDE, OBSERVABILITY_BASELINE, DEVELOPER_MANUAL, CHANGELOG (minor), and
  3 website pages (drift guard 0/0/0, build clean).

### Done (earlier — see prior archives)

- **ADR-010 Phase 3a — effective-config tooling** (`config print --effective`
  + `LoadEffective` + redaction fix; commit `7a416ce`, 2026-05-22 →
  `docs/iterations/2026-05-22-adr010-phase3a-effective-config.md`).
- **Website coverage manifests** (commit `bbc7d60`, 2026-05-22 →
  `docs/iterations/2026-05-22-website-coverage-manifests.md`).

- **`pkg/observability` (+`/hooks`) lifecycle = experimental** (commit
  `9227e7d`, 2026-05-22 →
  `docs/iterations/2026-05-22-observability-lifecycle-experimental.md`).
- **Nested-package contract coverage** (commit `1233bf4`, 2026-05-22 →
  `docs/iterations/2026-05-22-nested-package-contract-coverage.md`).
- **Shared package-enumeration registry** (commit `6e6a075`, 2026-05-22 →
  `docs/iterations/2026-05-22-shared-package-enumeration-registry.md`).
- **Website refresh + website-curator subagent** (commits `3ca91ce`,
  `5a79095`, 2026-05-21 → `docs/iterations/2026-05-21-website-refresh-and-curator.md`).
- **Freeze-scanner package-coverage gap** (combined `fix(contracts)` commit,
  2026-05-21 → `docs/iterations/2026-05-21-freeze-scanner-coverage-gap.md`).
- **Admin bootstrap DDL dialect-aware fix** (PR #78 → `2975108`).
- **Freeze-scanner constructor-gap fix** (PR #77 → `28f75b2`).
- **ADR-010 §2 config loader feature-complete** (Phases 2a–2d, PRs
  #73–#76).
- v0.7.0 (PRs #56–#59); CSRF hardening (ADR-006); slog redaction
  (ADR-007); CSRF follow-ups + schema drift (ADR-008 + ADR-009);
  MSSQL/Oracle SchemaDrift (#66); pkg/app+pkg/nucleus inventory (#65);
  ADR-010 Phase 1 + examples purge (#71).

### In progress

- (none)

### Blocked

- (none)

## Candidate next steps (priority order, pending owner confirmation)

_Carry-forward follow-ups (low priority, not blocking):_

- **GCS credential redaction forward-compat** (security-auditor 2026-05-23,
  Phase 3b). Today `app.Config.Storage.GCS` is an anonymous struct with only
  `bucket`/`public_bucket` — safe. If a future iteration wires the richer
  `pkg/storage.GCSConfig` (nested `CredentialSource` → flattens to
  `storage.gcs.credentials.value`, leaf `value`) into `app.Config`, that leaf
  is NOT in `observe.DefaultRedactedKeys()` and would leak via `/_/config` +
  logs. Add `value` (or a structural rule) to the canonical set in the same PR
  that lands the type change.
- **Reverse-proxy hardening note for `/_/config`** (doc-updater 2026-05-23).
  `docs/guides/DEPLOYMENT_GUIDE.md` production checklist could note that
  `/_/config` (like `/metrics`) is best blocked at the reverse-proxy for
  non-internal traffic as defence-in-depth. Owner call — left out to keep the
  Phase 3b diff focused.
- **Relocate `pkg/observability` to `internal/` post-v1.0** rather than ever
  promoting it to `stable` (architect-reviewer 2026-05-22). It is
  internal-facing plumbing; `experimental` buys time, but the eventual right
  move is relocation, tracked for the Phase 4 modularization pass.
- `discoverPublicPackages` double-reads each dir (WalkDir + `hasGoSource`'s
  `os.ReadDir`); could accumulate from the walk callback's `DirEntry`.
- The `*ast.InterfaceType` unexported-skip branch in `checkTypeSpecForLeaks`
  is effectively a no-op (cross-package interfaces can't carry unexported
  methods) — kept for symmetry.

_(The website coverage-manifests item — former candidate #1 — landed
2026-05-22.)_

1. **ADR-010 Phase 3b — auth-gated `/_/config` endpoint.** Direct follow-on
   from Phase 3a (landed 2026-05-22). Mount `GET /_/config` from the nucleus
   layer onto `App.Router`, gated on `App.Admin != nil`, wrapped with
   `admin.NewDatabaseAdminAuth(App.DefaultDB(), App.Session, App.Config.AdminPrefix)`
   over the app-wide Casbin default-deny (note: `WithAdmin()` from the ADR
   does NOT exist). Needs the effective-config snapshot threaded from the
   builder into `Run` (via a new `App` field) + a story for the direct-struct
   `Run(App{})` path (no file paths). Integration tests: 403 anon, 200 admin
   session, absent under `WithoutDefaults`. Reuses `LoadEffective` + the
   redaction helper from 3a. Medium effort, security-sensitive.

2. **ADR-010 Phase 3.1 — env-layer attribution + `file:line` provenance.**
   Wire the env config-value layer into the nucleus `loadFromFiles` path so
   `[env:NUCLEUS_*]` sources are real, and add line-aware parsing (YAML
   `yaml.Node`, TOML positions; JSON has no standard line API) so sources show
   `:line`. Owner deferred both from 3a. Larger; 3 format-specific walkers.

3. **Oracle model-scaffold identifier-casing (opened by PR #78).**
   `BuildOracleMigrationScaffold` quotes identifiers
   (`CREATE TABLE "ci_automig_live_users"` → case-sensitive lowercase),
   diverging from the unquoted-uppercase convention the rest of the
   Oracle path uses and `USER_TAB_COLUMNS` introspection expects. Blocks
   the Oracle `TestSQLMatrix_AutoMigrate_Exploratory` lane (deferred
   with a NOTE breadcrumb in `.github/workflows/ci.yml`). Needs a
   decision on the framework's Oracle identifier strategy
   (quoted-lowercase vs. unquoted-uppercase) incl. reserved-word and
   query/CRUD-layer implications — likely an ADR. When it lands, re-add
   the Oracle AutoMigrate_Exploratory test line.

5. **Oracle multi-block AutoMigrate execution (opened by PR #78).**
   Scaffolds for models with secondary indexes emit multiple
   `BEGIN…END;` PL/SQL blocks; the single-`Exec` AutoMigrate path (and
   the file Migrator's `tx.Exec`) can't run them as one batch. Needs a
   statement-splitting executor.

6. **ADR-010 Phase 3 — `/_/config` + `nucleus config print
   --effective`.** Compliance items #6, #12, #13. Auth-gated by
   `WithAdmin()` (Casbin default-deny); redaction via
   `observe.DefaultRedactedKeys()`. Requires per-key source tracking the
   Phase 2 loader does not yet capture.

7. **`session_cookie_secure` default `false`** (Phase 2b security-
   auditor MED-1). Pre-existing security default; the non-nullable
   mechanism doesn't cover it (default already permissive). Flip to
   `true` or add to the non-nullable set.

8. **ADR-010 §2 layer 3 — field-semantic validation** (ranges, enums,
   parseable durations; ADR-010 §96 layer 3). Standalone follow-up on
   the now-complete merge engine.

9. **ADR-010 Phase 4 — Docs-sync + website + new reference applications
   under a freshly-scoped `examples/`.** Target: v0.9.X. Also unblocks
   candidate #3 (extract inline website code examples into `examples/*`
   via raw-loader once reference apps exist).

10. **Cloud Secrets Provider plugin extraction (AWS → GCP → Azure →
    Vault).** Removes AWS SDK from core `go.mod`.

11. **Column-type comparison in `SchemaDrift`.** Cross-dialect
    type-family compatibility table.

12. **SchemaDrift end-to-end usage guide** in
    `docs/guides/MODELING_MULTI_DATABASE.md`.

13. **`go mod tidy` unblock** (admin/proto replace-directive).

14. **`tasks.Manager` struct→interface DEP** (optional DEP-2026-004).

15. **Audit §7 menores** — 503 path test for `/healthz`,
    endpoints-parity doc-parsing, `pkg/health/{db,redis,storage}.go`
    tests.

16. **(Optional) Promote the advisory `website-drift` CI job to a
    required gate.** Once manifests (candidate #3) exist and the job has
    proven stable over several pushes. Owner call.

## Carry-forward follow-ups (ADR-010 Phase 1, still open)

- **Service-shutdown timeout** — `nucleus.Run`'s `wg.Wait()` after
  `cancelServices()` has no deadline.
- **`Lifecycle.OnShutdown` context deadline** — derived from
  `context.Background()` with no bound.
- **`joinPath` double-slash collapse** — `routerAdapter.joinPath`
  produces `/x/x/123` when `prefix=/x` and `p=/x/123`.

## Files of interest

- `.claude/agents/website-curator.md` — new subagent owning
  `website/docs/**`, manifests, drift guard, site build.
- `.claude/agents/doc-updater.md` — narrowed to internal docs + godoc.
- `scripts/website/check-coverage.sh` — heuristic drift guard.
- `.github/workflows/ci.yml` — advisory `website-drift` job; Oracle
  AutoMigrate_Exploratory NOTE breadcrumb.
- `contracts/packages_test.go` — shared `allPublicPackages()` registry
  (single source of truth) + `frozenPackages()`/`firewalledPackages()`
  filters + two guard tests; candidate #1 (nested coverage) extends
  `discoverTopLevelPublicPackages` here.
- `contracts/freeze_test.go` — derives its set from `frozenPackages()`.
- `contracts/firewall_test.go` — derives its set from `firewalledPackages()`.
- `contracts/baseline/api_exported_symbols.txt` — frozen API baseline;
  rebaseline via `NUCLEUS_UPDATE_CONTRACT_BASELINE=1` after a `stable`
  promotion.
- `docs/reference/API_CONTRACT_INVENTORY.md` — Freeze Enforcement coupled-
  change note.
- `pkg/model/migration_scaffold_oracle.go` — candidate #4 target
  (identifier quoting).
- `pkg/nucleus/config.go`, `pkg/nucleus/nucleus.go` — Phase 2 loader
  (candidate #6 starting point).

## Notes / decisions log

- 2026-05-23 — ADR-010 Phase 3b (auth-gated `/_/config` endpoint) implemented.
  New `pkg/nucleus/config_endpoint.go`: `mountConfigEndpoint` (admin-session
  gate via `admin.NewDatabaseAdminAuth`; `Cache-Control: no-store`; JSON
  `EffectiveConfig`), `effectiveFromConfig` runtime fallback (flattens live
  `core.Config` into a `"runtime"`-kind snapshot). `nucleus.go`: unexported
  `App.effective *EffectiveConfig` captured in `FromConfigFile`, mounted in
  `Run`. Casbin default-deny exemption added via
  `core.Authorizer.AddPolicy(authz.BootstrapSubject, "/_/config", "*")` at
  `Run`-time — no edit to stable `pkg/authz.BootstrapAllowList`. Security
  hardening: `pkg/observe` canonical set extended with
  `access_key_id`/`aws_access_key_id` (closes a real S3 access-key-ID
  cleartext leak in both logs and `/_/config`). 7 tests in
  `config_endpoint_test.go`. Deliberate redaction non-decision: AWS
  access-key IDs ARE redacted; public identifiers (`account_name`,
  `smtp_user`, admin bootstrap username/email) are deliberately NOT
  (documented in redact.go). Loop verdicts: architect WARN→addressed (docs),
  code-reviewer NITS→addressed (pointer receiver, DefaultDB nil-warn, cloneApp
  comment, +2 tests), security FAIL→FIXED→re-audit PASS, contract-guardian
  PASS (no exported-symbol change; baselines untouched), test-runner PASS
  (full suite + race + contract freeze all green; website drift guard 0/0/0;
  `npm run build` clean). Docs updated: ADR-010 (status landed 2026-05-23),
  API_CONTRACT_INVENTORY (`runtime` ConfigSource.Kind + `/_/config` endpoint),
  CLI_CONTRACT_MATRIX, AUTH_GUIDE, OBSERVABILITY_BASELINE, DEVELOPER_MANUAL,
  CHANGELOG (Added + Security; semver minor). Website: 3 pages updated
  (concepts/configuration.md, features/admin.md, features/observability.md).
  Change set complete and verified; UNCOMMITTED (two-commit pattern:
  `feat(nucleus): auth-gated /_/config endpoint (ADR-010 Phase 3b)` +
  `chore(state): close ADR-010 Phase 3b iteration`).
- 2026-05-22 — ADR-010 Phase 3a (effective-config tooling) implemented.
  `loadFromFiles`→`loadMerged` refactor adds per-key provenance
  (snapshot-and-diff; default vs file; null-revert→default). New stable
  `pkg/nucleus` API `LoadEffective` + `ConfigSource`/`EffectiveValue`/
  `EffectiveConfig` (baseline +11, additive). New CLI `config print
  --effective`. Owner-confirmed scope: source-kind+path only (env-layer +
  file:line → Phase 3.1); `/_/config` endpoint → Phase 3b (the ADR's
  `WithAdmin()` gate does not exist — 3b will gate on `App.Admin != nil` +
  reuse `admin.NewDatabaseAdminAuth`). Security-auditor caught a real
  redaction gap (jwt_secret, admin_bootstrap_password, etc. printed in
  cleartext — flat compound leaves not matched by short atomic keys); FIXED
  by extending `observe.DefaultRedactedKeys()` (one canonical surface; fixes
  logs too). `config` documented `transitional` in CLI_CONTRACT_MATRIX — NOT
  added to the stable cli_primary_commands freeze baseline (consistent with
  the frozen⟺stable principle; freeze it when 3b stabilises the surface).
  Loop: architect/contract-guardian WARN→addressed (docs), code-reviewer
  NITS, security WARN→fixed, test-runner PASS. Pending commit.
- 2026-05-22 — Website coverage manifests (candidate #1) added via the
  website-curator subagent: `covers:`/`config_keys:` frontmatter on all 14
  previously-unmanifested `website/docs/` pages. Key constraint discovered:
  the drift guard's check #2 scans the full page body (not just frontmatter)
  for `pkg/<pkg>.<Symbol>` tokens and validates each against the freeze
  baseline, so `covers:` may only list STABLE symbols — experimental/
  transitional surfaces (observability, openapi, admin, outbox, providers)
  are deliberately excluded. `config_keys:` is NOT guard-validated but was
  kept honest against `CONFIG_KEY_REGISTRY.md`. Verified independently:
  `check-coverage.sh --strict` → 0/0/0, frontmatter-only diff across 14
  pages, `npm run build` clean, covers/config-keys spot-checked against the
  baseline + registry. Website-only — no CHANGELOG/contracts/code change.
  Pending commit.
- 2026-05-22 — `pkg/observability` (+`/hooks`) inventory + lifecycle
  (candidate #1) implemented. Owner chose `experimental` over a new
  `internal-facing` tag (would have been a taxonomy/governance change,
  arguably an ADR; `experimental` matches the `pkg/openapi` precedent and
  needs no new tag). Two `experimental` rows added to
  `API_CONTRACT_INVENTORY.md`; registry flipped `uninventoried` →
  `experimental` for both (still frozen:false/firewalled:false → baseline
  untouched, freeze 17 / firewall 20 unchanged). `lifecycleUninventoried`
  const kept as the placeholder for future newly-discovered packages.
  Architect backlog note: relocate `pkg/observability` to `internal/`
  post-v1.0 rather than promote to `stable` (Phase 4 modularization). Loop:
  architect PASS, code-reviewer PASS, contract-guardian PASS, test-runner
  PASS. No CHANGELOG/website — internal-facing, no user-facing change.
  Pending commit.
- 2026-05-22 — Nested-package contract coverage (candidate #1) implemented.
  Recursive `discoverPublicPackages` + 4 nested registry rows (owner-confirmed
  postures: secrets/asynq/memory = transitional, hooks = uninventoried; none
  frozen → baseline untouched). Owner chose "add AWS + enforce": added
  `aws-sdk-go-v2/config` + `.../service/secretsmanager` to the firewall
  forbidden map (ADR-005, Accepted). Adding `asynqprovider` to the firewall
  surfaced a latent over-strictness — `checkTypeSpecForLeaks` flagged
  forbidden types in UNEXPORTED struct fields, contrary to the firewall's
  "public surface" spec. Fixed to skip unexported named fields/methods while
  keeping embedded fields checked (`anyExported`). Security-auditor confirmed
  no leak vector opens (exported accessors/methods + embedded fields stay
  covered). Freeze set 17 / firewall set 20. Loop: architect PASS,
  code-reviewer NITS (interface-branch comment added; double-ReadDir +
  dead-branch deferred as optional cleanups), security PASS, contract-guardian
  PASS, test-runner PASS. Pending commit.
- 2026-05-22 — Shared package-enumeration registry (candidate #1)
  implemented. `contracts/packages_test.go` is now the single source of
  truth; freeze + firewall derive their sets via `frozen`/`firewalled`
  filters. Behaviour-preserving (baseline untouched, freeze set 17 /
  firewall set 18 unchanged). Two guard tests added:
  registry⟺filesystem match (machine-visible omissions) and
  frozen⟺lifecycle==stable invariant. Scope deliberately top-level only;
  nested-package coverage promoted to new candidate #1. Loop verdicts:
  architect PASS, code-reviewer NITS (gofmt + double-call + build-tag note
  all fixed), contract-guardian PASS, test-runner PASS (`go test ./...`
  green). No CHANGELOG / docs / website — internal test tooling only, no
  user-facing change. Landed as feature commit `6e6a075`; archived to
  `docs/iterations/2026-05-22-shared-package-enumeration-registry.md`.
- 2026-05-21 — Website refresh + website-curator subagent landed as two
  commits (`3ca91ce`, `5a79095`) on `origin/main`. Public site now
  reflects shipped Nucleus behaviour. Drift guard live (advisory CI).
  website-curator wired into iteration loop and commands. Two-docs-tree
  rule codified in subagent definitions and user memory. Permission rule
  for `.claude/` self-modification in gitignored `.claude/settings.local.json`.
  Three new follow-up candidates added (#3 covers: manifests, #9 note
  updated re: raw-loader tie-in, #16 optional required-gate promotion).
- 2026-05-21 — Freeze-scanner package-coverage gap landed as combined
  `fix(contracts)` commit on `main`. pkg/circuit + pkg/health now frozen;
  firewall scan covers admin/health/nucleus. Architect-reviewer endorsed
  the firewall expansion as in-bounds. circuit/health were already
  `stable` — only the removal-protection was missing, no lifecycle change.
  Two new follow-up candidates (#1 shared pkg-enum helper, #2 observability
  inventory entry) added per architect-reviewer findings.
- 2026-05-20 — PR #78 (admin bootstrap DDL + Oracle scaffold `/`).
  Discovered a chain of 4 latent Oracle bugs; fixed 2, de-scoped 2
  (#4 identifier-casing, #5 multi-block exec) as their own candidates.
- 2026-05-20 — Freeze-scanner constructor-gap fix (PR #77); ADR-010 §2
  complete (Phases 2b/2c/2d).
