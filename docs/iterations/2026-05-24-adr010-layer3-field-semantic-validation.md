# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

**ADR-010 §2 layer-3 field-semantic validation** (started 2026-05-24). Add the
layer-3 (field-semantic: ranges, enums, parseable durations) validator to the
config loader, completing ADR-010 §2's five-layer validator (layers 1–2 already
shipped). Owner-confirmed approach: a hand-written `validateSemantics(cfg)` in
the nucleus layer (FromConfigFile + Run), failing early and loud where today
some values are silently mishandled.

## Scope

- in: new `validateSemantics(*app.Config) error` + `ErrInvalidConfigValue`
  sentinel in `pkg/nucleus`; called in `FromConfigFile` (fail-fast at load) and
  `Run` (covers direct-struct). Validated fields (exact sets pinned from the
  consumers): **enums** `session_store`∈{memory,sql,redis}, `log_level`∈{debug,
  info,warn,warning,error}, `log_format`∈{json,text}, `session_cookie_samesite`
  ∈{strict,lax,none} (empty allowed → default); **ranges** `port`/`smtp_port`∈
  [0,65535] (0 = auto/unset, allowed — tests rely on it), `rate_limit_requests`/
  `rate_limit_burst`≥0; **durations** read/write/idle timeouts, jwt_expiry,
  session_lifetime, session_idle_timeout, rate_limit_window ≥0 (reject negative).
- out: `mail_driver`/`storage.provider` (plugin-extensible / already validated
  downstream), `env` (freeform label), `multitenant.resolver` (already
  auto-normalised by NormalizeRuntimeConfig — validating it is a no-op). Not in
  `app.New` (ADR scopes layer-3 to the nucleus layer; avoids changing pkg/app).

## Acceptance criteria

- [ ] Invalid enum/range/duration values fail early with an actionable
      `ErrInvalidConfigValue` (offending field + value + valid set/range).
- [ ] Runs in both `FromConfigFile` (builder, at load) and `Run` (direct-struct).
- [ ] Zero-value / default / Port:0 configs still pass (empties + zeros allowed).
- [ ] `go test ./...` green (no existing test broken); iteration loop clean.

## Status

### In progress (this iteration)

- (none — complete, pending commit)

### Done (this iteration)

- **ADR-010 §2 layer-3 field-semantic validation** (2026-05-24, pending commit).
  New `pkg/nucleus/validate_semantics.go`: `validateSemantics(*app.Config)` +
  exported `ErrInvalidConfigValue`. Validates 4 enums (session_store, log_level,
  log_format, session_cookie_samesite — case-insensitive, empty→default), 2
  port ranges [0,65535] (0 allowed), 2 non-negative rate-limit counts, 7
  non-negative durations. Wired into `FromConfigFile` (fail-fast at load) and
  `Run` (direct-struct; idempotent re-check that also catches post-Build
  programmatic mutations). Completes ADR-010 §2's five-layer validator. Additive
  freeze rebaseline (+`ErrInvalidConfigValue`). Loop: architect PASS (placement/
  scope/behaviour-change all sound; no new ADR), code-reviewer NITS→addressed
  (validatePort message no longer mis-claims "OS-assigned" for smtp_port; `%v`
  duration fmt; +4 rejection test rows + warning-alias valid case; double-
  validation rationale comment), contract additive + freeze PASS, test-runner
  PASS. Docs: ADR-010 status + §2 layer-3 note, CHANGELOG (Added + behaviour
  note), API_CONTRACT_INVENTORY sentinel list, observe godoc (`warning` alias).

### Blocked
- (none)

## Most recent completed iteration

- **Oracle multi-block AutoMigrate execution** (2026-05-24, COMPLETE —
  committed + pushed `d46d29c` + state close commit) →
  `docs/iterations/2026-05-24-oracle-multiblock-automigrate.md`
- **`session_cookie_secure` secure-by-default** (2026-05-23, COMPLETE —
  committed + pushed `243ff1a` + `345cc0e`) →
  `docs/iterations/2026-05-23-session-cookie-secure-default.md`
- **Oracle model-scaffold identifier-casing → unquoted-uppercase + ADR-011**
  (2026-05-23, COMPLETE — committed + pushed `9a45373` + `df9e246`) →
  `docs/iterations/2026-05-23-oracle-identifier-casing-adr011.md`

## Candidate next steps (priority order, pending owner confirmation)

_Carry-forward follow-up from layer-3 validation (2026-05-24):_

- **Referential check: `smtp_port` must be > 0 when `mail_driver=smtp`.**
  (code-reviewer, 2026-05-24.) Layer-3 allows `smtp_port: 0` (unset) since the
  mail subsystem already rejects it loudly at init — but only when the smtp
  driver is selected. A layer-4 (referential, cross-field) check could catch it
  at config load. Deferred: it couples `smtp_port` to `mail_driver` (layer-4,
  not layer-3) and the downstream error is already clear. Fold into the
  ADR-010 §2 layer-4 (referential) work if/when that lands.

_Carry-forward follow-ups from Oracle multi-block AutoMigrate (2026-05-24):_

- **Route admin-bootstrap PL/SQL through `db.ExecScript`.** (architect NIT,
  2026-05-24.) `pkg/admin/bootstrap_admin.go`'s `ensureBootstrapAdminUsersTable`
  Execs a single-block Oracle PL/SQL DDL directly (safe today — one block), but
  it bypasses `ExecScript`, so a future second block would silently fail. Route
  it through `ExecScript` to make "all Oracle PL/SQL goes through the splitter"
  an unconditional invariant.
- **Oracle DDL-auto-commit vs the Migrator transaction.** (code-reviewer,
  2026-05-24.) `applyMigration`/`rollbackMigration` wrap DDL + tracking-row
  inserts in a `*sql.Tx`, but Oracle DDL auto-commits, so the two are not atomic
  on Oracle (a failure after a committed DDL block leaves it applied). Pre-
  existing; flagged with a caveat comment. Tightening (non-transactional DDL +
  separate DML tx for Oracle) is a follow-up.
- **(Optional) export the `ExecScript` execer interface.** Currently unexported
  (`*sql.DB`/`*sql.Tx` satisfy it). Export it only if external callers ever need
  to pass a custom execer; backward-compatible to add later.

_Carry-forward follow-up from session_cookie_secure (2026-05-23):_

- **Startup validation: `SameSite=None` requires `Secure=true`.** (security-auditor
  LOW, 2026-05-23.) `pkg/auth/session.go` does not reject the
  `session_cookie_samesite: none` + `session_cookie_secure: false` combination,
  which browsers reject (cookie dropped). With the new secure default this needs
  a deliberate double opt-out, so blast radius is small — but a validation error
  in `NewSessionManager` / `buildSessionManager` would catch the silent misconfig.

_Carry-forward follow-ups from Oracle identifier-casing (2026-05-23):_

- **CI governance reconciliation (mssql + oracle): required vs exploratory.**
  (architect-reviewer 2026-05-23.) PRE-EXISTING contradiction surfaced this
  iteration: `.github/workflows/ci.yml` lists `db-matrix-live-mssql` and
  `db-matrix-live-oracle` in `ci-required-gate.needs` and fails the gate if
  either does not succeed (lines ~389-422), while
  `docs/governance/CI_MATRIX.md` (lines 15-16, 135) classifies both as
  "exploratory, non-blocking". Owner call: either promote both to required in
  CI_MATRIX (now defensible — the Oracle casing bug is fixed) or remove them
  from the required gate. Not changed this iteration (out of scope; owner
  decision).
- **Oracle reserved-word + dotted-identifier hardening.** (architect WARN-2 +
  security-auditor LOW, 2026-05-23.) `isValidIdentifierLike` (`pkg/model/meta.go`,
  now carries a `TODO(ADR-011 follow-up)`) accepts Oracle reserved words
  (`comment`/`number`/`date`/…) which break unquoted Oracle DDL/queries, and
  accepts `.` for all identifiers (intended for FK refs) which lets a dotted
  table name through as schema-qualified DDL. Both pre-existing (the bare CRUD
  layer already had them). Fix: selective quoting at the `oracleIdentifier`
  choke point + the CRUD layer, and split the allowlist (name = no dot; FK ref
  = dot allowed).

_Carry-forward follow-ups from Phase 3.1 (low priority, not blocking):_

- **Doc sweep side-effects** (doc-updater 2026-05-23, Phase 3.1). The
  env-var doc pass fixed pre-existing wrong examples (single-underscore
  `NUCLEUS_*` patterns, `session_cookie_samesite` key) in
  DEPLOYMENT_GUIDE and AUTH_GUIDE. No follow-up needed; captured for
  awareness only.

_Carry-forward follow-ups from Phase 3b (low priority, not blocking):_

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

_Prioritised candidate list (owner to confirm next):_

1. **ADR-010 §2 layer 3 — field-semantic validation** (ranges, enums,
   parseable durations; ADR-010 §96 layer 3). Standalone follow-up on
   the now-complete merge engine.

2. **ADR-010 Phase 4 — Docs-sync + website + new reference applications
   under a freshly-scoped `examples/`.** Target: v0.9.X. Also unblocks
   candidate #1 (extract inline website code examples into `examples/*`
   via raw-loader once reference apps exist).

3. **Cloud Secrets Provider plugin extraction (AWS → GCP → Azure →
   Vault).** Removes AWS SDK from core `go.mod`.

4. **Column-type comparison in `SchemaDrift`.** Cross-dialect
   type-family compatibility table.

5. **SchemaDrift end-to-end usage guide** in
   `docs/guides/MODELING_MULTI_DATABASE.md`.

6. **`go mod tidy` unblock** (admin/proto replace-directive).

7. **`tasks.Manager` struct→interface DEP** (optional DEP-2026-004).

8. **Audit §7 menores** — 503 path test for `/healthz`,
   endpoints-parity doc-parsing, `pkg/health/{db,redis,storage}.go`
   tests.

9. **(Optional) Promote the advisory `website-drift` CI job to a
   required gate.** Once manifests exist and the job has proven stable
   over several pushes. Owner call.

## Carry-forward follow-ups (ADR-010 Phase 1, still open)

- **Service-shutdown timeout** — `nucleus.Run`'s `wg.Wait()` after
  `cancelServices()` has no deadline.
- **`Lifecycle.OnShutdown` context deadline** — derived from
  `context.Background()` with no bound.
- **`joinPath` double-slash collapse** — `routerAdapter.joinPath`
  produces `/x/x/123` when `prefix=/x` and `p=/x/123`.

## Files of interest

- (TBD — no active iteration)

## Notes / decisions log

- 2026-05-23 — Oracle identifier-casing iteration complete — COMMITTED +
  PUSHED (`9a45373` fix + `df9e246` state). Archive at
  `docs/iterations/2026-05-23-oracle-identifier-casing-adr011.md`. Key
  design facts: scaffold now emits UNQUOTED identifiers (Oracle folds to
  UPPER); `quoteOracleIdentifier` → `oracleIdentifier` pass-through (single
  choke point for reserved-word follow-up). Matches CRUD (`pkg/model/crud.go`
  bare identifiers), migrations bootstrap (`pkg/db/migrate.go` unquoted),
  introspection (`schema_drift.go` `UPPER(...)`). ADR-011 pins the strategy.
  No exported-symbol change — freeze PASS, baseline untouched. Oracle live
  lane can only be verified in CI (requires an Oracle container).
- 2026-05-23 — ADR-010 Phase 3.1 complete — COMMITTED + PUSHED (`d28094c` +
  `06f76df`). Archive at
  `docs/iterations/2026-05-23-adr010-phase3.1-env-and-fileline.md`. Key
  design facts: `applyEnvLayer` in `loadMerged` after file loop; same
  `env.Provider`/`__`→`.` transform as `app.LoadConfig`; schema-recognised
  keys only; empty non-nullable security key is `ErrSecurityKeyNotNullable`.
  `ConfigSource.Line int` additive; YAML-only via `go.yaml.in/yaml/v3` node
  walk; TOML/JSON no line; CLI renders `kind:path:line`. `go.yaml.in/yaml/v3`
  promoted indirect→direct, confined to unexported helpers — no ADR needed.
  Known limitation: `_append`/`_remove`-derived keys and anchor/merge-key-
  reached keys carry no line.
