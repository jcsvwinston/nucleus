# Catalog bench — what extending an application can do today

This is the numerator of the A11 gate ("extensibility and catalog"). It exists
because that gate needs a number, and a number needs something that produces
it.

**Measured on 2026-10-04 against v1.30.1.** The session that opened the arc
(`S0`) measured 6 of 38; `N1` — one catalog, pinned to the release — moved
`CAT-02`, `CAT-03`, `CAT-07`, `CAT-08`, `CAT-09` and `CAT-10` to present; `N2`
gave the api starter the storage the catalog installs (`EN-10`…`EN-12`) and
the refusals of the published backends the command that installs them; and
`N3` gave each driver module its own classifier, so an application links only
the engine it added (`CAT-11`) and a postgres URL in a SQLite application is
refused naming `nucleus add postgres` (`CAT-01`): 17 of 38. Run it with:

```bash
go test ./internal/catalogbench/ -run 'TestCatalogBench$' -v
go test ./internal/catalogbench/ -run TestCatalogBenchSummary -v                     # per-family counts
NUCLEUS_CATALOG_BENCH_TABLE=1 go test ./internal/catalogbench/ -run TestCatalogBenchTable   # writes bench-table.md next to the cases
```

The last command writes a generated `bench-table.md` next to the cases, a
file that is not committed; the tables under "The result" — the per-family
summary and the catalogue — are pasted from it when a verdict moves, so the
page and the catalogue say the same thing.

The bench is not prose. Every control is a Go probe in `internal/catalogbench/`
that runs the real CLI in-process (`cli.Run`, exactly `nucleus <args>`),
scaffolds the api starter with `nucleus new --template api --offline`, runs
`nucleus add` on a copy of it, builds the project with the Go toolchain, boots
the binary and reads what the application says and serves — or drives a real
plugin executable through the runtime that would call it. `TestCatalogBench`
asserts the **recorded verdict** rather than success, so closing a gap turns
the suite red with "this one is present now, update the verdict" — which is
what keeps this page honest.

There was already a description of this: a read-only survey of the code at
the start of the arc listed what `nucleus add` resolves, which catalog entries
are core packages and which do not exist, and that only `mail.send` has a
runtime bridge. Reading is a hypothesis. The measurement confirmed that
outline and found five things it did not say (below).

## How an entry is measured

The starter is the smallest project the CLI writes. The probes pin it to the
checkout under measurement the way the CLI's own build tests do: a `replace`
for the framework and its SQLite driver, and a `replace` for every other
module this repository publishes, so that `nucleus add <x>` — which runs
`go get <module>@<released version>` — resolves the module from this checkout
and not from the proxy (a `replace` without a version covers every version, so
the pinned `go get` stays offline). One starter is scaffolded and built per run;
every entry works on a copy, and the Go build cache makes the copies cheap.

An entry is **present** when `nucleus add <name>` on that starter leaves a
project that builds with the module graph read-only, boots, and does the job
the entry is for — read from the application, not from the project: a storage
provider logs that it was initialised (and talks to the bench's stand-in
object store), an exporter serves its metrics or reports itself enabled, a
directory joins the authentication chain. A project that builds and boots
while the selected entry does nothing is **partial**, and the probe boots the
starter WITHOUT the module under the same configuration to say whether the
selection was refused or ignored.

The core entries (oidc, apikeys, accounts, sql-queue, websockets) have nothing
to `go get`; their catalog entry is the wiring. Until `nucleus add` knows the
name, the probe wires the capability by hand — the blank import and the
configuration on the starter for oidc, in-process for the rest — and partial
means "it works, and the catalog does not do it for you". When the command
learns a name, the probe switches to the real command on the starter; the
session that teaches it a name also gives the probe its wiring check. `N1`
taught it `oidc`, whose wiring check already existed (the sign-in route must
answer); the other four stay out of the command until `N4` gives them a
recipe, because no import registers them.

Whether an entry is pinned to the certified set is one control for the whole
catalog (`CAT-03`), not fifteen.

The first run on a machine downloads the SDKs the provider modules wrap
(AWS, Google Cloud, Azure, OpenTelemetry, LDAP); after that the build cache
answers. The probes that scaffold skip under `-short`, and a probe whose
module download fails because the proxy is unreachable skips and says so —
it never records a verdict for the network.

## The verdicts

| verdict | meaning |
|---|---|
| **present** | the control exists and its probe exercised it end to end |
| **partial** | a piece exists; the case records exactly what is missing |
| **absent** | no surface at all — the probe measures the absence: a refused name, a refusal that names nothing, a plugin nothing calls |

A control that cannot be probed does not belong in the bench. Where a
capability does not exist yet, the probe asks for it under the names other
ecosystems give it — `nucleus add redis` as well as `redis-cache`, a
`Serve` helper the way hashicorp/go-plugin has one, `--template module` and
`generate plugin` — and logs every place it looked, which is the honest form
of an absence.

## The result

**17 of 38 controls present. 7 partial. 14 absent.**

| family | present | partial | absent |
|---|---|---|---|
| catalog | 9 | 1 | 1 |
| entries | 6 | 5 | 4 |
| plugins | 2 | 1 | 9 |
| **total** | **17** | **7** | **14** |

### catalog — 9 present · 1 partial · 1 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `CAT-01` | every "not installed" refusal names the `nucleus add` that installs what it names | **present** | — |
| `CAT-02` | `nucleus add --help` lists every name the command accepts | **present** | — |
| `CAT-03` | an entry installs the certified set's version of its module | **present** | — |
| `CAT-04` | `nucleus new` fetches what it scaffolds at the set's versions | **partial** | go.mod pins the framework and the scaffold fetches the driver (and every --with module of this repository) at the version released with the CLI; quark and its driver, orbit and the bridges are fetched with no version — and so is a suite product `nucleus add` fetches. Their versions are the umbrella's certified set (versions.yaml), cut after this CLI is tagged (Nucleus tags first and Orbit requires the Nucleus it is cut against), so no release of this repository can carry them: the pin needs the set to travel with the CLI from the umbrella. |
| `CAT-05` | an entry can carry more than `go get` and a blank import (a Mount, a configuration block) | **absent** | the only thing an entry is, is a module path: `nucleus add` writes `import _` and nothing else, and the core entries that need wiring (accounts, apikeys, websockets, sql-queue) are not in the table at all. |
| `CAT-06` | adding an entry that is already there changes nothing | **present** | — |
| `CAT-07` | a mistyped name gets the nearest entry suggested | **present** | — |
| `CAT-08` | after `nucleus add`, the person is told the configuration the entry reads | **present** | — |
| `CAT-09` | the site's CLI reference lists every entry the command accepts | **present** | — |
| `CAT-10` | one catalogue: what `nucleus add` installs and what `nucleus new --with` resolves | **present** | — |
| `CAT-11` | an application links only the entries it added | **present** | — |

### entries — 6 present · 5 partial · 4 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `EN-01` | oidc — `nucleus add oidc` wires federated sign-in on the starter | **partial** | `nucleus add oidc` writes the blank import of pkg/auth/federated/oidc and names auth_federated, and the starter with an auth_federated block builds the federated set — but the sign-in routes are still the application's to mount (GET /auth/corp/start answers 404). |
| `EN-02` | saml — `nucleus add saml` installs a SAML identity provider | **absent** | no SAML provider anywhere: no package under pkg/auth/federated, no module under providers/, no dependency on crewjam/saml or gosaml2, nothing registers "saml" with the federated registry. |
| `EN-03` | apikeys — `nucleus add apikeys` puts API-key authentication on the starter | **partial** | works by hand — a SQL store, apikeys.Issue and apikeys.Middleware around a handler accept a valid key and refuse a forged one — but `nucleus add apikeys` is an unknown name and nothing mounts the middleware for you. |
| `EN-04` | accounts — `nucleus add accounts` mounts the account flows on the starter | **partial** | works by hand — POST /auth/register answers 202 — but `nucleus add accounts` is an unknown name, and the hand wiring is more than a Mount line: accounts.Module takes a finished *Service, so the author opens a *sql.DB of their own BEFORE the application is built and supplies a Mailer of their own (without one, registration answers 500; the api starter has no mailer at all). |
| `EN-05` | sql-queue — `nucleus add sql-queue` gives the starter a durable job queue | **partial** | works by configuration plus code — jobs_provider: sql and a module that registers a job; the queue does not exist until one does — but `nucleus add sql-queue` is an unknown name. |
| `EN-06` | redis-cache — `nucleus add redis-cache` gives pkg/cache a Redis backend | **absent** | pkg/cache has a memory and a SQL backend and no Redis one (its own docs say so), although go-redis is already in the core graph for sessions, the asynq queue and the realtime relay. |
| `EN-07` | websockets — `nucleus add websockets` serves a real-time channel on the starter | **partial** | works by hand — a hub the application owns and a route that calls realtime.ServeWS complete the handshake and deliver a broadcast — but `nucleus add websockets` is an unknown name. |
| `EN-08` | stripe — `nucleus add stripe` installs a billing provider | **absent** | no Stripe module, no stripe-go dependency, and the plugin SDK's subscription.create/cancel capabilities are a "stretch" line in the reference with no schema in pkg/plugins. |
| `EN-09` | sentry — `nucleus add sentry` reports the application's errors | **absent** | no Sentry module and no sentry-go dependency. The seam such a module would register on exists — the request-interceptor registry behind http_interceptors, which sees every request and its status — and nothing uses it to report. |
| `EN-10` | s3 — `nucleus add s3` gives the starter S3 storage | **present** | — |
| `EN-11` | gcs — `nucleus add gcs` gives the starter Google Cloud Storage | **present** | — |
| `EN-12` | azure — `nucleus add azure` gives the starter Azure Blob storage | **present** | — |
| `EN-13` | ldap — `nucleus add ldap` puts a directory in the starter's authentication chain | **present** | — |
| `EN-14` | otlp — `nucleus add otlp` exports the starter's telemetry over OTLP | **present** | — |
| `EN-15` | prometheus — `nucleus add prometheus` serves the starter's metrics | **present** | — |

### plugins — 2 present · 1 partial · 9 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `EX-01` | an example external plugin builds in a test and passes `nucleus plugin test --execute` | **absent** | no example plugin in the repository: no `package main` under a nucleus-plugin-*, example-plugin, examples/plugins or testdata/plugins directory. A plugin author implements the envelope from the reference alone. |
| `EX-02` | `mail.send` reaches an external plugin through the runtime | **present** | — |
| `EX-03` | `queue.publish` has a runtime bridge to an external plugin | **absent** | the payload schema exists and nothing sends it: the outbox's bridge types are webhook and a disabled kafka, and a plugin-backed bridge (type plugin, external, exec or nucleus-plugin) boots with a WARN and is dropped. |
| `EX-04` | `webhook.deliver` has a runtime bridge to an external plugin | **absent** | the payload schema exists and nothing sends it: outbound webhooks are the outbox's built-in HTTP bridge, and no bridge type hands a delivery to a plugin. |
| `EX-05` | an in-process example — a provider or a module — ships as a fixture tested in CI | **absent** | no directory named example or sample holds a tested provider, module or extension. The first-party modules under providers/ and exporters/ register the same way, but they are production code with their own SDKs, not a starting point. |
| `EX-06` | a community module template builds standalone and its test calls nucleustest.CheckModule | **absent** | the CLI writes applications (mvc, api, suite) and slices inside one (`generate module`); nothing writes a standalone module with its own go.mod: --template module/plugin/extension and generate plugin/extension/provider are all refused. |
| `EX-07` | `nucleus <name>` dispatches to a `nucleus-<name>` binary end to end | **present** | — |
| `EX-08` | the external commands on PATH are discoverable from the CLI | **absent** | neither `nucleus --help`, `nucleus help` nor `nucleus plugin list` mentions a nucleus-<name> command on PATH; a person finds one by knowing it is there. |
| `EX-09` | the plugin reference points a plugin author at a runnable example | **absent** | docs/reference/PLUGIN_SDK.md says it in so many words: no runnable example plugin ships in-tree, and no release is promised for one. |
| `EX-10` | `nucleus plugin test --execute` exercises the envelope, not only discovery | **partial** | --execute re-runs the capability listing and reports ok; it never sends a request envelope, so a plugin that answers every request with garbage and exit code 50 passes it. |
| `EX-11` | a plugin author has an SDK side: a helper that serves the envelope | **absent** | pkg/plugins is the host side only (discover, probe, execute); a plugin author re-implements the request and response envelopes, exit codes included, from the reference. |
| `EX-12` | an external plugin runs only when the configuration allows it | **absent** | any nucleus-plugin-<driver> on PATH that advertises mail.send is executed for mail_driver: <driver>; the reference's allowlist (plugins.allowed, allow_external) is marked "proposed" and the configuration does not know the keys. |

## What the shape of it says

**The command works, and since `N1` it is one table pinned to the release.**
For the module-backed entries `nucleus add` does what it says — the `go get`,
the blank import, a second run that changes nothing — and on the starter all
six of them (s3, gcs, azure, ldap, otlp, prometheus) go all the way to a
running application that uses them. At `S0` the three storage entries stopped
short of that: the api starter built no storage, so `storage.provider: s3` was
ignored without a word (`EN-10`…`EN-12`, closed in `N2` with `WithStorage()`).
Every entry of this repository is fetched at the version released with the CLI
(`CAT-03`): `internal/knownproviders/modules.json`, embedded in the CLI and
rewritten by release-please in the release PR that tags each module
(ADR-034). `nucleus add`, its help, `nucleus new --with`, the site's CLI
reference and the runtime's refusals read the same table (`CAT-02`, `CAT-09`,
`CAT-10`); a typo gets the nearest name (`CAT-07`); after an install the
command names the key that selects the entry (`CAT-08`). What stays floating
is the suite: orbit, quark and the bridges are versioned by the umbrella's
certified set, cut after this CLI is tagged, so `nucleus new --with quark` and
`nucleus add quark` fetch them at the proxy's latest tag and say so
(`CAT-04`).

**An entry that needs more than an import is still not expressible.** The
table has a field for what wires an entry, and `nucleus add` prints it, but
writes nothing beyond the import (`CAT-05`). `oidc` is in the catalog because
its import is its registration — `nucleus add oidc` writes it and names
`auth_federated`, and the sign-in routes remain the application's to mount
(`EN-01`). The other core entries (accounts, apikeys, sql-queue, websockets)
register nothing by import; they stay out of the table until the session that
gives an entry a wiring recipe (`N4`), and each still works only by hand
(`EN-03`, `EN-04`, `EN-05`, `EN-07`).

**Four entries do not exist; three of them already have a place to land.**
saml, redis-cache, stripe and sentry have no code and no dependency anywhere
(`EN-02`, `EN-06`, `EN-08`, `EN-09`). SAML has the federated registry OIDC
registers in; a reporter has the request-interceptor registry behind
`http_interceptors` (`pkg/router/interceptor/registry.go`); redis-cache has
the client — go-redis is in the core graph for sessions, the asynq queue and
the realtime relay — and no `pkg/cache` backend over it. Stripe has nothing:
the plugin reference's `subscription.*` capabilities are a stretch line with
no schema.

**The plugin contract is executable for one capability and documented for
three.** `mail.send` reaches an external plugin through the runtime and
`nucleus-<name>` commands dispatch end to end (`EX-02`, `EX-07`). The rest of
the plugin story is the host side alone: no example plugin, no in-process
example, no module template, no helper for the plugin side of the envelope,
no runtime bridge for `queue.publish` or `webhook.deliver`, and the reference
says so about the example in so many words (`EX-01`, `EX-03`…`EX-06`,
`EX-09`, `EX-11`).

## What the bench found that the reading did not

1. **The api starter never builds storage.** It runs `WithoutDefaults()`, which
   skips the storage subsystem, so `nucleus add s3` (or gcs, or azure) on it
   builds and boots and `storage.provider: s3` is ignored without a word — the
   starter without the module boots identically (`EN-10`…`EN-12`). On the mvc
   template the same selection is honoured: refused without the module, and
   with it the provider initialises against the configured endpoint (checked
   by hand while writing the probes). The arc's starter is the one where half
   of the module entries do nothing. *Closed in `N2` (NU-99):* `WithStorage()`
   builds the storage a `WithoutDefaults()` application's configuration
   declares and nothing while it declares none; the api starter carries it,
   and without it a declared storage block is still ignored — the
   application starts, as it did before (QADR-0010) — but with one ERROR
   line at boot and a `nucleus doctor` warning naming the option; it
   refuses to start from v2.0.0 (DEP-2026-013). `WithoutDefaults()` alone
   still builds no storage.
2. **Every driver module linked all five engines.** The starter imported only
   the SQLite driver, and its binary carried pgx, go-sql-driver/mysql,
   go-mssqldb and go-ora: each driver module imported `internal/dbclassify` for
   its predicate, and that package's `link.go` blank-imported every engine at
   package level (`CAT-11`). The consequence the reading could not see: the
   postgres "not imported, run `nucleus add postgres`" refusal never appeared
   in an application that added any driver — a postgres URL simply connected
   (`CAT-01`). This was NU-8 measured at the application rather than at the
   CLI. **Closed in `N3`:** each driver module now registers its own
   classifier, typed to its own engine; `internal/dbclassify` imports nothing
   but the standard library, and the package that links every engine,
   `internal/alldrivers`, is imported by the CLI and the test binaries only.
   The starter went from 137 modules, 536 packages and a 49.6 MB stripped
   binary to 108, 460 and 29.0 MB; the postgres URL is refused with
   `nucleus add postgres` (linked right now: sqlite).
   `TestEachDriverModuleLinksOnlyItsEngine` holds each driver module to its
   own engine.
3. **The ldap refusal is unreachable.** A starter configured for ldap and
   missing the module is refused by the strict configuration check first —
   `auth.ldap.url (did you mean databases.<alias>.url?)` among the unknown
   keys — so the auth chain's "this ships as its own module, go get it"
   message never runs for a correctly configured directory (`CAT-01`).
   *Closed in `N2` (NU-100):* both configuration paths now tag a published
   backend's keys ``not installed: `nucleus add ldap` `` and say once which
   module they configure; the storage, directory and OIDC refusals name the
   command beside the import, as the driver and exporter ones already did.
4. **`nucleus plugin test --execute` does not execute.** It re-runs the
   capability listing; a plugin that answers every request envelope with
   garbage and exit code 50 passes it as "execute smoke check passed"
   (`EX-10`). The reference calls it a contract smoke.
5. **External plugins run without an allowlist.** Any
   `nucleus-plugin-<driver>` on PATH that advertises `mail.send` receives the
   application's mail for `mail_driver: <driver>`; the reference's safety
   rules require the binary to be allowlisted, its configuration model is
   marked "proposed", and the configuration loader refuses the `plugins.*`
   keys as unknown (`EX-12`).

And two things seen in passing, outside this bench's controls:

- With `jobs_provider: sql`, stopping an application takes about nine seconds
  (the memory provider: under a millisecond), and during them the scheduler
  keeps firing ticks against a database that is already closed ("sqlprovider:
  scheduled tick could not be enqueued: sql: database is closed", once a
  second) until its leader lease fails to renew. `EN-05` pays those seconds on
  every run.
- `nucleus new` ended with "See the docs Quickstart and examples/mvc_api",
  a directory that was removed with the rest of `examples/` (NU-104). Since
  `N1` it points at the quickstart and the chosen template's guide, and a
  test checks both pages exist.

## What N1 found that the plan did not say

1. **release-please resolves an extra file relative to its package.** A
   package at `drivers/mssql` listing `internal/knownproviders/modules.json`
   would update `drivers/mssql/internal/knownproviders/modules.json`, a file
   that does not exist, and nothing would fail. A leading `/` makes the path
   relative to the repository root (`BaseStrategy.addPath` in release-please
   17.6.0, the version `release-please-action` v5.0.0 bundles); running that
   version's Go strategy and merge code over this configuration moved all
   thirteen keys of the file in one combined update.
2. **A version in a golden file breaks on the next release.** The help of
   `nucleus new` lists the catalog; printing each module's version there would
   have turned the help golden red on the first release PR, which does not run
   CI. The versions are printed by `nucleus add --help`, which has no golden.
3. **`CAT-01` and `CAT-04` cannot see a regression in what `N1` fixed.** Both
   stay partial for reasons outside this session (the api starter's storage,
   NU-8, the suite's set), so un-pinning the scaffold's driver or dropping the
   catalog from the federated refusal leaves the verdict unchanged. The unit
   tests in `internal/cli` and `pkg/auth` are what fail on those mutations.

## What "pinned to the certified set" means here

For a module of this repository the set's version is the one
`.release-please-manifest.json` records for it at this commit: the release
that carries this CLI publishes exactly those tags. Since `N1` the CLI carries
that manifest as `internal/knownproviders/modules.json`, and every
release-please package rewrites its own key of it in the release PR (an extra
file of type `json`, the path with a leading `/` so release-please resolves it
at the repository root rather than under the package). `CAT-03` is present
because every `nucleus add --dry-run` reads `go get <module>@v<manifest
version>`; a test fails when the file and the manifest differ, and another
when a package has no extra file for it. The suite's other products (orbit,
quark and the bridges `nucleus new --with` fetches, `CAT-04`) are versioned by
the umbrella's set, which this repository does not hold and cannot: the set is
certified after Nucleus is tagged. Their pin is the same question asked of the
umbrella.

## What this bench does not measure

Whether an entry's module itself is correct — its storage semantics, its
exporter's protocol, its directory queries — is measured where the module
lives (the storage-minio, providers-ldap and drivers-and-exporters lanes).
The panel that would list installed entries is Orbit's surface. And the
published module proxy is never consulted: a probe that installs from the
proxy would measure the network and yesterday's release, not this checkout.
