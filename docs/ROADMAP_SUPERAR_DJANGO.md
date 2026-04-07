# Roadmap To Surpass Django

Reference date: 2026-04-07.
Status: Active execution roadmap (6 weeks).

This roadmap is focused on surpassing Django in practical enterprise value without losing GoFrame identity.

## Objective

By 2026-05-19, GoFrame should provide:

- stronger operational and compatibility guarantees than Django (already a strength),
- comparable or better developer ergonomics in daily workflows,
- a clearer and more powerful data-model contract (PK, FK, indexes),
- stable multi-database and multitenant behavior across CLI and runtime.

## Baseline (as of 2026-04-07)

- Strong:
  - contract governance (`API`/`CLI`/config lifecycle docs),
  - SQL-first operations CLI,
  - multisite/multitenant isolation guardrails by default,
  - compatibility harness and release gate scripts.
- Main gaps to close:
  - scaffold consistency (`new`/`startapp`/`generate`),
  - CLI-wide DB alias selection (`--database <alias>`),
  - model contract depth (custom PK, FK declaration, simple/composite indexes),
  - admin maturity and output consistency across commands,
  - stale spec alignment with current implementation.

## Execution Snapshot (as of 2026-04-07)

- Week 1 completed: canonical scaffold contract aligned across `new`/`startapp`/`generate`.
- Week 2 completed: CLI multi-DB alias parity shipped in critical DB-dependent commands.
- Week 3 completed: model metadata contract includes PK/FK/index declarations with validation.
- Week 4 completed: migration scaffolding and `inspectdb` enriched from metadata contract.
- Week 5 completed: admin action-level authorization and bulk/error behavior hardening; CLI output homogeneity advanced.
- Week 6 in progress:
  - compatibility report and harness artifacts generated,
  - stable contract freeze gate active in CI,
  - `SPEC.md` synchronized to current architecture/dependency reality,
  - RC rehearsal executed successfully and critical dependency review documented.

## Success Metrics

1. DX consistency:
- `new`, `startapp`, and `generate` produce one canonical layout contract.
- No contradictory paths (`internal/...` vs `handlers/...`) in generated code.

2. Multi-DB operability:
- Core data commands support `--database <alias>` and include active alias in output.
- No regressions in existing default-alias flows.

3. Data-model power:
- Model tags support explicit custom PK, FK metadata, and indexes (simple/composite).
- Migration scaffolding reflects these declarations.

4. Admin and CLI quality:
- Admin lifecycle promoted from `transitional` to `stable` only if acceptance criteria pass.
- Pretty/plain/json behavior is homogeneous in critical commands.

5. Reliability:
- `go test ./...` green.
- `scripts/ci/run_compatibility_harness.sh --enforce-threshold` green.
- Exploratory DB stability report remains above policy thresholds.

## 6-Week Execution Plan

## Week 1 (2026-04-08 to 2026-04-14) - DX Contract Unification

Deliverables:

- Define canonical scaffold contract:
  - controllers/models/tasks/templates under `internal/...`,
  - no parallel `handlers/` root outside chosen contract.
- Refactor `generate` to align with `new` and `startapp`.
- Fix generated `go.mod` Go version drift in `new` scaffold.
- Add scaffold contract tests.

Acceptance criteria:

- `goframe new`, `goframe startapp`, and `goframe generate` output same structure philosophy.
- New tests validate generated paths and compile baseline stubs.

## Week 2 (2026-04-15 to 2026-04-21) - Multi-DB CLI Parity

Deliverables:

- Add `--database <alias>` to CLI DB-dependent commands:
  - `migrate`, `sqlmigrate`, `sqlflush`, `sqlsequencereset`, `flush`,
  - `seed`, `dumpdata`, `loaddata`, `inspectdb`, `ogrinspect`,
  - `shell`, `createcachetable`, `clearsessions`, `remove_stale_contenttypes`,
  - `createuser`, `changepassword`, `health`, `routes` (where relevant).
- Keep default behavior unchanged when flag is omitted.
- Include resolved alias in human-readable outputs.

Acceptance criteria:

- Alias selection is deterministic and test-backed.
- Existing CLI tests remain green; add alias-specific coverage.

## Week 3 (2026-04-22 to 2026-04-28) - Data Model v1 (PK/FK/Indexes)

Deliverables:

- Extend model tag contract to support:
  - custom PK field naming/column mapping,
  - explicit FK declaration (target model/table/column when needed),
  - index declarations (`index`, `unique`, composite group naming).
- Update metadata extraction in `pkg/model`.
- Add validation and error messages for invalid tag combinations.

Acceptance criteria:

- Metadata API exposes PK/FK/index info consistently.
- Unit tests cover valid/invalid declarations and backward compatibility.

## Week 4 (2026-04-29 to 2026-05-05) - Migration and Inspect Integration

Deliverables:

- Make migration scaffolding aware of model metadata additions.
- Define deterministic naming conventions for indexes and FK constraints.
- Improve `inspectdb` mapping so generated models can round-trip better.
- Add import bootstrap guidance for SQL-first onboarding.

Acceptance criteria:

- Generated migration SQL includes declared PK/FK/indexes for supported engines.
- Inspect/generated models require minimal manual edits for common schemas.

## Week 5 (2026-05-06 to 2026-05-12) - Admin v1 Hardening + Output Homogeneity

Deliverables:

- Strengthen admin behavior:
  - explicit action-level permission checks,
  - better bulk-action error reporting,
  - pagination/filter/search behavior hardening.
- Normalize CLI output modes in critical commands:
  - consistent plain/pretty/json semantics,
  - consistent status tagging and error shape.

Acceptance criteria:

- Admin critical API tests pass with permission and validation scenarios.
- CLI contract tests verify stable parseable output for automation paths.

## Week 6 (2026-05-13 to 2026-05-19) - Release Readiness and Contract Freeze Prep

Deliverables:

- Sync `SPEC.md` with actual architecture and dependency reality.
- Refresh docs and changelog for all user-visible changes.
- Run and archive:
  - full test suite,
  - compatibility harness,
  - exploratory stability drill reports.
- Prepare `v0.x` release candidate package with explicit compatibility statement.

Acceptance criteria:

- Mandatory release checklist artifacts are present and reviewed.
- No unresolved high-severity regressions in stable surfaces.

## Risks and Mitigations

1. Scope risk (too much in 6 weeks):
- Mitigation: lock Week 1-2 as mandatory foundation, stage Week 3-5 by severity.

2. Cross-engine SQL complexity:
- Mitigation: ship stable guarantees first for required engines, keep exploratory deltas explicit.

3. Backward compatibility regressions:
- Mitigation: add contract tests before refactors and enforce changelog migration notes.

## Execution Discipline

- Every merged slice must include:
  - tests,
  - docs update,
  - contract-matrix update when surface changes.
- No undocumented behavior changes in stable commands/config keys.

## Exit Decision (2026-05-19)

Proceed with “GoFrame surpasses Django in enterprise platform value” narrative only if:

1. Week 1-2 deliverables are complete and stable.
2. Week 3 model contract is implemented and test-backed.
3. Week 5 output/admin consistency meets automation and UX thresholds.
4. Week 6 release artifacts pass without unresolved blockers.
