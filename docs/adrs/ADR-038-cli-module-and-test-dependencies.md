# ADR-038: The CLI is a module of its own, released with the framework; the tests' foreign dependencies leave the framework's go.mod

- Status: Accepted
- Date: 2026-10-06
- Deciders: jcsvwinston
- Related: [ADR-031](ADR-031-drivers-and-exporters-as-modules.md) (the
  driver modules and the hello-world it measured),
  [ADR-032](ADR-032-packaging-moves-and-the-major-line.md) (what a packaging
  move may change), NU-106 and NU-8 in the 2026-09-03 audit, A12 `S0` and
  `N1` in the umbrella's plan, quark's ADR-0024 (the same move for quark's
  CLI)

## Context

A module's `go.mod` lists every module its own packages **and its own
tests** import, and an application that requires the framework inherits that
list into its build graph. In 1.31.0 the framework's `go.mod` carried, besides
what applications run:

- the five database engines, because the `nucleus` CLI links every engine and
  lived in the framework's module, and because the test binaries linked every
  engine through `internal/alldrivers` so the DB matrix lanes could reach
  PostgreSQL, MySQL, SQL Server and Oracle;
- miniredis (and the Lua interpreter it embeds), which only tests used;
- goleak, which only two `TestMain` functions used.

Measured on 2026-10-06 (A12 `N1`): `pkg/app` alone — the hello-world ADR-031
measured at 87 modules — had a build list of 92; with `drivers/sqlite` it was
105, and the `nucleus new` api starter 108. None of those programs links an
engine it did not import (NU-8 closed that); the extra modules are in the
build list `go mod download` fetches and a vulnerability scan walks. Nothing
watched the number, so it grew.

## Decision

1. **The CLI is a module of its own**, `github.com/jcsvwinston/nucleus/cmd/nucleus`.
   It links the five engines the way an application does, by importing the
   five driver modules, which register their drivers and classifiers.
   `internal/cli` stays in the framework's module: the CLI module may import
   it because its path sits under the framework's, and nothing it needs is
   missing from the framework's `go.mod`. `internal/alldrivers` is retired.

2. **The CLI is released from the framework's commit, with the framework's
   number.** For every `vX.Y.Z` there is a `cmd/nucleus/vX.Y.Z` on the same
   commit, so `go install github.com/jcsvwinston/nucleus/cmd/nucleus@vX.Y.Z`
   — the line the suite's install page and its integration workflow use —
   and `@latest` mean what they meant, and `nucleus version` still prints the
   framework's version. The CLI requires the framework at the version that
   commit cuts: the requirement carries the `x-release-please-version` marker,
   release-please rewrites it in the release PR (an extra file of the root
   package), `check_version_claims.sh` fails if it drifts on `main`, and the
   release workflow checks it before it creates the tag. The CLI is not a
   release-please package: one with a version series of its own would stop
   `@vX.Y.Z` from resolving.

3. **The framework's tests link SQLite and nothing else foreign.**
   - `internal/testsqlite` links SQLite and registers its classifier for the
     test binaries; the DB matrix lanes link the other four engines on top
     with `scripts/ci/test_with_engines.sh`, which builds the tests in a
     throwaway workspace with an overlay that imports the four driver
     modules. The matrix tests stay where they are, next to the unexported
     code they exercise; a lane that forgets the script fails with the
     framework's "import _ .../drivers/postgres" error.
   - `internal/testdeps`, a module that is never published, holds what needs
     the foreign code itself: the tests that hold `internal/dbclassify` to
     each engine's real error type, the driver-graph test, and a miniredis
     server. `internal/testredis` builds that server and starts one process
     per server a test asks for, answering the inspection the tests used
     (`Get`, `TTL`, `Keys`, `FastForward`) over the wire.
   - `internal/leakcheck` replaces goleak's `VerifyTestMain` with the same
     default filters and retry policy.
   - pgx's real error type is classified in `drivers/postgres`'s tests; the
     framework's tests use a stand-in of the same shape.

4. **A lane holds the numbers.** `hello-world-size` builds `pkg/app` alone
   and with `drivers/sqlite` against the tree and fails when the build list,
   the linked modules, the packages or the stripped binary pass the ceilings
   in `scripts/ci/hello-size/ceilings.tsv`. The first three are exact: one
   more is a decision for the pull request that brings it. The binary carries
   2% of headroom, because it grows with every line of code in a linked
   package.

## Consequences

| Program (darwin/arm64) | Build list | Linked modules | Packages | Binary / `-s -w` |
|---|---|---|---|---|
| hello (`pkg/app`) | 92 → **79** | 39 | 372 | 31.0 / 21.3 MB |
| hello + `drivers/sqlite` | 105 → **92** | 48 | 408 | 36.8 / 25.2 MB |
| `nucleus new --template api` | 108 → **95** | 57 | 465 | 42.8 / 29.3 MB |

The binaries do not change: the modules that left were never linked. What
changes is what an application downloads, scans and is told it depends on.

- **Nothing changes for an application or for `go install`.** The package
  path is the same; the version numbers are the same.
- **Building the CLI from a clone needs a workspace.** Its `go.mod` names a
  framework release, never a `replace` (`go install` refuses a module that
  carries one). `make workspace` writes a `go.work`; CI, the release job and
  the Dockerfile write their own with `scripts/ci/cli_workspace.sh`, whose
  versioned replace also covers a release branch, where the requirement names
  a version that does not exist yet.
- **Until the first release cut after this change, the CLI module cannot be
  resolved from the proxy.** Every release up to v1.31.0 ships `cmd/nucleus`
  inside the framework's module, so a CLI module requiring one of them sees
  its own package twice and Go refuses with `ambiguous import`. The release PR
  rewrites the requirement to the version it cuts, which no longer ships it.
  Between the merge of this change and that release, `go install …@latest`
  can resolve the CLI module's pseudo-version and fail the same way, so the
  change is merged right before a release. The standalone lane says which of
  the two cases holds instead of checking the CLI module while either does.
- **`nucleustest` still links SQLite.** Its `TempSQLite` and `CheckModule`
  open a SQLite database for callers that link no driver, so removing the
  import would break them; that move belongs to the major (QADR-0010), with
  a notice first.
- **What the graph still carries** is the framework itself: asynq and what it
  requires reach the build list through the core, and leave in the major
  (QADR-0010); the hello-world with `drivers/sqlite` is 0.2 MB over the 25 MB
  the A12 plan set for its stripped binary, all of it linked code this
  change does not touch.
