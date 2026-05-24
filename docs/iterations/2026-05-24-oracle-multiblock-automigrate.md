# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

**Oracle multi-block AutoMigrate execution** (started 2026-05-24; PR #78
follow-up). Oracle AutoMigrate fails on models with secondary indexes because
the scaffold emits multiple `BEGIN…END;` PL/SQL blocks and go-ora executes only
one block per `Exec`. Fix via a `/`-delimited statement-splitting executor
(owner-confirmed approach) used by both `App.AutoMigrate` and the file Migrator.

## Scope

- in: scaffold (`pkg/model/migration_scaffold_oracle.go`) separates PL/SQL
  blocks with `/` on its own line; new exported `db.ExecScript(execer, system,
  script)` in `pkg/db` splits Oracle scripts on `/` lines (stripping the `/`,
  which go-ora rejects) and Execs each block, pass-through for other dialects;
  wire it into `App.AutoMigrate` (app.go:119) and Migrator
  `applyMigration`/`rollbackMigration` (migrate.go:603/654, via `m.db.system`).
- in: invert the prior no-`/` regression-guard test (the `/` is now an
  executor-stripped split marker — the "don't send `/` to go-ora" constraint
  still holds); splitter unit tests; add a secondary index to the live
  AutoMigrate fixture so CI exercises the multi-block path.
- out: a general multi-statement splitter for non-Oracle dialects (the
  pure-Go SQLite/modernc multi-statement limitation is noted as a possible
  follow-up; other drivers accept `;`-separated multi-statement today).

## Acceptance criteria

- [ ] Oracle scaffold with a secondary index produces `/`-separated blocks;
      `db.ExecScript` splits + Execs each (verified by unit test).
- [ ] `App.AutoMigrate` and the file Migrator both route Oracle scripts through
      `ExecScript`; non-Oracle behaviour unchanged (pass-through).
- [ ] Live AutoMigrate fixture gains a secondary index (CI Oracle lane exercises
      the multi-block path).
- [ ] `go test ./...` green; iteration loop clean (architect on the `/` reversal).

## Status

### In progress (this iteration)

- (none — complete, pending commit)

### Done (this iteration)

- **Oracle multi-block AutoMigrate execution** (2026-05-24, pending commit).
  New exported `db.ExecScript(execer, system, script)` (+ unexported
  `splitOracleStatements`): Oracle scripts split on `/`-terminator lines
  (stripped — go-ora rejects `/` and runs one PL/SQL block per Exec); other
  dialects pass through to a single Exec. The scaffold (`writeOraclePLSQLBlock`)
  now emits `/` after each block — refines (doesn't revert) the prior `/`-removal
  fix, since the executor strips `/` before the driver. Wired into
  `App.AutoMigrate` (app.go) and the file Migrator apply+rollback (migrate.go,
  via `m.db.system`). Live AutoMigrate fixture gained a secondary index so the
  Oracle CI lane exercises multi-block. Additive freeze rebaseline
  (+`db.ExecScript`). CRLF-normalised splitter (Windows .up.sql). Loop:
  architect WARN→addressed (godoc on unexported execer; no new ADR), code
  NITS→addressed (CRLF fix+test, inline-`/` test, faithful test constant,
  tx-auto-commit caveat comment), contract-guardian PASS (additive; firewall
  clean; inventory updated), test-runner PASS (full suite + freeze). Docs:
  CHANGELOG (Fixed resolves Oracle follow-up #2 + Added db.ExecScript),
  API_CONTRACT_INVENTORY.

### Blocked
- (none)

## Most recent completed iteration

- **`session_cookie_secure` secure-by-default** (2026-05-23, COMPLETE —
  pending owner commit; see HANDOFF.md two-commit sequence) →
  `docs/iterations/2026-05-23-session-cookie-secure-default.md`
- **Oracle model-scaffold identifier-casing → unquoted-uppercase + ADR-011**
  (2026-05-23, COMPLETE — committed + pushed `9a45373` + `df9e246`) →
  `docs/iterations/2026-05-23-oracle-identifier-casing-adr011.md`

## Candidate next steps (priority order, pending owner confirmation)

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

1. **Oracle multi-block AutoMigrate execution (opened by PR #78).**
   Scaffolds for models with secondary indexes emit multiple
   `BEGIN…END;` PL/SQL blocks; the single-`Exec` AutoMigrate path (and
   the file Migrator's `tx.Exec`) can't run them as one batch. Needs a
   statement-splitting executor.

2. **ADR-010 §2 layer 3 — field-semantic validation** (ranges, enums,
   parseable durations; ADR-010 §96 layer 3). Standalone follow-up on
   the now-complete merge engine.

3. **ADR-010 Phase 4 — Docs-sync + website + new reference applications
   under a freshly-scoped `examples/`.** Target: v0.9.X. Also unblocks
   candidate #2 (extract inline website code examples into `examples/*`
   via raw-loader once reference apps exist).

4. **Cloud Secrets Provider plugin extraction (AWS → GCP → Azure →
   Vault).** Removes AWS SDK from core `go.mod`.

5. **Column-type comparison in `SchemaDrift`.** Cross-dialect
   type-family compatibility table.

6. **SchemaDrift end-to-end usage guide** in
   `docs/guides/MODELING_MULTI_DATABASE.md`.

7. **`go mod tidy` unblock** (admin/proto replace-directive).

8. **`tasks.Manager` struct→interface DEP** (optional DEP-2026-004).

9. **Audit §7 menores** — 503 path test for `/healthz`,
   endpoints-parity doc-parsing, `pkg/health/{db,redis,storage}.go`
   tests.

10. **(Optional) Promote the advisory `website-drift` CI job to a
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
