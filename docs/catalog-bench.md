# Catalog bench — what extending an application can do today

This is the numerator of the A11 gate ("extensibility and catalog"). It exists
because that gate needs a number, and a number needs something that produces
it.

**Measured on 2026-10-05 against v1.31.0.** The session that opened the arc
(`S0`) measured 6 of 38; `N1` — one catalog, pinned to the release — moved
`CAT-02`, `CAT-03`, `CAT-07`, `CAT-08`, `CAT-09` and `CAT-10` to present; `N2`
gave the api starter the storage the catalog installs (`EN-10`…`EN-12`) and
the refusals of the published backends the command that installs them;
`N3` gave each driver module its own classifier, so an application links only
the engine it added (`CAT-11`) and a postgres URL in a SQLite application is
refused naming `nucleus add postgres` (`CAT-01`): 17 of 38. `N4` — entries
that carry their wiring — moved `CAT-05`, `EN-01`, `EN-03` and `EN-05`: 21 of
38. `N10` gave the plugin side its half — `plugins.Serve` for a plugin
author (`EX-11`), an example plugin CI builds and runs through the real
runtime (`EX-01`) that the reference points at (`EX-09`), `nucleus plugin
test --execute` sending real envelopes (`EX-10`), the external commands in
the help and in `plugin list` (`EX-08`), and an opt-in allowlist in the
configuration (`EX-12`): 27 of 38. `N11` closed the family — the outbox
hands `queue.publish` and `webhook.deliver` to an external plugin through a
bridge of type `plugin` (`EX-03`, `EX-04`), an in-process example ships as a
tested fixture (`EX-05`), and `nucleus new --template module` writes a
module repository whose test calls `nucleustest.CheckModule` (`EX-06`): 31
of 38. `N7` — the Sentry module, and the core seam it reports through —
moved `EN-09`: 32 of 38. `N6` gave `pkg/cache` a Redis backend the
configuration selects and `nucleus add redis-cache` installs, and two
instances of the starter share what one of them caches (`EN-06`): 33 of
38. `N5` built the account flows and a realtime hub from the runtime and gave
both a recipe (`EN-04`, `EN-07`): 35 of 38. `N8` — `nucleus add saml`
installs a SAML 2.0 service provider, `providers/auth-saml`, and a sign-in
through it runs end to end on the starter — moved `EN-02`: 36 of 38. Run it
with:

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

The core entries (oidc, apikeys, accounts, sql-queue, redis-cache,
websockets) have nothing to `go get`; their catalog entry is the wiring. Until `nucleus add` knows the
name, the probe wires the capability by hand — the blank import and the
configuration on the starter for oidc, in-process for the rest — and partial
means "it works, and the catalog does not do it for you". When the command
learns a name, the probe switches to the real command on the starter: it
builds the project `nucleus add` left, boots it with the `nucleus.yml` the
command wrote, and runs the entry's **wiring check** against the running
application. The session that teaches the command a name writes that check
in the same change, and a probe without one cannot record present (until
`N4` the shared measurement recorded present for an accepted name with no
check; it now records partial and says so).

Since `N4` three core names are in the command, each with its check;
`N6` added a fourth and `N5` the last two:

- **oidc** — the probe does what the person does after the command: it
  replaces the placeholder issuer the recipe wrote with the address of the
  bench's stand-in OpenID Connect provider (discovery, a published RSA key,
  a token endpoint that checks the PKCE verifier). The check is a sign-in end
  to end: `/auth/corp/start` must redirect to the provider with the callback
  the operator registers, and `/auth/corp/callback` must exchange the code,
  verify the id_token (key, audience, issuer, nonce) and answer 200 with the
  identity.
- **apikeys** — a key issued with the real `nucleus apikey create` into the
  running application's own database is accepted, a forged one is refused
  with 401, and a request without one still passes.
- **sql-queue** — a job has to run on the durable queue. No entry can write
  the job (it is the application's), so the probe adds the one thing a
  person adds — a module that registers a job — to the project the command
  left; the check reads the application's SQLite file for finished runs in
  `nucleus_jobs` and the boot log for `provider=sql`. On the in-process
  queue the job runs too and the table does not exist: the rows are what
  tell the two apart.

- **redis-cache** (since `N6`) — the probe adds the one thing a person
  adds, a module that takes the cache with `nucleus.CacheFrom` in `OnStart`
  and caches through it on two routes, and points `cache.redis_url` at the
  bench's server: a real Redis when `NUCLEUS_CACHE_REDIS_URL` names one (the
  "Module Jobs (real Redis)" lane sets it), an in-process miniredis
  otherwise. The check starts a SECOND instance of the same binary with the
  same configuration: a value written through the framework's cache in the
  first is read in the second, and the server holds it under
  `nucleus:cache:`. The memory cache answers the first instance and not the
  second.

- **accounts** — the person's flow, on the starter as the command left it:
  `POST /auth/register` answers 202 and the confirmation mail reaches the
  application's log (the recipe writes `mail_driver: log`, which is where the
  person reads the link in development, and where the probe reads its
  token); sign-in answers 403 before the address is confirmed, the link
  answers 200, and sign-in then answers 200.
- **websockets** — what goes on a topic is the application's, so the probe
  adds the one thing a person adds: a module that takes the hub with
  `nucleus.RealtimeFrom` and publishes from a handler. The check opens a
  WebSocket on `/realtime/bench`, the route the recipe serves, and has to
  receive a frame the handler published; the subscription is taken after the
  handshake, so it publishes until a frame arrives.
- **accounts** — the person's flow, on the starter as the command left it:
  `POST /auth/register` answers 202 and the confirmation mail reaches the
  application's log (the recipe writes `mail_driver: log`, which is where the
  person reads the link in development, and where the probe reads its
  token); sign-in answers 403 before the address is confirmed, the link
  answers 200, and sign-in then answers 200.
- **websockets** — what goes on a topic is the application's, so the probe
  adds the one thing a person adds: a module that takes the hub with
  `nucleus.RealtimeFrom` and publishes from a handler. The check opens a
  WebSocket on `/realtime/bench`, the route the recipe serves, and has to
  receive a frame the handler published; the subscription is taken after the
  handshake, so it publishes until a frame arrives.

Since `N7` module entries have checks too, because what proves them is not
in the boot log:

- **sentry** — the probe does what the person does after the command: it
  replaces the empty `dsn` the recipe wrote with the DSN of the bench's
  stand-in Sentry (an httptest server that accepts what the SDK posts to a
  project's envelope endpoint), and adds what an application already has:
  a module with a route whose handler returns an error and one that panics.
  The check drives both through the running starter: each must answer 500,
  and each must arrive at the stand-in as an event with its level (`error`,
  `fatal`), the route template, the status, the request id the response
  carried, and the error or the panic value.
- **saml** (since `N8`) — a module entry whose recipe mounts the same
  `FederatedSignIn()` and writes an `auth_federated` block. The bench cannot
  sign SAML itself — XML signatures need a library this module does not
  depend on — so its stand-in identity provider is the SAML module's own
  `samltest`, compiled as a program inside the project `nucleus add saml`
  left (whose module graph already holds the library) and run beside the
  application; the probe points `idp_metadata_url` at it. The check is what
  a browser does: `/auth/corp/metadata` serves the service-provider
  metadata, `/auth/corp/start` redirects to the identity provider with an
  AuthnRequest, the identity provider's auto-posting form posted to the
  callback answers 200 for `bench-user`, and the same form posted again does
  not. The refusals — an unsigned or forged assertion, another audience, an
  expired window, a replay, the wrapping shapes — are the module's own tests,
  not the bench's.

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

**36 of 38 controls present. 1 partial. 1 absent.**

| family | present | partial | absent |
|---|---|---|---|
| catalog | 10 | 1 | 0 |
| entries | 14 | 0 | 1 |
| plugins | 12 | 0 | 0 |
| **total** | **36** | **1** | **1** |

### catalog — 10 present · 1 partial · 0 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `CAT-01` | every "not installed" refusal names the `nucleus add` that installs what it names | **present** | — |
| `CAT-02` | `nucleus add --help` lists every name the command accepts | **present** | — |
| `CAT-03` | an entry installs the certified set's version of its module | **present** | — |
| `CAT-04` | `nucleus new` fetches what it scaffolds at the set's versions | **partial** | go.mod pins the framework and the scaffold fetches the driver (and every --with module of this repository) at the version released with the CLI; quark and its driver, orbit and the bridges are fetched with no version — and so is a suite product `nucleus add` fetches. Their versions are the umbrella's certified set (versions.yaml), cut after this CLI is tagged (Nucleus tags first and Orbit requires the Nucleus it is cut against), so no release of this repository can carry them: the pin needs the set to travel with the CLI from the umbrella. |
| `CAT-05` | an entry can carry more than `go get` and a blank import (a Mount, a configuration block) | **present** | — |
| `CAT-06` | adding an entry that is already there changes nothing | **present** | — |
| `CAT-07` | a mistyped name gets the nearest entry suggested | **present** | — |
| `CAT-08` | after `nucleus add`, the person is told the configuration the entry reads | **present** | — |
| `CAT-09` | the site's CLI reference lists every entry the command accepts | **present** | — |
| `CAT-10` | one catalogue: what `nucleus add` installs and what `nucleus new --with` resolves | **present** | — |
| `CAT-11` | an application links only the entries it added | **present** | — |

### entries — 14 present · 0 partial · 1 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `EN-01` | oidc — `nucleus add oidc` wires federated sign-in on the starter | **present** | — |
| `EN-02` | saml — `nucleus add saml` installs a SAML identity provider | **present** | — |
| `EN-03` | apikeys — `nucleus add apikeys` puts API-key authentication on the starter | **present** | — |
| `EN-04` | accounts — `nucleus add accounts` mounts the account flows on the starter | **present** | — |
| `EN-05` | sql-queue — `nucleus add sql-queue` gives the starter a durable job queue | **present** | — |
| `EN-06` | redis-cache — `nucleus add redis-cache` gives pkg/cache a Redis backend | **present** | — |
| `EN-07` | websockets — `nucleus add websockets` serves a real-time channel on the starter | **present** | — |
| `EN-08` | stripe — `nucleus add stripe` installs a billing provider | **absent** | out of the suite's scope by the owner's decision (2026-10-06): a billing seam and a Stripe module were built in A11 N9 and withdrawn before any release, as frameworks leave payments to an application package; no Stripe module, no stripe-go dependency, and the plugin SDK's subscription.create/cancel capabilities stay a "stretch" line in the reference. |
| `EN-09` | sentry — `nucleus add sentry` reports the application's errors | **present** | — |
| `EN-10` | s3 — `nucleus add s3` gives the starter S3 storage | **present** | — |
| `EN-11` | gcs — `nucleus add gcs` gives the starter Google Cloud Storage | **present** | — |
| `EN-12` | azure — `nucleus add azure` gives the starter Azure Blob storage | **present** | — |
| `EN-13` | ldap — `nucleus add ldap` puts a directory in the starter's authentication chain | **present** | — |
| `EN-14` | otlp — `nucleus add otlp` exports the starter's telemetry over OTLP | **present** | — |
| `EN-15` | prometheus — `nucleus add prometheus` serves the starter's metrics | **present** | — |

### plugins — 12 present · 0 partial · 0 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `EX-01` | an example external plugin builds in a test and passes `nucleus plugin test --execute` | **present** | — |
| `EX-02` | `mail.send` reaches an external plugin through the runtime | **present** | — |
| `EX-03` | `queue.publish` has a runtime bridge to an external plugin | **present** | — |
| `EX-04` | `webhook.deliver` has a runtime bridge to an external plugin | **present** | — |
| `EX-05` | an in-process example — a provider or a module — ships as a fixture tested in CI | **present** | — |
| `EX-06` | a community module template builds standalone and its test calls nucleustest.CheckModule | **present** | — |
| `EX-07` | `nucleus <name>` dispatches to a `nucleus-<name>` binary end to end | **present** | — |
| `EX-08` | the external commands on PATH are discoverable from the CLI | **present** | — |
| `EX-09` | the plugin reference points a plugin author at a runnable example | **present** | — |
| `EX-10` | `nucleus plugin test --execute` exercises the envelope, not only discovery | **present** | — |
| `EX-11` | a plugin author has an SDK side: a helper that serves the envelope | **present** | — |
| `EX-12` | an external plugin runs only when the configuration allows it | **present** | — |

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

**Since `N4` an entry carries its wiring.** A catalog row can hold a recipe:
calls spliced into the `nucleus.New()` chain of `main.go` with the editor
`generate module --mount` uses, the configuration block the entry reads
(written into `nucleus.yml` only when none of its keys is set there, printed
otherwise), the routes it serves and what is left to the person (ADR-035).
`nucleus add` and `nucleus new --with` apply it, a second run changes
nothing (`CAT-06` now re-adds oidc as well as prometheus), and `--dry-run`
prints it (`CAT-05`). Three core entries use it: `oidc` mounts
`nucleus.FederatedSignIn()`, the framework's pair of sign-in handlers, so
`/auth/<name>/start` stops answering 404 (`EN-01`); `apikeys` adds
`WithAPIKeys()` (`EN-03`); `sql-queue` writes `jobs_provider: sql` (`EN-05`).
Since `N5` the last two use it too (ADR-036): `accounts` adds `WithMail()` and
`Mount(accounts.FromRuntime())` — the flows built at start-up on the default
database, the application's mail sender and its sessions — and writes
`mail_driver: log` with the `modules.accounts` block (`EN-04`); `websockets`
adds `WithRealtime()`, a hub the application owns and its channels at
`GET /realtime/{topic}` (`EN-07`). Since `N7` and `N8` module entries carry
one too: `sentry` writes the block that puts it in the request path
(`EN-09`), and `saml` mounts the same `FederatedSignIn()` and writes its
`auth_federated` block (`EN-02`).

**One entry does not exist.** stripe has no code and no dependency anywhere
(`EN-08`), and nothing to land on: the plugin reference's `subscription.*`
capabilities are a stretch line with no schema. Sentry was a second until
`N7`: a module of its own, `providers/errors-sentry`, registered with the
request-interceptor registry, which `nucleus add sentry` installs and puts
in the request path (`EN-09`). redis-cache was a third until `N6`. SAML was
the fourth until `N8`: a module of its own, `providers/auth-saml`,
registered with the federated registry OIDC registers in, which `nucleus
add saml` installs and mounts (`EN-02`).

**Since `N6` the application has a cache, and redis-cache shares it.** The
framework builds one cache per application from the `cache` block — on
every stack, `WithoutDefaults()` included — and modules take it with
`nucleus.CacheFrom(rt)`: the in-memory cache when nothing is set,
`cache.provider: sql` over the default database, `cache.provider: redis`
over the backend `pkg/cache/rediscache` registers when imported.
`nucleus add redis-cache` writes the import and the block; selected and not
imported, the application does not start and names the command (`CAT-01`
asks it as a tenth refusal). The backend is in the framework module because
the measurement allowed it: go-redis is linked into every application
already (below).

**The plugin contract has both sides since `N10`, and since `N11` a
runtime bridge for each of its three capabilities.** `mail.send` reaches an
external plugin through the mail runtime, `queue.publish` and
`webhook.deliver` through the outbox, and `nucleus-<name>` commands dispatch
end to end (`EX-02`…`EX-04`, `EX-07`). A plugin author writes one handler per capability and
`plugins.Serve` speaks the envelope and the exit codes (`EX-11`);
`internal/fixtures/plugins/nucleus-plugin-maildir` is a complete mail
provider built on it, which its own test and this bench build, put on PATH
and reach through the mail runtime and `nucleus plugin test --execute`
(`EX-01`, `EX-09`). That command sends a real envelope per capability and
fails with the plugin's exit code and stderr (`EX-10`); `nucleus --help`,
`nucleus help` and `nucleus plugin list` list the external commands they
find (`EX-08`); and the configuration's `plugins` block decides which
external executables run, opt-in until v2.0.0 (`EX-12`, DEP-2026-014).

An outbox bridge of type `plugin` hands each committed message to
`nucleus-plugin-<provider>` as a `queue.publish` envelope (topic, the
message id as key, the payload JSON as body) or a `webhook.deliver` one (the
request the `webhook` bridge would send, signed the same way); the probes
boot the starter with the bridge, enqueue a message into its outbox table and
read the envelope the plugin receives and the row the outbox marks delivered
(`EX-03`, `EX-04`). The allowlist governs it as it governs mail, and the
plugin's exit code decides between the outbox's retry with backoff and its
dead letter. `internal/fixtures/plugins/nucleus-plugin-relay` serves both
capabilities behind an application's outbox in its own test, and
`internal/fixtures/inprocess/dirqueue` does the same `queue.publish`
delivery in process — a module that registers an outbox bridge written in
Go, held to `CheckModuleIn` (`EX-05`). `nucleus new <name> --template
module` writes a repository an application can `go get`: go.mod, the
module (a route, its policy row, typed configuration, a value it
`Provide`s), a test that calls `nucleustest.CheckModule`, a README and a
workflow that runs `go test`; the probe scaffolds it, points it at this
checkout and runs its tests outside any workspace, and is present only when
the test that calls `CheckModule` ran and passed (`EX-06`).

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
   (`EX-10`). The reference calls it a contract smoke. *Closed in `N10`
   (NU-101):* it sends a request envelope per capability — the one
   `--capability` names or every one the plugin advertises — with a sample
   payload or the one `--payload` holds, checks the response envelope,
   `accepted` and the echoed `request_id`, and fails with the plugin's exit
   code, its stderr in the report.
5. **External plugins run without an allowlist.** Any
   `nucleus-plugin-<driver>` on PATH that advertises `mail.send` receives the
   application's mail for `mail_driver: <driver>`; the reference's safety
   rules require the binary to be allowlisted, its configuration model is
   marked "proposed", and the configuration loader refuses the `plugins.*`
   keys as unknown (`EX-12`). *Closed in `N10` (NU-102):*
   `plugins.allow_external`, `plugins.allowed` and `plugins.commands` are
   part of the schema; when set, the mail runtime, `nucleus sendtestemail`,
   the `nucleus plugin` commands and the dispatcher of `nucleus <name>`
   refuse an executable they do not list before executing it. Nothing set
   runs everything, as before (QADR-0010), with one WARN at boot for an
   unlisted mail plugin and a `plugin doctor` warning; from v2.0.0 only the
   listed ones run (DEP-2026-014).

And two things seen in passing, outside this bench's controls:

- With `jobs_provider: sql`, stopping an application takes about nine seconds
  (the memory provider: under a millisecond), and during them the scheduler
  keeps firing ticks against a database that is already closed ("sqlprovider:
  scheduled tick could not be enqueued: sql: database is closed", once a
  second) until its leader lease fails to renew. Measured by hand,
  `EN-05` paid those seconds on every run; since `N4` it measures the
  starter, which the bench stops with a kill; the graceful stop was not
  measured again.
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

## What N4 found that the plan did not say

1. **`jobs_provider: sql` alone builds the queue.** `EN-05` had recorded that
   "the queue does not exist until a module registers a job". On the starter
   it does: a non-default provider builds the jobs runtime with no jobs (the
   NF-13 rule for enqueue-only applications), so the tables and the leader
   lease appear at boot. The recipe is therefore one line of configuration;
   what the control needed was a job to prove the queue runs, not wiring.
2. **The documented API-key composition never keyed the limiter on the key.**
   The reference said the key's owner lands in the context the rate limiter
   keys on. That holds only if the key middleware runs before the limiter,
   and every way an application could mount it — `Use` on the builder, `Use`
   on a module's router — puts it after, because the framework's stack is
   assembled inside `app.New`. `WithAPIKeys` mounts it next to the bearer
   decode; a test in `pkg/app` fails when it moves behind the limiter.
3. **The default-deny layer does not see a key's owner.** It resolves its
   subject from bearer claims, so on the default stack a route a program
   calls with a key is authorised for the anonymous subject and gated by
   `apikeys.Require`. Recorded in the reference; turning a key into a policy
   subject is a decision of its own, not a recipe.
4. **The instrument changed in three places.** `CAT-05` asked the four core
   names of S0's reading and was present only when all four wrote wiring;
   it now runs the real command on every core name the command accepts and
   is present when each wrote beyond its import and the catalog carries both
   a chain call and a configuration block — a name the command refuses is
   measured by its own entry control. The shared core measurement boots the
   configuration the command wrote instead of the starter's, and records
   partial, not present, when an entry has no wiring check. And `CAT-06`
   re-adds an entry with a recipe. Verified by mutation: a recipe that
   writes nothing drops `CAT-05` to absent and `EN-01`, `EN-03`, `EN-05` to
   partial; a chain call the editor no longer recognises drops `CAT-06` to
   partial; the queue block without `jobs_provider`, a callback that refuses
   the state, and a missing key middleware each drop their entry.

## What N10 found that the plan did not say

1. **There is no plugins directory.** The plan and NU-102 speak of
   `nucleus-<name>` "on PATH or in the plugins dir"; the only place the CLI
   and the runtime have ever looked is PATH. The listing and the allowlist
   cover PATH, and no directory was invented for them.
2. **A list of structures cannot say "declared empty".** The loader turns an
   absent `auth_federated` into an empty list, and `plugins.allowed` is the
   same shape, so "the allowlist is set" cannot mean "the key is present":
   until v2.0.0 an empty list restricts nothing, and refusing everything is
   `allow_external: false` — which, unlike a list, can also come from the
   environment (`NUCLEUS_PLUGINS__ALLOW_EXTERNAL=false`). A misspelt key
   inside an entry (`capabilites:`) is invisible to the strict key check for
   the same reason; it decodes as an entry with no capabilities, which the
   semantic check refuses.
3. **The dispatcher has no `--config` of its own.** An external command
   receives its arguments untouched, so `nucleus <name>` reads the
   allowlist from `nucleus.yml` in the working directory or the file
   `NUCLEUS_CONFIG` names. A configuration that does not load stops the
   command only when it declares a `plugins` block — otherwise an
   unrelated broken file would have started refusing commands that ran
   before.
4. **`nucleus plugin list` and the mail runtime executed every candidate to
   read its capabilities.** A capability probe is an execution; under an
   allowlist an unlisted binary is now listed refused and never run, not
   even with `capabilities`.
5. **The mail guide promised a health capability for plugins**
   (`mail.health`) that the contract never had: the sender that runs a
   plugin does not implement the health check. The sentence is corrected,
   and the retired-claims guard holds it, together with the sentences that
   said no example plugin ships.

## What N11 found that the plan did not say

1. **The outbox retried every failure the same way.** A bridge had no way
   to say "this message will never be accepted": a delivery the receiver
   refused was retried with backoff until `max_retries`, and only then went
   to the dead letter. The plugin contract has that answer — exits `10` and
   `30` are non-retriable — and nothing on the outbox side could hear it.
   `outbox.Permanent` is the way to say it: the dispatcher fails the message
   on the attempt that returned it (in a fan-out, only when every failure
   was permanent), and the plugin bridge returns it for those two exits.
   Every other error keeps the old semantics, so no existing bridge changes
   behaviour.
2. **A refusal during shutdown must not be permanent.** A plugin that is
   killed because the application is stopping exits with no code the
   contract knows, and `ExecuteRequest` marks it non-retriable; reading that
   as a refusal would have dead-lettered every message in flight at
   shutdown. The bridge treats only an answered `10` or `30` as permanent,
   and nothing while its context is cancelled.
3. **The probes' instrument changed in two places.** `EX-05` looked only
   for directories named example or sample, which the owner's decision of
   2026-09-12 (no `examples/`; examples are tested fixtures) made the wrong
   place to look; it now also reads `internal/fixtures/`, and leaves out a
   `package main` there (an external plugin is `EX-01`'s). `EX-06` accepted
   any passing `go test ./...` with `CheckModule` written somewhere in a
   test file; it now finds the test functions that call it and requires
   each to have run and passed. Verified by mutation: a bridge type the
   application no longer recognises drops `EX-03` and `EX-04` to absent; a
   queue key that is not the message id drops `EX-03` to partial, an
   unsigned webhook delivery `EX-04`; an in-process fixture whose test fails
   drops `EX-05` to partial; a policy row in the template about a route the
   module does not serve, or a template test that no longer calls
   `CheckModule`, drops `EX-06` to partial.
4. **The bench cannot see the retry rule.** The probes' plugin always
   accepts, so a dispatcher that dead-lettered every failure, or retried
   every one, measures the same. The unit tests of `pkg/outbox`, the
   bridge's tests in `pkg/app` and the relay fixture's own test (a 503 back
   to pending, a 410 to the dead letter) are what fail on that mutation.

## What N7 found that the plan did not say

1. **The interceptor seam carried the status and not the error.** The bench
   had recorded that the seam a reporter would register on existed. It did,
   and an interceptor saw every 500 — but the error behind it went to the
   log and nowhere else, so a reporter built on the seam alone sends "500 on
   GET /orders/{id}" and not why. The core gained one exported type,
   `interceptor.ErrorReporter`: a method on the writer an interceptor hands
   down, which the router calls with the error before it writes the 500
   (ADR-029, amended). It is a method and not a function to register so that
   the module builds against the release it pins, v1.31.0, which the
   standalone lane requires; measured there, with the workspace off, the
   module reports panics and sees no handler error — its handler-error tests
   fail with 0 events — and with this tree it reports both.
2. **`Mux.With` on the application's router runs the router's whole stack a
   second time** for the routes it registers: a middleware `Use`d on the
   router ran twice for one request, the request was logged twice, and the
   handler's `RouteFromContext` was empty, because the copy of the
   telemetry middleware starts a route holder the mux never fills. A group
   (`Group(func(g){ g.Use(...) })`) does not. Found while testing a route
   with its own `Timeout`, the case the documentation shows with `With`; it
   is outside this bench's controls and is left for a session of its own.
3. **The instrument grew in two places.** `CAT-01` boots the starter with
   the Sentry block and the module not added (9 refusals instead of 8): the
   configuration path tags `interceptors.sentry.*` `not installed: nucleus
   add sentry`, and `http_interceptors: [sentry]` alone is refused by the
   interceptor registry with the same command. `CAT-11` lists sentry-go
   among the dependencies the starter must not link. Verified by mutation:
   without the router's call to the reporters, or without `Build` keeping
   the reporter an interceptor hands down, `EN-09` drops to partial (the
   handler's error is answered 500 and no event arrives); a recipe without
   `http_interceptors: [sentry]` drops it to partial the same way (the
   module is linked and not in the request path); without the catalog entry
   the command refuses the name and `EN-09` is partial again ("code exists
   at providers/errors-sentry, and nucleus add does not install it");
   without the `interceptors` case in the not-installed tag, `CAT-01` drops
   to partial (8 of 9).

## What N6 found that the plan did not say

1. **go-redis is linked into every application, Redis or not.** A
   hello-world (`nucleus.New().FromConfigFile(...).Start()` and the SQLite
   driver) links `github.com/redis/go-redis/v9` through four importers:
   `pkg/auth` (the session store registry), `pkg/signals` (the relay),
   `pkg/health` (the `redis_url` probe) and `pkg/tasks/providers/asynq`. So
   the Redis backend adds no module to any application, and it went into
   the framework module rather than a sibling one — but into a package of
   its own, `pkg/cache/rediscache`, so it is not a fifth unconditional
   importer: the day the others leave the hello-world (A12 moves asynq out),
   the cache does not hold go-redis in. Measured with `go list -deps` and
   `go version -m` on a `-trimpath -ldflags='-s -w'` binary, against
   main at `fbf613a2`: hello + SQLite went from 463 packages, 57 linked
   modules and 29,059,362 bytes to 464, 57 and 29,093,570 (+1 package,
   `pkg/cache`, the framework's cache wiring; +34 KB). Importing the Redis
   backend adds one package, 16,656 bytes and no module. A sibling module
   would have cost the same packages plus one module, and a release-please
   package, a manifest entry and a standalone lane.
2. **There was no cache in the application at all.** `pkg/cache` was a
   library: nothing in the framework built one or handed one to a module,
   so "a Redis backend" alone would have been a constructor the bench could
   not reach from the starter. The wiring is new — `cache.*` in the schema,
   `App.Cache`, `nucleus.CacheFrom` as an optional interface beside the
   published `Runtime` (QADR-0010), a `/healthz` probe for a backend that
   has a server — and it is built on `WithoutDefaults()` too, so a selected
   backend cannot be ignored the way a storage block once was (NU-99).
3. **The interface has no prefix invalidation, no tags and no stampede
   guard,** and neither has the memory backend, so the Redis one has none
   either: `Get`, `Set` with a TTL the server keeps (`SET … PX`), `Delete`.
   Keys go under `cache.prefix` (`nucleus:cache:`), and that default must
   be quoted when written in YAML — `prefix: nucleus:cache:` does not parse.
4. **The firewall forbids go-redis in public signatures** ("redis client
   should be wrapped"), so the backend has `Open(ctx, url, Options)` and no
   constructor over a caller's client. The URL parser's errors quote the
   whole URL, password included; the backend keeps the reason and drops
   the URL.
5. **`nucleus add redis` now suggests `redis-cache`.** It is the start of
   exactly one name, and the catalog test that expected no suggestion for
   it was changed rather than adding `redis` as an alias: the session store
   and the job queue speak Redis too, and are not what the entry installs.

Verified by mutation, each against the bench: the backend not registering
on import, the framework ignoring `cache.provider`, the recipe selecting
the memory cache, the entry writing no import, the backend ignoring its
prefix, and `CacheFrom` finding nothing each drop `EN-06` to partial; the
refusal losing its catalog hint drops `CAT-01` to partial.

## What N5 found that the plan did not say

1. **The option the plan named cannot exist where it would read best.**
   `WithAccounts()` in `pkg/app` or on the builder would import
   `pkg/accounts`, which imports `pkg/nucleus` (its module is a
   `nucleus.ModuleSpec`), which imports `pkg/app`. The entry point is a
   module, `accounts.FromRuntime()`, mounted by the recipe; the realtime hub,
   which imports nothing of the framework, is an option (`WithRealtime()`).
2. **"No mailer" is two things, and the plan saw one.** The api starter is
   built `WithoutDefaults()` and has no mail sender at all — and with one
   (the new `WithMail()`), the default `mail_driver` is `noop`, which
   delivers nothing. A refusal for the first alone would have let the
   starter answer 202 while every confirmation link went nowhere. Both are
   refused at boot (`mail.Discards`), and `mail_driver: log`, development
   only, is what the recipe writes so the flow works at once.
3. **The default-deny layer never saw a session either.** NU-112 was about
   API keys, and the account flows had the same gap: a signed-in account was
   `anonymous` to the gate, so no policy could grant a route to it. Deciding
   the key's subject meant deciding what an identity is: one namespace, the
   id behind the request — a token's user id, a key's owner, the account a
   session signed in (`auth.SessionKeySubject`, which `StartSession` now
   writes). A key with no scopes reaches what its owner reaches; a scope is a
   subject of its own (`scope:<name>`), because the policy model's request is
   `sub, obj, act` and growing it would break every custom model.
4. **A module outside `pkg/nucleus` cannot read the application's
   configuration.** `FederatedSignIn` reads `public_base_url` through the
   framework's own runtime; `accounts.FromRuntime`, in `pkg/accounts`,
   cannot, so `base_url` and `from` are required in its own block
   (`modules.accounts`) instead of falling back to `public_base_url` and
   `mail_from`, and the development-only rule for `mail_driver: log` lives
   in the mail subsystem, which has the configuration.
5. **The api starter has no default-deny layer**, so on it every realtime
   topic is open to whoever reaches the route; on the default stack a
   channel is authorised by its path like any route. The recipe says so,
   and so does the guide.

## What N8 found that the plan did not say

1. **The state cookie never rode a SAML callback.** `FederatedSignIn` set it
   `SameSite=Lax`, which is right for OIDC — the callback is a top-level GET
   — and wrong for SAML: the identity provider returns the browser with a
   form POST from its own site, and browsers do not send a Lax cookie with
   a cross-site POST. By those rules a browser's SAML callback arrives
   without the cookie and is refused with "the sign-in was not started
   here", while a Go client, which ignores SameSite, signs in — read from the
   rules, not measured in a browser. A provider now declares a cross-site
   form-post callback (`federated.CrossSiteFormPostCallback`) and its
   instance gets `SameSite=None; Secure` over https. The bench drives the
   flow with a Go client, so it cannot see this; the unit test in
   `pkg/nucleus` reads the cookie's attributes.
2. **With `csrf_enabled` the POST callback answered 419.** The identity
   provider's form cannot carry the application's CSRF token, so on the mvc
   starter (which turns CSRF on) the callback was refused before the
   framework checked the state. `FederatedSignIn` now exempts each declared
   instance's callback path, and logs it at boot.
3. **The library leaves policy open that a service provider has to close.**
   crewjam/saml 0.5.1 accepts an assertion with no `AudienceRestriction`,
   accepts an unsigned assertion inside a signed Response, accepts an
   assertion with no `SubjectConfirmation` (so its `InResponseTo` and
   `Recipient` are never read), takes the first valid assertion of several,
   and dereferences `Conditions` and `Subject` without checking them (a
   signed assertion without them panics it). Its default service-provider
   metadata advertises the artifact binding, and an encryption key when a
   certificate is configured. The module closes each one and has a test
   that fails when it is reopened, verified by mutation; the
   encrypted-assertion refusal is the one its tests cannot isolate, because
   without a key the library fails to decrypt anyway.
4. **The library's test suite passes on the newer goxmldsig.** The module
   requires goxmldsig 1.6.1 and etree 1.8.1 where crewjam/saml 0.5.1 pins
   1.4.0 and 1.5.0. Run with those versions, crewjam's own suite fails only
   on two expected error messages of its signature-wrapping tests (the
   newer goxmldsig words the same rejection differently); the wrapped
   responses are still refused.
5. **The instrument grew in two places, and its boot got stricter.**
   `CAT-01` boots the starter with a SAML instance declared and the module
   not added, and the refusal names `nucleus add saml`; `CAT-11` lists
   crewjam/saml among the dependencies the starter must not link. Verified
   by mutation: without the metadata route `EN-02` drops to partial (404 on
   `/auth/corp/metadata`), and so does it with a recipe that does not mount
   `FederatedSignIn()`. The framework logged "server listening" just before
   it bound the port, so a probe could dial before anything accepted — CI
   hit it once on `EN-09` as connection refused; the bench's boot now waits
   for the line and a port that takes a connection. The framework side is
   fixed since NU-115 (the line follows the bind); the bench keeps the
   extra dial.

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
