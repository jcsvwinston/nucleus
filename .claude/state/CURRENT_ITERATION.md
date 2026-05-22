# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

No active iteration. Last completed: **`pkg/observability` (+`/hooks`)
inventory entry + lifecycle** (commit `9227e7d`, 2026-05-22), archived at
`docs/iterations/2026-05-22-observability-lifecycle-experimental.md`.

## Scope

- (no active iteration)

## Acceptance criteria

- (no active iteration)

## Status

### Done (earlier — see prior archives)

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

1. **Add `covers:`/`config_keys:` frontmatter manifests to the 14
   `website/docs/` pages.** NEW (surfaced by website-curator 2026-05-21).
   None exist yet; the drift guard's dangling-ref check has no signal until
   manifests are present. A `website-curator` task. Medium effort; enables
   the guard's most useful check. (The `pkg/observability`(+`/hooks`)
   inventory + lifecycle item — former candidate #1 — landed 2026-05-22.)

   Carry-forward notes from recent iterations (low priority, not blocking):
   - **Relocate `pkg/observability` to `internal/` post-v1.0** rather than
     ever promoting it to `stable` (architect-reviewer 2026-05-22). It is
     internal-facing plumbing; `experimental` buys time, but the eventual
     right move is relocation, tracked for the Phase 4 modularization pass.
   - `discoverPublicPackages` double-reads each dir (WalkDir +
     `hasGoSource`'s `os.ReadDir`); could accumulate from the walk callback's
     `DirEntry`.
   - The `*ast.InterfaceType` unexported-skip branch in
     `checkTypeSpecForLeaks` is effectively a no-op (cross-package interfaces
     can't carry unexported methods) — kept for symmetry.

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
