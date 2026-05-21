# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

No active iteration. Last completed: **Website refresh + website-curator
subagent** (commits `3ca91ce`, `5a79095`, 2026-05-21), archived at
`docs/iterations/2026-05-21-website-refresh-and-curator.md`.

## Scope

- (no active iteration)

## Acceptance criteria

- (no active iteration)

## Status

### Done (earlier — see prior archives)

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

1. **Shared package-enumeration helper for contract scanners.** NEW
   (surfaced by architect-reviewer 2026-05-21). `contracts/freeze_test.go`
   and `contracts/firewall_test.go` hand-maintain near-identical `packages`
   slices; drift risk is real (the `pkg/observability` omission is
   evidence). Extract a single `allPublicPackages()` source-of-truth so
   each scanner applies a filter predicate and omissions become
   machine-visible. Medium effort.

2. **`pkg/observability` inventory entry + firewall scan.** NEW (surfaced
   by architect-reviewer 2026-05-21). It is a real public `pkg/*` package
   with no `API_CONTRACT_INVENTORY.md` row and no scanner coverage
   (currently leak-free, imports nothing forbidden). Needs a lifecycle-tag
   decision (e.g. `experimental` or a new `internal-facing` annotation) —
   an owner call.

3. **Add `covers:`/`config_keys:` frontmatter manifests to the 14
   `website/docs/` pages.** NEW (surfaced by website-curator 2026-05-21).
   None exist yet; the drift guard's dangling-ref check has no signal until
   manifests are present. A `website-curator` task. Medium effort; enables
   the guard's most useful check.

4. **Oracle model-scaffold identifier-casing (opened by PR #78).**
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
- `contracts/freeze_test.go` — pkg/circuit + pkg/health now frozen;
  inclusion-rule + deliberate-omissions comment.
- `contracts/firewall_test.go` — pkg/admin, pkg/health, pkg/nucleus
  now scanned; firewall-vs-freeze divergence comment.
- `contracts/baseline/api_exported_symbols.txt` — regenerated baseline
  (+28 circuit + health symbols).
- `docs/reference/API_CONTRACT_INVENTORY.md` — Freeze Enforcement coupled-
  change note.
- `pkg/model/migration_scaffold_oracle.go` — candidate #4 target
  (identifier quoting).
- `pkg/nucleus/config.go`, `pkg/nucleus/nucleus.go` — Phase 2 loader
  (candidate #6 starting point).

## Notes / decisions log

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
