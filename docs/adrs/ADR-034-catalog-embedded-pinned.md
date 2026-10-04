# ADR-034: The `nucleus add` catalog is one table, embedded and pinned to the release

- Status: Accepted
- Date: 2026-10-04
- Deciders: jcsvwinston
- Amended by: [ADR-035](ADR-035-catalog-entries-carry-their-wiring.md) — an
  entry may carry its wiring (a chain call, a configuration block), which is
  how `apikeys` and `sql-queue` joined the table and `oidc` got its routes
- Related: [ADR-024](ADR-024-ldap-provider-module.md),
  [ADR-030](ADR-030-cloud-backends-as-modules.md) and
  [ADR-031](ADR-031-drivers-and-exporters-as-modules.md) (the modules the
  catalog installs), [ADR-028](ADR-028-federated-authentication-seam.md)
  (the federated registry `oidc` joins), the A11 bench
  (`docs/catalog-bench.md`)

## Context

`nucleus add <name>` resolved a name through `internal/knownproviders`, ran
`go get <module>` with no version — so it installed whatever the module proxy
called latest that day, not the version released with the CLI the person
runs — and wrote a blank import. The same package fed the "you have not
imported it" refusals of five subsystems, but as four maps the help read and
a fifth it did not: `nucleus add aws-sm` worked and `nucleus add --help`
never mentioned it. `nucleus new --with` read a sixth list, the suite
products, so `nucleus add quark` and `nucleus new --with s3` were both
refused. After an install the command said nothing about the configuration
that selects what it installed, so `nucleus add s3` followed by nothing did
nothing; a mistyped name got the whole table back.

The A11 bench measured it on 2026-10-04 (`CAT-02`, `CAT-03`, `CAT-07`,
`CAT-08`, `CAT-10`). The arc's plan asked for a remote, signed index. The
owner decided against it the same day: the CLI lives in the root module
(NU-8), so every dependency a fetcher or a signature verifier brings is
inherited by every application, and a remote index is a second source of
truth for names the runtime refusals already carry.

## Decision

1. **One table.** `internal/knownproviders/catalog.go` is the catalog. Each
   row has a name and its aliases, how it ships (below), the group it is
   listed under, the module, the package its blank import names, the key the
   runtime registry knows it by (`pgx`, `aws-sm:`), the configuration that
   selects it, and what wires it. `nucleus add`, `nucleus add --help`,
   `nucleus new --with`, `nucleus new --help` and the refusals of `pkg/db`,
   `pkg/observe`, `pkg/storage`, `pkg/auth`, `pkg/auth/secrets` and the
   federated registry read it; the per-subsystem lookups the refusals used
   (`DBDriver`, `StorageProvider`, …) stay, as views keyed the way each
   registry is keyed. A test in `internal/cli` reads what each surface
   prints or accepts — not the table — and fails when one names something
   the others do not; another compares the table with the `nucleus add` row
   of the site's CLI reference, which this repository does not generate.

2. **Three ways of shipping.**
   - A **module** (`drivers/*`, `exporters/*`, `providers/*`) is fetched at
     the version released with the CLI and registers through its blank
     import.
   - A **core** entry is a package of the framework module: nothing to
     fetch, the blank import is the wiring. Today that is `oidc`, which
     registers with the federated registry when imported. The other core
     capabilities the arc names — accounts, apikeys, sql-queue, websockets —
     register nothing by import; their wiring is code (a store, a `Mount`, a
     middleware), so they stay out of the table until the session that gives
     an entry a recipe (A11 N4) can write it. A name the table carries is a
     name `nucleus add` can honour.
   - A **suite product** (`orbit`, `quark`, `quarkbridge`,
     `quarkdatasource`) is fetched and not imported: the templates wire it
     (`nucleus new --with orbit` writes the `Mount`), and `nucleus generate
     module <name> --data quark` imports the ORM. `nucleus add` fetches it
     and says what wires it.

   `nucleus add` and `nucleus new --with` take the same names. On `new`, a
   module or core entry gets its blank import written into the rendered
   `main.go` before the network step, so the tidy keeps it.

3. **Pinned to the release.** `go get` names a version: the one released
   with the CLI. `internal/knownproviders/modules.json`, embedded with
   `go:embed`, carries the version of every package release-please versions
   (the framework as `"."` included). release-please keeps it in step: every
   package lists it as an extra file of type `json` with the jsonpath of its
   own key (`$['drivers/postgres']`), so the release PR that tags a module
   rewrites its line in the same commit. Two tests guard it: one compares
   the file with `.release-please-manifest.json` and fails on any
   difference, one fails when a package has no such extra file or has it
   with the wrong path or jsonpath.

   **How release-please resolves the path.** An extra file is relative to
   its package unless it starts with `/`, which makes it relative to the
   repository root: `BaseStrategy.addPath` in release-please 17.6.0 — the
   version `release-please-action` v5.0.0 (the pin in
   `.github/workflows/release-please.yml`) bundles, per its lockfile —
   strips the leading slashes and does not prefix the package path. So the
   root package lists `internal/knownproviders/modules.json` and every other
   package `/internal/knownproviders/modules.json`; without the slash,
   `drivers/mssql` would target `drivers/mssql/internal/knownproviders/modules.json`,
   a file that does not exist, and its version would silently stay behind.
   With `separate-pull-requests: false` the manifest merges every package's
   updates for one path into one `CompositeUpdater`, so a release of several
   modules rewrites several keys of the same file. Both were confirmed by
   running release-please 17.6.0's own Go strategy and merge code over this
   repository's configuration: all thirteen keys moved to the bumped
   versions, the result equal to the bumped manifest.

4. **The suite products are not pinned, and that is recorded, not hidden.**
   Their versions belong to the umbrella's certified set (`versions.yaml` in
   the Quantum repository), which is written after this CLI is tagged:
   Nucleus tags first in every release train, and Orbit requires the Nucleus
   it is cut against. No release of this repository can carry the orbit and
   quark versions certified with it. Pinning them needs the set to travel
   with the CLI from the umbrella; until then they are fetched at the tag the
   module proxy calls latest, the CLI says so on every fetch, and the bench
   keeps `CAT-04` partial for exactly that.

5. **No network, no keys, no new dependency.** The catalog a person sees is
   the one their CLI was released with. A newer catalog comes with a newer
   CLI, which is how the module versions it pins come too.

6. **The command finishes the job it starts.** After an install it prints
   the configuration that selects the entry (`storage.provider: s3`,
   `auth_backends: [ldap]`, `otlp_endpoint`), or what wires it when no key
   does. A name that is not in the catalog gets the nearest one when a typo
   explains it (`prometeus` → `prometheus`), and the list of names when not.
   The runtime's refusals print `nucleus add <name>` first and the two steps
   it stands for after.

## Consequences

- `nucleus add` stops installing versions nobody released together with the
  CLI that installs them. A dev build pins the versions of the last release,
  which exist on the proxy.
- After a rebase onto a release, `modules.json` disagrees with the manifest
  until it is copied over; the comparison test says so in those words.
- A module added later in A11 (saml, redis-cache, stripe, sentry) joins the
  catalog with a row, a release-please package and its extra file;
  forgetting the extra file fails the package test, forgetting the row fails
  the test that every package has an entry.
- Additive only (QADR-0010): no name `nucleus add` or `nucleus new --with`
  accepted before is refused now, and the runtime lookups keep their
  signatures.
- A third-party catalog is out of scope; a third-party module is still
  `go get` plus an import, as before.
