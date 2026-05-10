---
description: Run the heavy-weight pre-release validation per docs/governance/RELEASE_CHECKLIST.md. Aborts on any open WARN or FAIL.
argument-hint: [target version, e.g. v0.6.1 or v0.7.0-rc1]
---

Execute the **Release Preparation** flow. The target version is in
$ARGUMENTS (default: ask the user).

Steps:

1. Confirm target version with the user. Validate format (`vX.Y.Z` or
   `vX.Y.Z-rcN`). Confirm we are on `main` (or the agreed release
   branch) and the working tree is clean (`git status` empty).

2. Delegate to `contract-guardian` to verify nothing on a stable
   surface was removed/renamed without a deprecation entry.

3. Run the contract freeze tests:
   `bash scripts/ci/check_contract_freeze.sh`.

4. Run the compatibility harness:
   `bash scripts/ci/run_compatibility_harness.sh --min-pass-rate 100
   --enforce-threshold`.

5. Generate the dependency impact report:
   `bash scripts/release/generate_dependency_impact_report.sh
   --enforce-critical-review`.

6. Generate the full compatibility report:
   `bash scripts/release/generate_compatibility_report.sh
   --enforce-threshold`. The report must say `READY`.

7. Delegate to `governance-checker` in **release-prep** mode. Any WARN
   becomes FAIL.

8. Delegate to `doc-updater` to ensure `README.md`, `CHANGELOG.md`, and
   relevant `docs/*` reflect the about-to-ship behaviour. Promote
   `## [Unreleased]` to `## [<target version>] - YYYY-MM-DD` (use
   absolute date) and add a fresh empty `## [Unreleased]`.

9. Delegate to `test-runner` for `go test ./...` final sweep.

10. Print a release-readiness verdict:

    ```
    ## Release Readiness — <target version>

    Verdict: READY | NOT_READY

    Gates:
    - contract freeze         : PASS|FAIL
    - compatibility harness   : PASS|FAIL
    - dependency impact       : PASS|FAIL
    - compatibility report    : READY|NOT_READY
    - governance (release)    : PASS|FAIL
    - docs & changelog        : PASS|FAIL
    - go test ./...           : PASS|FAIL

    Next manual step:
    - Confirm version in goreleaser config.
    - Tag and push: `git tag <version> && git push origin <version>`.
    ```

Do **not** create the git tag or push. The user always performs the
final tag/push step.
