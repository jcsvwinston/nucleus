# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

_Awaiting direction from the owner. No active iteration._

## Scope

- in: …
- out: …

## Acceptance criteria

- [ ] …

## Status

### Done
- (none yet — no active iteration)

### In progress
- (none)

### Blocked
- (none)

## Most recent completed iteration

- **ADR-010 Phase 3b — auth-gated `GET /_/config` endpoint** (2026-05-23,
  COMPLETE — pending commit by owner) →
  `docs/iterations/2026-05-23-adr010-phase3b-config-endpoint.md`

## Candidate next steps (priority order, pending owner confirmation)

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

1. **ADR-010 Phase 3.1 — env-layer attribution + `file:line` provenance.**
   Wire the env config-value layer into the nucleus `loadFromFiles` path so
   `[env:NUCLEUS_*]` sources are real, and add line-aware parsing (YAML
   `yaml.Node`, TOML positions; JSON has no standard line API) so sources show
   `:line`. Owner deferred both from 3a. Larger; 3 format-specific walkers.

2. **Oracle model-scaffold identifier-casing (opened by PR #78).**
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

3. **Oracle multi-block AutoMigrate execution (opened by PR #78).**
   Scaffolds for models with secondary indexes emit multiple
   `BEGIN…END;` PL/SQL blocks; the single-`Exec` AutoMigrate path (and
   the file Migrator's `tx.Exec`) can't run them as one batch. Needs a
   statement-splitting executor.

4. **`session_cookie_secure` default `false`** (Phase 2b security-
   auditor MED-1). Pre-existing security default; the non-nullable
   mechanism doesn't cover it (default already permissive). Flip to
   `true` or add to the non-nullable set.

5. **ADR-010 §2 layer 3 — field-semantic validation** (ranges, enums,
   parseable durations; ADR-010 §96 layer 3). Standalone follow-up on
   the now-complete merge engine.

6. **ADR-010 Phase 4 — Docs-sync + website + new reference applications
   under a freshly-scoped `examples/`.** Target: v0.9.X. Also unblocks
   candidate #3 (extract inline website code examples into `examples/*`
   via raw-loader once reference apps exist).

7. **Cloud Secrets Provider plugin extraction (AWS → GCP → Azure →
   Vault).** Removes AWS SDK from core `go.mod`.

8. **Column-type comparison in `SchemaDrift`.** Cross-dialect
   type-family compatibility table.

9. **SchemaDrift end-to-end usage guide** in
   `docs/guides/MODELING_MULTI_DATABASE.md`.

10. **`go mod tidy` unblock** (admin/proto replace-directive).

11. **`tasks.Manager` struct→interface DEP** (optional DEP-2026-004).

12. **Audit §7 menores** — 503 path test for `/healthz`,
    endpoints-parity doc-parsing, `pkg/health/{db,redis,storage}.go`
    tests.

13. **(Optional) Promote the advisory `website-drift` CI job to a
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

- `pkg/nucleus/config_endpoint.go` — new Phase 3b endpoint (UNCOMMITTED).
- `pkg/nucleus/config_endpoint_test.go` — 7 tests (UNCOMMITTED).
- `pkg/nucleus/nucleus.go` — unexported `App.effective` snapshot field (UNCOMMITTED).
- `pkg/observe/redact.go` — canonical redaction set extended with AWS access-key pair (UNCOMMITTED).
- `docs/iterations/2026-05-23-adr010-phase3b-config-endpoint.md` — this iteration's archive.
- `docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md` — Phase 3b design decisions.
- `docs/reference/API_CONTRACT_INVENTORY.md` — `runtime` ConfigSource.Kind + `/_/config` endpoint documented.
- `.claude/agents/website-curator.md` — subagent owning `website/docs/**`.
- `contracts/packages_test.go` — shared `allPublicPackages()` registry.
- `contracts/baseline/api_exported_symbols.txt` — frozen API baseline.

## Notes / decisions log

- 2026-05-23 — ADR-010 Phase 3b complete (pending commit). See archive at
  `docs/iterations/2026-05-23-adr010-phase3b-config-endpoint.md` for full
  decisions log. Key design facts: endpoint mounted from nucleus layer when
  `core.Admin != nil`; three defence-in-depth layers (mount gate → Casbin
  exemption via `AddPolicy` at runtime → admin-session check); `App.effective`
  threads snapshot builder→Run; direct-struct path falls back to
  `"runtime"`-kind snapshot; AWS access-key IDs redacted, public identifiers
  deliberately not.
