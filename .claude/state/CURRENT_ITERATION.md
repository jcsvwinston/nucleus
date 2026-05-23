# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

Awaiting direction — no active iteration. Owner to confirm the next candidate
from the priority list below.

## Scope

- in: (TBD — pending owner selection)
- out: (TBD)

## Acceptance criteria

- [ ] (TBD — pending owner selection)

## Status

### Done
- (none yet this iteration)

### In progress
- (awaiting direction)

### Blocked
- (none)

## Most recent completed iteration

- **ADR-010 Phase 3.1 — env-layer attribution + `file:line` provenance**
  (2026-05-23, COMPLETE, pending owner commit — see two-commit sequence in
  HANDOFF.md) →
  `docs/iterations/2026-05-23-adr010-phase3.1-env-and-fileline.md`

## Candidate next steps (priority order, pending owner confirmation)

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

1. **Oracle model-scaffold identifier-casing (opened by PR #78).**
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

2. **Oracle multi-block AutoMigrate execution (opened by PR #78).**
   Scaffolds for models with secondary indexes emit multiple
   `BEGIN…END;` PL/SQL blocks; the single-`Exec` AutoMigrate path (and
   the file Migrator's `tx.Exec`) can't run them as one batch. Needs a
   statement-splitting executor.

3. **`session_cookie_secure` default `false`** (Phase 2b security-
   auditor MED-1). Pre-existing security default; the non-nullable
   mechanism doesn't cover it (default already permissive). Flip to
   `true` or add to the non-nullable set.

4. **ADR-010 §2 layer 3 — field-semantic validation** (ranges, enums,
   parseable durations; ADR-010 §96 layer 3). Standalone follow-up on
   the now-complete merge engine.

5. **ADR-010 Phase 4 — Docs-sync + website + new reference applications
   under a freshly-scoped `examples/`.** Target: v0.9.X. Also unblocks
   candidate #3 (extract inline website code examples into `examples/*`
   via raw-loader once reference apps exist).

6. **Cloud Secrets Provider plugin extraction (AWS → GCP → Azure →
   Vault).** Removes AWS SDK from core `go.mod`.

7. **Column-type comparison in `SchemaDrift`.** Cross-dialect
   type-family compatibility table.

8. **SchemaDrift end-to-end usage guide** in
   `docs/guides/MODELING_MULTI_DATABASE.md`.

9. **`go mod tidy` unblock** (admin/proto replace-directive).

10. **`tasks.Manager` struct→interface DEP** (optional DEP-2026-004).

11. **Audit §7 menores** — 503 path test for `/healthz`,
    endpoints-parity doc-parsing, `pkg/health/{db,redis,storage}.go`
    tests.

12. **(Optional) Promote the advisory `website-drift` CI job to a
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
