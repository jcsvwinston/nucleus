# Contributing to Nucleus

Thanks for your interest in improving Nucleus.

This document describes the preferred workflow for contributing code, docs, and tests.

## Development Setup

1. Fork and clone the repository.
2. Install Go `1.26+` (current minimum supported version; matches the `go 1.26.3` directive in `go.mod`).
3. Run tests:

```bash
go test ./...
```

4. Optional full release rehearsal:

```bash
bash scripts/release/rehearse_rc.sh
```

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
go test ./...
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
