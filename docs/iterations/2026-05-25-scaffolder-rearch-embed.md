# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

**Scaffolder re-architecture — demo content out of core CLI strings into real
embedded files.** Owner observation (2026-05-25): `internal/cli/new.go` carries
~700 lines of the demo project's source as Go string literals — demo content
living in the framework core, never seen by the compiler (which is exactly why
the RBAC verb bug slipped past review and only `running` the scaffold caught it).
Owner chose **"embed de archivos reales"**: extract the scaffold templates to
real files under `internal/cli/scaffold/templates/{_common,api,mvc}/`
(`*.go.tmpl` + `.sql`/`.yaml`/`.html`/`.csv`), `//go:embed all:templates` them,
render via `text/template` (`{{.Module}}`/`{{.ProjectName}}`/`{{.Port}}`), and
reduce `new.go` to scaffolding LOGIC only (delete the string constants). Add a
test that renders both templates and `go build`s a rendered instance so the demo
is compiler-checked again. (Files live under `internal/cli` — not `examples/` —
because `go:embed` can only reach its own package subtree; owner accepted this.)

## Most recent completed iteration

- **Scaffolder re-architecture — embedded template files** (2026-05-25, COMPLETE —
  committed `625a85f` + state; pushed). Demo project source moved out of
  `internal/cli/new.go` (~990→170 lines; 21 string constants deleted) into real
  `//go:embed all:templates`-ed files under `internal/cli/scaffold/templates/`
  (`_common`/`api`/`mvc`), rendered via `text/template`. Output byte-identical to
  the prior string-based scaffolder; new `new_build_test.go` renders both templates
  and `go build`s a generated instance (demo back under the compiler). code-reviewer
  NITS addressed (slug dead-code removed; `path`-only TrimPrefix; quoted replace
  target; template allow-list).
- **ADR-010 Phase 4, Slice 2 — scaffolder/example/docs convergence** (2026-05-25,
  COMPLETE — pushed `c3f0cd2` framework + `ebee63d` scaffolder + `0b88cc9` docs +
  `09162c7` state). Module.Models→registry; builder OpenAPI mount; both scaffold
  templates → fluent+Module; website include-from-source (quickstart/project-structure
  import examples/mvc_api). Verified end-to-end (caught + fixed an RBAC act-verb bug
  by running the generated mvc server). →
  `docs/iterations/2026-05-25-adr010-phase4-slice2-convergence.md`. Carry-forward:
  internal-doc cmd/server drift sweep; articles-vs-notes demo naming (the scaffolder
  re-arch may settle this).

## Acceptance criteria (scaffolder re-arch)

- [x] Demo templates extracted to real `//go:embed`-ed files under `internal/cli/scaffold/templates/{_common,api,mvc}/`; `text/template` renderer.
- [x] `new.go` reduced to scaffolding logic only; 21 string constants deleted.
- [x] Generated output byte-identical to the prior scaffolder (verified by diff -r).
- [x] `new_build_test.go` renders BOTH templates and `go build`s a generated instance (demo under the compiler); `go build`/`vet`/`gofmt` clean.
- [x] code-reviewer NITS addressed; committed `625a85f` + this state commit; pushed.

## Status

- **COMPLETE.** No active iteration after this. Awaiting owner direction.

## CARRY-FORWARD FOLLOW-UP (surfaced this iteration — NOT done)

- **Internal-doc scaffold-layout drift (owner to schedule).** The scaffolder migration
  (sub-slice 3) made these INTERNAL docs stale — they still describe the old
  `cmd/server/main.go` + manual `app.New` scaffold layout: `docs/QUICKSTART.md`,
  `docs/reference/PROJECT_LAYOUT.md`, `docs/reference/DEVELOPER_MANUAL.md`,
  `docs/guides/DETAILED_TUTORIAL.md`, `docs/guides/DEPLOYMENT_GUIDE.md`,
  `docs/MODULARIZATION.md`, `README.md`. The PUBLIC website (quickstart/project-structure)
  is already fixed; these unpublished internal refs are a focused `doc-updater` sweep
  (cmd/server→root main.go, fluent surface) — out of this iteration's stated scope
  (scaffolder + website). (Compatibility reports under docs/reports/ are historical
  snapshots — leave them.)

## Status

### Done
- Design confirmed: `app.New` populates `a.DB`/`a.DBs` unconditionally (even under
  `WithoutDefaults()`), so `rt.DB()` resolves; `pkg/db` registers the sqlite driver
  (example drops its blank import); no `pkg/nucleus` test asserts Run ordering
  (safe to reverse Routes/OnStart).
- `pkg/nucleus/runtime.go` NEW — `Runtime` interface (`DB`/`AutoMigrate`/`Logger`) +
  unexported `runtime` struct bound to the module `DefaultDB` alias; nil-safe.
  `runtime_test.go` NEW (4 tests, green).
- `module.go` — `OnStart`/`OnShutdown` signature `*App`→`Runtime` (iface + `Module[C]` +
  wrappers). `nucleus.go` `Run()` — per-module Runtime + OnStart-before-Routes reorder;
  docstring updated.
- `examples/mvc_api` reworked to `rt.DB()` (examples-maintainer): dropped
  openSQLite/resolveDBURL/sanitizeURL/modernc-import/OnShutdown + the lazy controller;
  deleted `lifecycle_regression_test.go`. `go build`/`go test` green.
- Contract: additive rebaseline (4 lines: `type:Runtime` + 3 iface-methods); zero
  removals. Full `contracts/` suite green (freeze + firewall + sorted-unique).
- ADR-010 amended (Runtime definition + Gap-1/Gap-2 rationale + Phase 4 Slice log).
- architect-reviewer PASS (2 WARN/NITs); contract-guardian PASS. Fixed architect NIT:
  `AutoMigrate` godoc now notes it resolves alias from model metadata (not `rt.alias`).
- **End-to-end VERIFIED** (the process lesson): migrate → start (no panic) → full CRUD
  201/200/200/200/422/400/204/404/404; module logged the managed DB with no DSN leak;
  server stopped + scratch db removed.

- Post-review fix (code-reviewer + security-auditor both flagged): `Run` now
  registers each module's `OnShutdown` only AFTER its `OnStart` succeeds (was:
  all OnShutdowns up front) — no orphaned shutdown hooks on a mid-sequence
  OnStart failure. Added pointer-identity comment + a configured-named-alias
  test. Re-verified: build/vet/`-race`/examples/contracts all green.

### Done (this iteration)
- **Sub-slices 1+2 (framework enablement) — COMMITTED `c3f0cd2`.** `Module.Models`→registry
  auto-registration in Run; `nucleus.OpenAPISpec`/`App.OpenAPI`/`AppBuilder.WithOpenAPI`.
  Additive freeze rebaseline; ADR-010 Slice 2 amendment; CHANGELOG + API_CONTRACT_INVENTORY.
  Reviews: architect WARN (stable↔experimental openapi coupling) → resolved via ADR amendment;
  code NITs addressed; contract-guardian PASS.
- **Sub-slice 3 (scaffolder migration) — implemented + VERIFIED end-to-end, pending commit.**
  Owner decisions: keep repo/service layering (re-rooted on fluent Module); scoped RBAC.
  Both `api`+`mvc` templates → `nucleus.New()...Mount(Module).Start()`; `internal/articles`
  module wires repo→service via `rt.DB()`; controllers → `*nucleus.Context`; mvc `web` module
  (self-contained home template) + scoped `rbac_policy.csv`; dropped ensureSchema/blanket-RBAC/
  cmd/server. Delegated bulk edit to isolated worktree, then VERIFIED by generating BOTH
  templates + building against local pkg + RUNNING the mvc server. **Caught a runtime bug the
  implementer missed:** rbac_policy.csv used raw HTTP methods (GET/POST) but the authz
  middleware maps to CRUD verbs (read/create) → all routes 403. Fixed + locked with a test
  assertion. Re-run: GET/ /health GET+POST /api/articles /openapi.json 200/201; /admin 302;
  DELETE 403. Added `internal/cli/new_scaffold_test.go`. code-reviewer NITS (ParseFiles-CWD
  comment + api-WithoutDefaults test assertion applied).

- **Sub-slice 4 (include-from-source docs) — COMMITTED `0b88cc9`.** Wired `remark-code-import`
  (rootDir=repo root) into the Docusaurus docs preset; rewrote quickstart.md +
  project-structure.md to `file=`-import real code from `examples/mvc_api` and reflect the
  new scaffold layout. website-curator UPDATED; `npm run build` SUCCESS (verified by me too);
  drift guard clean; docs-content-verifier 0 violations. CI follow-up recorded (docs.yml
  triggers on website/** only → add examples/** trigger in the Slice 3 website-check.yml gate).

### In progress
- **CONVERGENCE ITERATION COMPLETE.** All 4 sub-slices committed (`c3f0cd2`, `ebee63d`,
  `0b88cc9`) + this state commit. Scaffold, example, and website docs now share the fluent
  idiom. Pending: push to origin/main. Carry-forward: internal-doc sweep (above).

### Blocked
- (none)

### Deferred follow-up (carry-forward)
- **(architect WARN, Gap-1) `Runtime.AutoMigrate` production guard.** Optional
  `slog.Warn` when `NUCLEUS_ENV=production` and a module calls `rt.AutoMigrate`.
  Low-cost, future slice.

## Most recent completed iteration

- **ADR-010 Phase 4, Slice "Gap 1"/"Gap 2" — `nucleus.Runtime` in module lifecycle**
  (2026-05-25, COMPLETE — committed `318e76c` + state `7630536`, pushed). `Runtime`
  handle (`DB`/`AutoMigrate`/`Logger`) replaces `*nucleus.App` in `OnStart`/`OnShutdown`;
  `Run` runs OnStart before Routes and registers OnShutdown only on OnStart success.
  `examples/mvc_api` reworked to `rt.DB()`. Full loop green; end-to-end verified.
  → `docs/iterations/2026-05-25-adr010-phase4-gap1-runtime.md`
- **ADR-010 Phase 4, Slice 1 — `examples/mvc_api` reference app** (2026-05-24,
  COMPLETE — committed `9e27243` + state close commit; verified end-to-end:
  server runs, full CRUD 200/201/200/200/200/400/422/204/404). First
  post-Phase-1 reference app on the fluent surface. Running it (not just unit
  tests) caught + fixed a startup panic (`Resource("")`-under-`Prefix`) and
  wrong doc commands (`cmd/nucleus`; migrate flag order); the framework gaps it
  surfaced (P1/P2/Gap-1/Module.Models) are recorded as follow-ups, no `pkg/`
  change. → `docs/iterations/2026-05-24-adr010-phase4-slice1-mvc-api.md`
- **Website audit + process hardening** (2026-05-24, COMPLETE — committed +
  pushed `a5ad7e6` + `76f1d4c`; done outside the layer-3 session). Added
  the `docs-content-verifier` subagent + CLAUDE.md §9/§10 anti-falsehood
  discipline (and wired it into doc-updater/website-curator/iterate/sync-docs),
  and fixed 3 P0 website body-content falsehoods (wrong Go version, non-existent
  `auth.VerifyPassword`, non-existent `storage.driver` key) + expanded the
  configuration/models-and-database/intro/principles pages →
  `docs/iterations/2026-05-24-website-audit-y-process-hardening.md`. NOTE: these
  two commits did not touch the live `.claude/state/*` files, which is why this
  handoff reconciled them in from `git log`.
- **ADR-010 §2 layer-3 field-semantic validation** (2026-05-24, COMPLETE —
  committed + pushed `ffeb609` + `9412807`) →
  `docs/iterations/2026-05-24-adr010-layer3-field-semantic-validation.md`
- **Oracle multi-block AutoMigrate execution** (2026-05-24, COMPLETE —
  committed + pushed `d46d29c` + `aad8bf8`) →
  `docs/iterations/2026-05-24-oracle-multiblock-automigrate.md`
- **`session_cookie_secure` secure-by-default** (2026-05-23, COMPLETE —
  committed + pushed `243ff1a` + `345cc0e`) →
  `docs/iterations/2026-05-23-session-cookie-secure-default.md`
- **Oracle model-scaffold identifier-casing → unquoted-uppercase + ADR-011**
  (2026-05-23, COMPLETE — committed + pushed `9a45373` + `df9e246`) →
  `docs/iterations/2026-05-23-oracle-identifier-casing-adr011.md`

## Candidate next steps (priority order, pending owner confirmation)

_Framework bugs surfaced by running examples/mvc_api end-to-end (2026-05-24):_

- **P1 — `WithoutDefaults()` does not suppress the admin bootstrap.**
  `pkg/app/app.go:~272` calls `admin.EnsureBootstrapAdminUser` UNCONDITIONALLY,
  before the `if !o.skipDefaults` guard (~line 477). So any service built with
  `.WithoutDefaults()` still creates the admin users table and logs/prints a
  generated admin password to stderr on first boot — a correctness + minor
  security-hygiene bug affecting every lightweight service. Fix: move the
  `EnsureBootstrapAdminUser` call inside the `!o.skipDefaults` branch.
  `pkg/app`-only change; no public-contract change. Worth a near-term fix
  (higher than the example follow-ups) — confirm with owner.
  **Scope clarified (verified on the live mvc_api server 2026-05-24):** the
  admin *panel* IS correctly gated by `skipDefaults` — `/admin/*` and the
  admin-gated `/_/config` return 404 under `WithoutDefaults()` (`core.Admin` is
  nil). So this is a *leaked-orphaned-user* bug (a `nucleus_admin_users` row +
  a password printed to stderr, with no portal mounted to use it), NOT an
  exposed-admin-portal bug.
- **P2 — `Router.Resource("")` under a module `Prefix` panics at startup.**
  `pkg/nucleus/router.go` `Resource("")` → `joinPath("")="" → mux.Get("")` →
  invalid `"GET "` pattern → `net/http.ServeMux` panic. A footgun for any module
  author who sets `Prefix` then calls `Resource("")` expecting the prefix to
  apply. Fix: `joinPath` should yield `/` (not `""`) when prefix+path are both
  empty, or `Resource` should reject/normalise an empty base. Fold into the
  Gap-1 (`nucleus.Runtime`) slice or fix standalone.

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

1. **ADR-010 Phase 4, Slice "Gap 1" — `nucleus.Runtime` into module lifecycle
   (next).** Surfaced by Slice 1 (`examples/mvc_api`) + flagged by
   architect-reviewer as a **BLOCKER for the website include-from-source slice**:
   `ModuleSpec.OnStart`/`OnShutdown` receive `*nucleus.App` (config), not the
   runtime `*app.App`, so modules can't reach the managed `*sql.DB`/`AutoMigrate`
   (mvc_api opens its own connection — teaches the wrong pattern). Add a
   `nucleus.Runtime` handle (thin wrapper over `*app.App`: `DB()`, `AutoMigrate`,
   …) as an arg to `OnStart`/`OnShutdown` (pre-`v1.0` `Module[C]`/`ModuleSpec`
   signature change; no external consumers ⇒ no deprecation; ADR-010 amendment +
   freeze rebaseline), then rework `examples/mvc_api` to `rt.DB()`. Also (Gap 2)
   document the Routes-before-OnStart ordering in `ModuleSpec` godoc (optionally
   reverse it). Gates Slice 2 (website include-from-source).

2. **ADR-010 Phase 4, Slice 2 — website include-from-source + quickstart/
   project-structure rewrite** (importing real code from `examples/mvc_api`).
   Gated on Gap 1. Then Slice 3 (more apps + `website-check.yml` CI gate).

3. **ADR-010 §2 layer 4 — referential validation** (module `Requires` →
   configured DB aliases; auth providers; observability exporters; the
   `smtp_port`>0-when-`mail_driver=smtp` cross-field check carried forward
   from layer-3). The penultimate validator layer (layer 5 = module-specific).

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
