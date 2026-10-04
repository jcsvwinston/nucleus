# Catalog bench — what extending an application can do today

This is the numerator of the A11 gate ("extensibility and catalog"). It exists
because that gate needs a number, and a number needs something that produces
it.

**Measured on 2026-10-04 against v1.30.1 — the session that opened the arc
(`S0`).** Run it with:

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
`go get` with no version — resolves the module from this checkout and not
from whatever the proxy holds. One starter is scaffolded and built per run;
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
session that teaches it a name also gives the probe its wiring check.

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

**6 of 38 controls present. 15 partial. 17 absent.**

| family | present | partial | absent |
|---|---|---|---|
| catalog | 1 | 6 | 4 |
| entries | 3 | 8 | 4 |
| plugins | 2 | 1 | 9 |
| **total** | **6** | **15** | **17** |

### catalog — 1 present · 6 partial · 4 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `CAT-01` | every "not installed" refusal names the `nucleus add` that installs what it names | **partial** | measured by booting the starter with each entry selected and not added: the OTLP and Prometheus refusals name `nucleus add`; the postgres URL is not refused at all (the SQLite driver module links every engine, NU-8), s3/gcs/azure are ignored by the api starter, ldap is refused as `unknown configuration key(s) ... auth.ldap.url (did you mean databases.<alias>.url?)` before the auth hint can run, and oidc's refusal names neither the package nor the command. |
| `CAT-02` | `nucleus add --help` lists every name the command accepts | **partial** | `nucleus add aws-sm` installs providers/secrets-aws and --help never mentions it: the help is built from four of the table's five groups. |
| `CAT-03` | an entry installs the certified set's version of its module | **absent** | every entry runs `go get <module>` with no version, which resolves @latest from the proxy. The CLI knows the set for exactly one module — the framework it pins in a scaffold, rewritten by release-please — and nothing for the twelve modules .release-please-manifest.json versions beside it. |
| `CAT-04` | `nucleus new` fetches what it scaffolds at the set's versions | **partial** | go.mod pins the framework to the CLI's own version; the driver and every --with module (orbit, quark and its driver, the bridges) are fetched with no version, and the CLI has no record of the set's orbit and quark. |
| `CAT-05` | an entry can carry more than `go get` and a blank import (a Mount, a configuration block) | **absent** | the only thing an entry is, is a module path: `nucleus add` writes `import _` and nothing else, and the core entries that need wiring (accounts, apikeys, websockets, sql-queue) are not in the table at all. |
| `CAT-06` | adding an entry that is already there changes nothing | **present** | — |
| `CAT-07` | a mistyped name gets the nearest entry suggested | **partial** | `nucleus add prometeus` prints the whole table under "available:" and leaves the person to find the name in it; no nearest match. |
| `CAT-08` | after `nucleus add`, the person is told the configuration the entry reads | **absent** | after `nucleus add s3`, `ldap` or `otlp` nothing names storage.provider, auth_backends or otlp_endpoint — neither the output, the dry run nor nucleus.yml — and an installed entry does nothing until it is selected. |
| `CAT-09` | the site's CLI reference lists every entry the command accepts | **partial** | the `nucleus add` row of the CLI reference names the drivers, exporters, storage providers and ldap, and not aws-sm. |
| `CAT-10` | one catalogue: what `nucleus add` installs and what `nucleus new --with` resolves | **absent** | two tables: `--with` knows the suite's siblings (orbit, quark, the two bridges) and `add` knows the optional modules; `nucleus add quark` and `nucleus new --with s3` are both refused. |
| `CAT-11` | an application links only the entries it added | **partial** | the starter adds only the SQLite driver and its binary links pgx, go-sql-driver/mysql, go-mssqldb and go-ora: every driver module imports internal/dbclassify, whose link.go blank-imports all five engines (NU-8, measured here at the application). The storage, exporter and directory modules stay out until added. |

### entries — 3 present · 8 partial · 4 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `EN-01` | oidc — `nucleus add oidc` wires federated sign-in on the starter | **partial** | works by hand — a blank import of pkg/auth/federated/oidc and an auth_federated block, and the starter builds the federated set — but `nucleus add oidc` is an unknown name, and the sign-in routes are still the application's to mount (GET /auth/corp/start answers 404). |
| `EN-02` | saml — `nucleus add saml` installs a SAML identity provider | **absent** | no SAML provider anywhere: no package under pkg/auth/federated, no module under providers/, no dependency on crewjam/saml or gosaml2, nothing registers "saml" with the federated registry. |
| `EN-03` | apikeys — `nucleus add apikeys` puts API-key authentication on the starter | **partial** | works by hand — a SQL store, apikeys.Issue and apikeys.Middleware around a handler accept a valid key and refuse a forged one — but `nucleus add apikeys` is an unknown name and nothing mounts the middleware for you. |
| `EN-04` | accounts — `nucleus add accounts` mounts the account flows on the starter | **partial** | works by hand — POST /auth/register answers 202 — but `nucleus add accounts` is an unknown name, and the hand wiring is more than a Mount line: accounts.Module takes a finished *Service, so the author opens a *sql.DB of their own BEFORE the application is built and supplies a Mailer of their own (without one, registration answers 500; the api starter has no mailer at all). |
| `EN-05` | sql-queue — `nucleus add sql-queue` gives the starter a durable job queue | **partial** | works by configuration plus code — jobs_provider: sql and a module that registers a job; the queue does not exist until one does — but `nucleus add sql-queue` is an unknown name. |
| `EN-06` | redis-cache — `nucleus add redis-cache` gives pkg/cache a Redis backend | **absent** | pkg/cache has a memory and a SQL backend and no Redis one (its own docs say so), although go-redis is already in the core graph for sessions, the asynq queue and the realtime relay. |
| `EN-07` | websockets — `nucleus add websockets` serves a real-time channel on the starter | **partial** | works by hand — a hub the application owns and a route that calls realtime.ServeWS complete the handshake and deliver a broadcast — but `nucleus add websockets` is an unknown name. |
| `EN-08` | stripe — `nucleus add stripe` installs a billing provider | **absent** | no Stripe module, no stripe-go dependency, and the plugin SDK's subscription.create/cancel capabilities are a "stretch" line in the reference with no schema in pkg/plugins. |
| `EN-09` | sentry — `nucleus add sentry` reports the application's errors | **absent** | no Sentry module and no sentry-go dependency. The seam such a module would register on exists — the request-interceptor registry behind http_interceptors, which sees every request and its status — and nothing uses it to report. |
| `EN-10` | s3 — `nucleus add s3` gives the starter S3 storage | **partial** | adds, builds and boots, and storage is never built: the api starter runs WithoutDefaults(), which skips storage, so `storage.provider: s3` is ignored without a word — the starter without the module boots identically. On the mvc template the same selection is honoured. |
| `EN-11` | gcs — `nucleus add gcs` gives the starter Google Cloud Storage | **partial** | adds, builds and boots; the api starter never builds storage, so `storage.provider: gcs` is ignored (see EN-10). |
| `EN-12` | azure — `nucleus add azure` gives the starter Azure Blob storage | **partial** | adds, builds and boots; the api starter never builds storage, so `storage.provider: azure` is ignored (see EN-10). |
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

**The command works; the catalog is six names deep and floats.** For the
module-backed entries `nucleus add` does what it says — the `go get`, the blank
import, a second run that changes nothing — and on the starter three of them
(ldap, otlp, prometheus) go all the way to a running application that uses
them. But every entry installs `@latest` (`CAT-03`): the CLI pins exactly one
module, the framework itself, to its own version, and knows nothing of the
twelve module versions `.release-please-manifest.json` records beside it. The
catalog the owner decided on — embedded and pinned to the certified set — is a
table of module paths today, not of versions.

**An entry is a module path and nothing else.** There is no field for a Mount,
a configuration block or a route (`CAT-05`), so the five core entries cannot
be expressed at all: they are refused as unknown names, and each works by hand
(`EN-01`, `EN-03`, `EN-04`, `EN-05`, `EN-07`). After an install the command is
silent about the configuration that selects what it installed (`CAT-08`), so
`nucleus add s3` followed by nothing does nothing.

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
   of the module entries do nothing.
2. **Every driver module links all five engines.** The starter imports only
   the SQLite driver, and its binary carries pgx, go-sql-driver/mysql,
   go-mssqldb and go-ora: each driver module imports `internal/dbclassify` for
   its predicate, and that package's `internal/dbclassify/link.go`
   blank-imports every engine at package level (`CAT-11`). The consequence the
   reading could not see: the postgres "not imported, run `nucleus add
   postgres`" refusal never appears in an application that added any driver —
   a postgres URL simply connects (`CAT-01`). This is NU-8 measured at the
   application rather than at the CLI.
3. **The ldap refusal is unreachable.** A starter configured for ldap and
   missing the module is refused by the strict configuration check first —
   `auth.ldap.url (did you mean databases.<alias>.url?)` among the unknown
   keys — so the auth chain's "this ships as its own module, go get it"
   message never runs for a correctly configured directory (`CAT-01`).
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
- `nucleus new` still ends with "See the docs Quickstart and examples/mvc_api",
  a directory that was removed with the rest of `examples/`.

## What "pinned to the certified set" means here

`CAT-03` needs a definition the CLI could meet, because today there is nothing
to compare against. For a module of this repository the set's version is the
one `.release-please-manifest.json` records for it at this commit: the release
that carries this CLI publishes exactly those tags, and release-please already
rewrites the framework's own pin in the CLI source (the
`x-release-please-version` marker on the scaffold's framework version) on
every release. The control is present when every `nucleus add --dry-run` reads
`go get <module>@v<manifest version>`. The suite's other products (orbit,
quark and the bridges `nucleus new --with` fetches, `CAT-04`) are versioned by
the umbrella's set, which this repository does not hold; their pin is the same
question asked of the umbrella.

## What this bench does not measure

Whether an entry's module itself is correct — its storage semantics, its
exporter's protocol, its directory queries — is measured where the module
lives (the storage-minio, providers-ldap and drivers-and-exporters lanes).
The panel that would list installed entries is Orbit's surface. And the
published module proxy is never consulted: a probe that installs from the
proxy would measure the network and yesterday's release, not this checkout.
