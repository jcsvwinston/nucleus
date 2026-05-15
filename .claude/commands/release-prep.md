---
description: Heavy-weight pre-release validation — full governance check, contract freeze, compatibility harness, release checklist sign-off.
argument-hint: optional target version (e.g. v0.7.0)
---

Run the **pre-release** validation pass for the version in `$ARGUMENTS` (or the next implied version inferred from `CHANGELOG.md` and `git tag` if omitted).

This is the strictest gate the project supports. Stop on any blocker and surface it to the user — do not proceed silently past a failure.

## Steps

1. **State reconciliation.** Read `.claude/state/CURRENT_ITERATION.md` and verify the active iteration is empty or archived. If an iteration is still in progress, abort with:
   > "iteration `<title>` is still active — close it via `/handoff` and archive it under `docs/iterations/` before running `/release-prep`."

2. **Governance cross-check.** Delegate to `governance-checker` for the **full-strength** run (not the light-touch variant used in `/iterate`). It verifies:
   - `docs/governance/COMPATIBILITY_SLO.md` deprecation timeline against actual entries in `docs/deprecations/`.
   - `docs/governance/CI_MATRIX.md` lanes against `.github/workflows/`.
   - `docs/governance/RELEASE_CHECKLIST.md` items, one by one.

3. **Contract freeze.** Run:
   ```bash
   bash scripts/ci/check_contract_freeze.sh
   ```
   Report any drift in `pkg/*` exported symbols, CLI surface, or `nucleus.yml` schema.

4. **Compatibility harness.** Run:
   ```bash
   bash scripts/ci/run_compatibility_harness.sh --enforce-threshold
   ```

5. **Compatibility report.** Generate:
   ```bash
   bash scripts/release/generate_compatibility_report.sh \
     --output dist/reports/compatibility_report.md \
     --enforce-threshold
   ```

6. **Dependency-impact report.** Generate:
   ```bash
   bash scripts/release/generate_dependency_impact_report.sh \
     --output dist/reports/dependency_impact_report.md
   ```

7. **Docs coverage (website).** Verify every stable surface symbol has at least one `website/docs/*` page declaring it in `covers:`. Run `scripts/website/check-coverage.sh` when it exists; otherwise delegate to `doc-updater` for a manual coverage report.

8. **`CHANGELOG.md`** — verify the `Unreleased` block is ready for renaming to the target version, with entries grouped by `### Added / ### Changed / ### Fixed / ### Deprecated / ### Removed`. Propose the rename diff but do not apply it yet.

9. **Migrations.** Verify each new entry under `docs/deprecations/` for this release has a matching migration assistant under `docs/migration_assistants/`. Delegate to `migration-assistant` if any are missing.

10. **Final report.** Produce a go / no-go report:
    - **GO** → list the next manual steps: tag, push, GitHub release notes, npm/PyPI mirrors if applicable.
    - **NO-GO** → list every blocker with its source step and severity. Do not propose a partial release.

## Optional rehearsal

The full rehearsal path (everything above plus a dry-run of the release artefacts) is:
```bash
bash scripts/release/rehearse_rc.sh
```

Suggest this to the user when they want a more thorough simulation than the validation above provides.
