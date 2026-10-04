// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package catalogbench

// controls is the bench: every capability the extension surface is measured
// on, with the verdict this repository RECORDS for it.
//
// The list is a policy choice, not a derivation of what happens to exist. It
// comes from what an application author needs to EXTEND an application —
// install an official piece without reading its README, write a plugin or a
// module of their own from an example that runs — taken from the products
// that already answer it: `cargo add` and `npm install` for a command that
// pins what it installs, Django's and Rails' generators and engines for an
// entry that wires more than an import, hashicorp/go-plugin for the plugin
// side of an executable contract, and `git help -a` / `kubectl plugin list`
// for external commands a person can find.
//
// The fifteen entries are the catalog the A11 arc names (owner decision of
// 2026-10-04: the four that do not exist — saml, redis-cache, stripe,
// sentry — will be built; the catalog is embedded in the CLI and pinned to
// the certified set's versions).
//
// A verdict is what the probe MEASURED on the day it was recorded. Moving one
// is a deliberate edit in the change that moves the code.
func controls() []control {
	return []control{
		// ---- catalog: the command and its table ----------------------------
		{id: "CAT-01", family: "catalog", title: "every \"not installed\" refusal names the `nucleus add` that installs what it names",
			want: partial, note: "measured by booting the starter with each entry selected and not added: s3, gcs, azure, ldap, otlp, " +
				"prometheus and oidc are refused naming the `nucleus add` that installs them; the postgres URL is not refused at " +
				"all — the SQLite driver module links every engine (NU-8, session N3).",
			probe: probeHintsNameTheFix},
		{id: "CAT-02", family: "catalog", title: "`nucleus add --help` lists every name the command accepts",
			want: present, probe: probeHelpListsEveryName},
		{id: "CAT-03", family: "catalog", title: "an entry installs the certified set's version of its module",
			want: present, probe: probeAddPinsTheSet},
		{id: "CAT-04", family: "catalog", title: "`nucleus new` fetches what it scaffolds at the set's versions",
			want: partial, note: "go.mod pins the framework and the scaffold fetches the driver (and every --with module of this " +
				"repository) at the version released with the CLI; quark and its driver, orbit and the bridges are fetched with no " +
				"version — and so is a suite product `nucleus add` fetches. Their versions are the umbrella's certified set " +
				"(versions.yaml), cut after this CLI is tagged (Nucleus tags first and Orbit requires the Nucleus it is cut " +
				"against), so no release of this repository can carry them: the pin needs the set to travel with the CLI from " +
				"the umbrella.",
			probe: probeNewPinsWhatItFetches},
		{id: "CAT-05", family: "catalog", title: "an entry can carry more than `go get` and a blank import (a Mount, a configuration block)",
			want: absent, note: "the only thing an entry is, is a module path: `nucleus add` writes `import _` and nothing else, and the " +
				"core entries that need wiring (accounts, apikeys, websockets, sql-queue) are not in the table at all.",
			probe: probeEntryWritesMoreThanAnImport},
		{id: "CAT-06", family: "catalog", title: "adding an entry that is already there changes nothing",
			want: present, probe: probeReAddIsNoOp},
		{id: "CAT-07", family: "catalog", title: "a mistyped name gets the nearest entry suggested",
			want: present, probe: probeUnknownNameSuggests},
		{id: "CAT-08", family: "catalog", title: "after `nucleus add`, the person is told the configuration the entry reads",
			want: present, probe: probeAddNamesTheConfiguration},
		{id: "CAT-09", family: "catalog", title: "the site's CLI reference lists every entry the command accepts",
			want: present, probe: probeSiteListsTheCatalog},
		{id: "CAT-10", family: "catalog", title: "one catalogue: what `nucleus add` installs and what `nucleus new --with` resolves",
			want: present, probe: probeOneCatalogue},
		{id: "CAT-11", family: "catalog", title: "an application links only the entries it added",
			want: partial, note: "the starter adds only the SQLite driver and its binary links pgx, go-sql-driver/mysql, go-mssqldb and " +
				"go-ora: every driver module imports internal/dbclassify, whose link.go blank-imports all five engines (NU-8, " +
				"measured here at the application). The storage, exporter and directory modules stay out until added.",
			probe: probeLinksOnlyWhatItAdded},

		// ---- entries: one per catalog entry --------------------------------
		{id: "EN-01", family: "entries", title: "oidc — `nucleus add oidc` wires federated sign-in on the starter",
			want: partial, note: "`nucleus add oidc` writes the blank import of pkg/auth/federated/oidc and names auth_federated, and " +
				"the starter with an auth_federated block builds the federated set — but the sign-in routes are still the " +
				"application's to mount (GET /auth/corp/start answers 404).",
			probe: probeEntryOIDC},
		{id: "EN-02", family: "entries", title: "saml — `nucleus add saml` installs a SAML identity provider",
			want: absent, note: "no SAML provider anywhere: no package under pkg/auth/federated, no module under providers/, no " +
				"dependency on crewjam/saml or gosaml2, nothing registers \"saml\" with the federated registry.",
			probe: probeEntrySAML},
		{id: "EN-03", family: "entries", title: "apikeys — `nucleus add apikeys` puts API-key authentication on the starter",
			want: partial, note: "works by hand — a SQL store, apikeys.Issue and apikeys.Middleware around a handler accept a valid key " +
				"and refuse a forged one — but `nucleus add apikeys` is an unknown name and nothing mounts the middleware for you.",
			probe: probeEntryAPIKeys},
		{id: "EN-04", family: "entries", title: "accounts — `nucleus add accounts` mounts the account flows on the starter",
			want: partial, note: "works by hand — POST /auth/register answers 202 — but `nucleus add accounts` is an unknown name, and " +
				"the hand wiring is more than a Mount line: accounts.Module takes a finished *Service, so the author opens a " +
				"*sql.DB of their own BEFORE the application is built and supplies a Mailer of their own (without one, " +
				"registration answers 500; the api starter has no mailer at all).",
			probe: probeEntryAccounts},
		{id: "EN-05", family: "entries", title: "sql-queue — `nucleus add sql-queue` gives the starter a durable job queue",
			want: partial, note: "works by configuration plus code — jobs_provider: sql and a module that registers a job; the queue " +
				"does not exist until one does — but `nucleus add sql-queue` is an unknown name.",
			probe: probeEntrySQLQueue},
		{id: "EN-06", family: "entries", title: "redis-cache — `nucleus add redis-cache` gives pkg/cache a Redis backend",
			want: absent, note: "pkg/cache has a memory and a SQL backend and no Redis one (its own docs say so), although go-redis is " +
				"already in the core graph for sessions, the asynq queue and the realtime relay.",
			probe: probeEntryRedisCache},
		{id: "EN-07", family: "entries", title: "websockets — `nucleus add websockets` serves a real-time channel on the starter",
			want: partial, note: "works by hand — a hub the application owns and a route that calls realtime.ServeWS complete the " +
				"handshake and deliver a broadcast — but `nucleus add websockets` is an unknown name.",
			probe: probeEntryWebSockets},
		{id: "EN-08", family: "entries", title: "stripe — `nucleus add stripe` installs a billing provider",
			want: absent, note: "no Stripe module, no stripe-go dependency, and the plugin SDK's subscription.create/cancel capabilities " +
				"are a \"stretch\" line in the reference with no schema in pkg/plugins.",
			probe: probeEntryStripe},
		{id: "EN-09", family: "entries", title: "sentry — `nucleus add sentry` reports the application's errors",
			want: absent, note: "no Sentry module and no sentry-go dependency. The seam such a module would register on exists — the " +
				"request-interceptor registry behind http_interceptors, which sees every request and its status — and nothing " +
				"uses it to report.",
			probe: probeEntrySentry},
		{id: "EN-10", family: "entries", title: "s3 — `nucleus add s3` gives the starter S3 storage",
			want: present, probe: probeEntryS3},
		{id: "EN-11", family: "entries", title: "gcs — `nucleus add gcs` gives the starter Google Cloud Storage",
			want: present, probe: probeEntryGCS},
		{id: "EN-12", family: "entries", title: "azure — `nucleus add azure` gives the starter Azure Blob storage",
			want: present, probe: probeEntryAzure},
		{id: "EN-13", family: "entries", title: "ldap — `nucleus add ldap` puts a directory in the starter's authentication chain",
			want: present, probe: probeEntryLDAP},
		{id: "EN-14", family: "entries", title: "otlp — `nucleus add otlp` exports the starter's telemetry over OTLP",
			want: present, probe: probeEntryOTLP},
		{id: "EN-15", family: "entries", title: "prometheus — `nucleus add prometheus` serves the starter's metrics",
			want: present, probe: probeEntryPrometheus},

		// ---- plugins: what an application can write ------------------------
		{id: "EX-01", family: "plugins", title: "an example external plugin builds in a test and passes `nucleus plugin test --execute`",
			want: absent, note: "no example plugin in the repository: no `package main` under a nucleus-plugin-*, example-plugin, " +
				"examples/plugins or testdata/plugins directory. A plugin author implements the envelope from the reference alone.",
			probe: probeExamplePlugin},
		{id: "EX-02", family: "plugins", title: "`mail.send` reaches an external plugin through the runtime",
			want: present, probe: probeMailBridge},
		{id: "EX-03", family: "plugins", title: "`queue.publish` has a runtime bridge to an external plugin",
			want: absent, note: "the payload schema exists and nothing sends it: the outbox's bridge types are webhook and a disabled " +
				"kafka, and a plugin-backed bridge (type plugin, external, exec or nucleus-plugin) boots with a WARN and is " +
				"dropped.",
			probe: probeQueuePublishBridge},
		{id: "EX-04", family: "plugins", title: "`webhook.deliver` has a runtime bridge to an external plugin",
			want: absent, note: "the payload schema exists and nothing sends it: outbound webhooks are the outbox's built-in HTTP bridge, " +
				"and no bridge type hands a delivery to a plugin.",
			probe: probeWebhookDeliverBridge},
		{id: "EX-05", family: "plugins", title: "an in-process example — a provider or a module — ships as a fixture tested in CI",
			want: absent, note: "no directory named example or sample holds a tested provider, module or extension. The first-party " +
				"modules under providers/ and exporters/ register the same way, but they are production code with their own " +
				"SDKs, not a starting point.",
			probe: probeInProcessExample},
		{id: "EX-06", family: "plugins", title: "a community module template builds standalone and its test calls nucleustest.CheckModule",
			want: absent, note: "the CLI writes applications (mvc, api, suite) and slices inside one (`generate module`); nothing writes " +
				"a standalone module with its own go.mod: --template module/plugin/extension and generate plugin/extension/" +
				"provider are all refused.",
			probe: probeCommunityTemplate},
		{id: "EX-07", family: "plugins", title: "`nucleus <name>` dispatches to a `nucleus-<name>` binary end to end",
			want: present, probe: probeExternalCommandDispatch},
		{id: "EX-08", family: "plugins", title: "the external commands on PATH are discoverable from the CLI",
			want: absent, note: "neither `nucleus --help`, `nucleus help` nor `nucleus plugin list` mentions a nucleus-<name> command " +
				"on PATH; a person finds one by knowing it is there.",
			probe: probeExternalCommandsListed},
		{id: "EX-09", family: "plugins", title: "the plugin reference points a plugin author at a runnable example",
			want: absent, note: "docs/reference/PLUGIN_SDK.md says it in so many words: no runnable example plugin ships in-tree, and no " +
				"release is promised for one.",
			probe: probePluginSDKPointsAtExample},
		{id: "EX-10", family: "plugins", title: "`nucleus plugin test --execute` exercises the envelope, not only discovery",
			want: partial, note: "--execute re-runs the capability listing and reports ok; it never sends a request envelope, so a " +
				"plugin that answers every request with garbage and exit code 50 passes it.",
			probe: probePluginTestExercisesEnvelope},
		{id: "EX-11", family: "plugins", title: "a plugin author has an SDK side: a helper that serves the envelope",
			want: absent, note: "pkg/plugins is the host side only (discover, probe, execute); a plugin author re-implements the request " +
				"and response envelopes, exit codes included, from the reference.",
			probe: probePluginAuthoringHelper},
		{id: "EX-12", family: "plugins", title: "an external plugin runs only when the configuration allows it",
			want: absent, note: "any nucleus-plugin-<driver> on PATH that advertises mail.send is executed for mail_driver: <driver>; " +
				"the reference's allowlist (plugins.allowed, allow_external) is marked \"proposed\" and the configuration does not " +
				"know the keys.",
			probe: probePluginAllowlist},
	}
}
