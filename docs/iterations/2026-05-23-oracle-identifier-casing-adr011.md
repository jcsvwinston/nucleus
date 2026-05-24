# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

**Oracle model-scaffold identifier-casing — fix to unquoted-uppercase + ADR-011**
(started 2026-05-23; PR #78 follow-up, candidate #1). `BuildOracleMigrationScaffold`
quotes every identifier (`CREATE TABLE "users"` → case-sensitive lowercase),
but the rest of the Oracle path uses UNQUOTED identifiers (Oracle folds to
UPPERCASE): the CRUD runtime layer (`pkg/model/crud.go`) emits bare identifiers,
the migrations/checksums bootstrap (`pkg/db/migrate.go`) creates tables
unquoted, and introspection (`schema_drift.go`, `automigrate_live_test.go`)
matches via `UPPER(...)`. A quoted-lowercase table is invisible to all of them.

## Scope

- **ADR-011** pinning the framework's Oracle identifier strategy:
  **unquoted-uppercase** (Oracle's natural folding; matches CRUD + migrations +
  introspection + DBA norms). Documents the rationale, the rejected
  quoted-everywhere alternative, and the reserved-word caveat.
- Fix `BuildOracleMigrationScaffold` to emit unquoted identifiers (drop
  `quoteOracleIdentifier`); update its doc comment.
- Update scaffold tests (they assert quoted output) + the now-stale Oracle
  comment in `pkg/db/schema_drift.go` (it claims the scaffold double-quotes).
- Re-enable the Oracle `TestSQLMatrix_AutoMigrate_Exploratory` CI lane line +
  update the NOTE breadcrumb in `.github/workflows/ci.yml`.
- out: **reserved-word handling** (pre-existing, broader — the bare-identifier
  CRUD path already breaks on a column named `comment`/`number`/etc.; tracked
  as a separate follow-up). Also out: quoting the CRUD/migrations/introspection
  layers (rejected alternative).

## Acceptance criteria

- [ ] ADR-011 written and Accepted.
- [ ] Scaffold emits unquoted identifiers; `CREATE TABLE articles (...)` not
      `"articles"`. Output round-trips with `UPPER(...)` introspection.
- [ ] Scaffold tests updated; `go test ./...` green.
- [ ] CI Oracle `AutoMigrate_Exploratory` line re-enabled; NOTE updated.
- [ ] Iteration loop clean; CHANGELOG + docs updated.

## Status

### In progress (this iteration)

- (none — complete, pending commit)

### Done (this iteration)

- **Oracle model-scaffold identifier-casing → unquoted-uppercase + ADR-011**
  (2026-05-23, pending commit). `BuildOracleMigrationScaffold` now emits
  UNQUOTED identifiers (Oracle folds to UPPER), matching the CRUD layer,
  migrations bootstrap, and `USER_TAB_COLUMNS` introspection — a scaffolded
  table is no longer invisible to the rest of the framework. `quoteOracleIdentifier`
  → `oracleIdentifier` pass-through (the single choke point for the future
  reserved-word follow-up). Corrected the stale `schema_drift.go` Oracle
  comment; re-enabled the Oracle `TestSQLMatrix_AutoMigrate_Exploratory` CI
  lane. ADR-011 written + Accepted. No exported-symbol change (freeze PASS).
  Loop: architect WARN→addressed (CI-governance finding surfaced as follow-up;
  reserved-word TODO added), code-reviewer NITS→addressed (down-script quote
  guard, pass-through test), security-auditor PASS (isValidIdentifierLike is
  the injection gate; quoting was never the defense; one pre-existing LOW
  dotted-identifier note tracked), test-runner PASS (full suite + freeze). Docs:
  ADR-011, CHANGELOG (Fixed), schema_drift comment, CI NOTE.

### Blocked
- (none)

## Most recent completed iteration

- **ADR-010 Phase 3.1 — env-layer attribution + `file:line` provenance**
  (2026-05-23, COMPLETE, pending owner commit — see two-commit sequence in
  HANDOFF.md) →
  `docs/iterations/2026-05-23-adr010-phase3.1-env-and-fileline.md`

## Candidate next steps (priority order, pending owner confirmation)

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

1. **Oracle multi-block AutoMigrate execution (opened by PR #78).**
   Scaffolds for models with secondary indexes emit multiple
   `BEGIN…END;` PL/SQL blocks; the single-`Exec` AutoMigrate path (and
   the file Migrator's `tx.Exec`) can't run them as one batch. Needs a
   statement-splitting executor.

2. **`session_cookie_secure` default `false`** (Phase 2b security-
   auditor MED-1). Pre-existing security default; the non-nullable
   mechanism doesn't cover it (default already permissive). Flip to
   `true` or add to the non-nullable set.

3. **ADR-010 §2 layer 3 — field-semantic validation** (ranges, enums,
   parseable durations; ADR-010 §96 layer 3). Standalone follow-up on
   the now-complete merge engine.

4. **ADR-010 Phase 4 — Docs-sync + website + new reference applications
   under a freshly-scoped `examples/`.** Target: v0.9.X. Also unblocks
   candidate #3 (extract inline website code examples into `examples/*`
   via raw-loader once reference apps exist).

5. **Cloud Secrets Provider plugin extraction (AWS → GCP → Azure →
   Vault).** Removes AWS SDK from core `go.mod`.

6. **Column-type comparison in `SchemaDrift`.** Cross-dialect
   type-family compatibility table.

7. **SchemaDrift end-to-end usage guide** in
   `docs/guides/MODELING_MULTI_DATABASE.md`.

8. **`go mod tidy` unblock** (admin/proto replace-directive).

9. **`tasks.Manager` struct→interface DEP** (optional DEP-2026-004).

10. **Audit §7 menores** — 503 path test for `/healthz`,
    endpoints-parity doc-parsing, `pkg/health/{db,redis,storage}.go`
    tests.

11. **(Optional) Promote the advisory `website-drift` CI job to a
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

- 2026-05-23 — ADR-010 Phase 3.1 complete (pending owner commit). Archive at
  `docs/iterations/2026-05-23-adr010-phase3.1-env-and-fileline.md`. Key
  design facts: `applyEnvLayer` in `loadMerged` after file loop; same
  `env.Provider`/`__`→`.` transform as `app.LoadConfig`; schema-recognised
  keys only; empty non-nullable security key is `ErrSecurityKeyNotNullable`.
  `ConfigSource.Line int` additive; YAML-only via `go.yaml.in/yaml/v3` node
  walk; TOML/JSON no line; CLI renders `kind:path:line`. `go.yaml.in/yaml/v3`
  promoted indirect→direct, confined to unexported helpers — no ADR needed.
  Known limitation: `_append`/`_remove`-derived keys and anchor/merge-key-
  reached keys carry no line.
