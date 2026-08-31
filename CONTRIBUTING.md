# Contributing to Nucleus

Thanks for your interest in improving Nucleus.

This document describes the preferred workflow for contributing code, docs, and tests.

## Development Setup

1. Fork and clone the repository.
2. Install Go `1.26+` — the exact minimum is the `go` directive in
   `go.mod`, which is the only place that number lives (prose copies of it
   have drifted three separate times; we stopped writing them).
3. Run tests:

```bash
go test ./...
```

4. Optional full release rehearsal:

```bash
bash scripts/release/rehearse_rc.sh
```

## Before opening a PR: `make check`

`make check` runs everything CI's cheap lanes will run: `go vet`, every
local guard, and the test suite. Run it before pushing — the guards exist
precisely to catch the classes of drift a PR reviewer will not spot:

| Guard | Fails when |
|---|---|
| `check_version_claims.sh` | a version claim in README/SPEC/SECURITY/CLAUDE/site drifts from the release manifest, or the scaffold's Go directives drift from `go.mod` |
| `check_docs_product_voice.sh` | docs pick up marketing hype |
| `check_adr_index.sh` | an ADR is missing from the index |
| `check_versioned_docs_markers.sh` | a versioned docs snapshot carries a live version marker |
| `check_internal_docs_drift.sh` | living internal docs (docs/ **and the root .md files**) cite a file that does not exist |
| `check_docs_archive_freshness.sh` | the newest docs snapshot falls behind the published minor |
| `check_example_pins.sh` | `examples/showcase_demo` pins drift |
| `check_contract_freeze.sh` | a frozen surface changed without its baseline |
| `gen-config-reference` | `website/docs/reference/configuration.md` is stale against the config registry |
| `check-coverage.sh --strict` | the public site references removed APIs, or a fenced example contradicts the freeze baseline / config registry |

If you **intentionally** changed a frozen surface (new exported symbol,
CLI command, config key, extension-facing field, security default), run
`make regen-baselines` and commit the regenerated files in the same
change; `config_key_patterns.txt` and `cli_primary_commands.txt` are
maintained by hand.

The heavier lanes — the database matrix (Postgres/MySQL/MariaDB in
Docker), `jobs-redis`, `storage-minio`, and the showcase smoke — run only
in CI; `make check` does not need any external service. To reproduce them
locally, `docker-compose.test.yml` brings up the same services CI uses.

## Branch and Commit Workflow

1. Create a branch from `main`.
2. Keep commits focused and atomic.
3. Use clear commit messages (for example: `feat(cli): add xyz command`).
4. Open a Pull Request against `main`.

## Pull Request Expectations

A PR should include:

- clear summary of what changed and why
- tests for behavior changes (or rationale if not applicable)
- docs updates when command/API behavior changes
- changelog entry when user-facing functionality is added/changed

If your change adds an exported symbol, a CLI command, a config key, a
framework service an extension can read, or moves a security default, a
baseline under `contracts/baseline/` has to be regenerated **in the same
change**. The tests tell you which one and how. Two of those baselines
block change in *both* directions on purpose: a new extension-facing field
is a promise to plugin authors, and a security default that gets stricter
still changes the behaviour of deployments that relied on the old one.

When a change spans several PRs, land the **code before the prose**.
`scripts/ci/check_internal_docs_drift.sh` fails on internal documentation
that cites a file which is not in the tree, and a file that only exists on
an unmerged branch is, from the guard's point of view, a file that does not
exist. Splitting a feature into "implementation" and "documentation" PRs is
fine — merging them in that order is what keeps the guard honest instead of
noisy.

Before opening a PR, run:

```bash
make check
```

If your changes affect release packaging or docs integrity, also run:

```bash
bash scripts/release/rehearse_rc.sh
```

## Areas to Prioritize

- CLI ergonomics and parity improvements
- reliability, observability, and security hardening
- documentation quality and onboarding DX
- test coverage for regression-prone paths

## Reporting Bugs

When opening an issue, include:

- Go version and OS
- command(s) executed
- config snippet (`nucleus.yml`) if relevant
- expected behavior vs actual behavior
- reproducible steps and logs/error output

## Code of Conduct

By participating, you agree to follow the project Code of Conduct:

- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
