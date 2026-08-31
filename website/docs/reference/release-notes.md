---
sidebar_position: 3
title: Release notes
description: What changed in each Nucleus release, in plain terms.
covers: []
config_keys: []
---

# Release notes

The current release is **v1.22.0**. {/* x-release-please-version */}

Nucleus is on the stable `v1.x` line (`v1.0.0` tagged 2026-07-10): stable
surfaces are frozen by contract tests, and every `v1.x` upgrade is designed
to be drop-in for code that uses them — see
[Support & compatibility](../architecture/compatibility.md) and the
[upgrade guide](../operations/upgrade.md). Commit-level detail for every
release, including the pre-1.0 history, lives on
[GitHub Releases](https://github.com/jcsvwinston/nucleus/releases).

## v1.21.0 (2026-08-30)

Debt found while closing the QCD audit arc, and one behaviour change.

### Worth reading before you upgrade

`app.WithOpenAuthz()` now decodes the bearer token. It always switched off
authorization; it also used to skip authentication, so an app in open mode
saw no identity — request interceptors and handlers alike got nothing from
`ClaimsFromContext` even when a valid token was on the wire. Open mode now
runs the JWT decode like any other app and only the enforcement step is
skipped. If you relied on open mode leaving the context claimless, this is a
change: handlers and interceptors will now see the caller's identity, and a
decoded `user_id` appears in logs where it did not before. There are no new
rejections — the decode never denies a request.

### Release machinery

Root-only release pull requests now tag on merge without hand intervention.
The cause was mechanical: a release PR that shipped only the root module
carried no component on its shared branch, and the tagger's standalone path
compared that empty component against the configured module path and gave
up, leaving the PR merged but untagged. Cutting the release PRs per component
puts the component back on the branch. The same round also fixed released
tags shipping with no build artifacts — tags created by the automation raise
no push event, so the asset build is now chained explicitly.

### Tooling

The example-pin guard tolerates one minor of lag with a loud warning instead
of failing outright, so a sibling repo cutting a tag no longer reddens every
open pull request at once; a companion writer and a workflow close the lag
mechanically. Two documentation snapshots that announced an earlier version
than their own were corrected, and the guards that watch for it were widened
to catch the prose form that slipped past. The documented way to mount
federated sign-in routes is now backed by an executable example, which
surfaced and corrected three mismatches in the prose.

## v1.20.1 (2026-08-30)

Release machinery only; no change to the framework's behaviour.

The LDAP provider module now requires this release line rather than one from
three releases back. The module depends on the root of its own repository, an
edge that can never be perfectly current — any release containing the
requirement is by definition later than it — so the suite manifest tolerates
exactly one release of lag there and calls anything beyond it staleness. It
had drifted to three while the framework moved through v1.18.0, v1.19.0 and
v1.20.0, which is enough to block certifying a set.

## v1.20.0 (2026-08-30)

The second half of the external coverage audit. v1.19.0 closed the six
findings it ranked highest; these are the remaining six, and two of them
turned out to be already fixed or overstated — which is the argument for
measuring before believing a report, including this one.

### Three behaviour changes worth reading before you upgrade

**A third-party request interceptor now sees who is calling.** The chain was
mounted outside the bearer decode, so with a valid token on the wire an
interceptor got nothing from `ClaimsFromContext` while the handler behind it
saw the same request authenticated. An interceptor that cannot tell who is
calling cannot audit, cannot scope a tenant and cannot rate-limit per
principal — most of what the seam is for. It now runs after the decode and
still before the default-deny layer, so it also observes requests the
enforcer is about to deny. Nothing moved relative to the request ID, the
real-IP resolution, the rate limiter or CSRF: those still run first and
still reject before an interceptor sees anything.

**`nucleus changepassword` refuses instead of lying.** With `auth_backends`
configured and the local backend not in it, the panel authenticates through
the chain and never reads the local password hash — and the command wrote
one anyway, printed "Password updated" and exited 0. The operator walked
away believing access was restored. Exiting 0 without producing the effect
the user believes they got is bad anywhere; on the command someone reaches
for when they are locked out it is the worst possible place. A chain that
*includes* the local backend still proceeds: there the hash is the
break-glass path, which the v1.19.0 chain change made more important, not
less.

**`X-Real-IP` is filtered the way `X-Forwarded-For` already was.** The
forwarded-for walk skips any hop that is itself a trusted proxy — it is
looking for the real client as seen by the outermost trusted hop. The
`X-Real-IP` fallback applied no such filter. Under a catch-all
`trusted_proxies` that made it a spoofing vector: every peer is trusted, the
walk skips every hop, and whatever the header said became the client. A
correctly configured deployment sees no change, because a real client is not
in `trusted_proxies`.

### And three corrections

`nucleus doctor --json` now exits non-zero when the report says
`unhealthy`. The verdict was returned from the text renderer only, so the
same report exited 1 as text and 0 as JSON — and the one mode that never
failed was exactly the one CI consumes.

A module declaring `Object ""` — "my root is public, my subtree is not" —
now actually serves its root. That spelling emitted a policy row for
`<prefix>` exactly, and the only way to reach a module root over HTTP is
`<prefix>/`, because the mux answers `<prefix>` with an unconditional
redirect. The row was dead on arrival and the intent was not expressible any
other way.

The startup log no longer says federated sign-in is **ready** when the
routes it prints answer 404. The framework builds the providers and owns the
flow — it issues the anti-forgery state, holds the pending sign-in and
refuses a callback that does not carry it back — but the application mounts
the two handlers, because what happens after a successful callback is the
application's decision. The log, the godoc and the authentication guide all
said otherwise; all three now agree with the code, and the guide carries the
example.

## v1.19.0 (2026-08-30)

An external coverage audit ran the extension surface that v1.13–v1.17 added
— pluggable authentication, session stores, storage, interceptors, federated
sign-in — and measured what it does rather than reading what it claims. This
release is what it found: one fail-open, one kill switch that deleted data,
and four seams that could not be reached by the documented route.

### A backend that rejects now ends the login attempt

**This one is a breaking change.** A chain of `auth_backends` treated a
rejection and an unreachable backend the same way: both moved to the next
backend. So an employee whose directory account had been revoked, but whose
local row still carried the old password, was let in — the directory's
certain no was worth exactly as much as a timeout.

The two look alike, because neither produced a user, but their first causes
are opposite. A rejection is the identity source's verdict on these
credentials. An unreachable backend proves nothing at all. Now only the
second falls through, which is the break-glass path the ordering exists for.

The consequence is worth stating plainly: **a chain is a fallback for
unavailability, not a way to federate several user populations.** Every
account must be acceptable to the first backend that recognises the request,
because anything behind a rejection is unreachable by design. If you were
relying on `[ldap, local]` to serve accounts that exist only in the local
table, see the [upgrade guide](../operations/upgrade.md).

What makes this a correction rather than a decision is that the framework
already published the corrected behaviour in four places — the README, the
`auth_backends` reference, the `backendtest` conformance kit, and orbit's own
documentation. Only the function's own doc comment described the fail-open,
and it was the one that was wrong.

### `storage.cleanup.enabled: false` no longer deletes your objects

The switch did not switch anything. `Cleaner` did not record the flag
anywhere, and the two branches that were supposed to act on it returned
identical values, so `Start` swept regardless — immediately on boot, before
the first tick. Anything under the cleanup prefix older than `max_age` was
gone on every start, with nothing logged to say a disabled cleaner had run.
If you use `_tmp/` as a working area for uploads in progress or staging,
this was silent data loss.

### The extension surface is reachable by the documented route

Three separate defects with one shape: what v1.13–v1.17 added so that
somebody else could extend the framework could not be used the way the
documentation describes.

- **A configured session store kept its capabilities.** Installing a store
  through configuration wrapped it in an adapter carrying only the three
  contract methods, and both the session library and `ActiveSessions`
  discover enumeration and the context-taking variants by type assertion.
  With `session_store: redis` or `sql`, the active-sessions view went blind,
  the documented `SCS().Iterate` escape hatch panicked, and every session
  read and write dropped the request context.
- **A third-party session store now boots.** Layer 3 validated
  `session_store` against a hand-written `{memory,sql,redis}`, so a store
  registered with `RegisterSessionStore` was built by the container and
  refused by the runner and by every CLI subcommand — the same file, two
  verdicts. It is validated against the registry now, and an unknown name
  still fails at boot, naming what is actually available.
- **The fluent builder reads the same subtrees the CLI does.**
  `interceptors.<name>.*` was not captured at all on that path, and
  `auth.<instance>.*` was missed for federated instances, so both were built
  from their `default:` tags instead of from the file. Silently: the
  interceptor mounted and the instance existed, just without the settings
  the operator wrote.

### Two smaller corrections

`auth.ErrUserNotFound` is an alias of `backend.ErrUserNotFound` again, as the
package documentation promised. They were two distinct values with identical
message text, so a leaf backend's error was unrecognisable to code comparing
the other — and nothing in a log could show it.

A second `App` in one process now logs a warning when it reuses an
already-registered backend name. The registry is per-process, so the second
App's `UserProvider` is dropped and the first one's user table answers in its
place. That is intentional, but it is stronger than "reuses" and it was
happening in complete silence.

## v1.18.0 (2026-08-29)

Deciding whether a write was rejected by a unique constraint no longer
depends on what language the database server speaks.

### `db.IsUniqueViolation`

A handler that writes a row needs to tell "that e-mail is already taken"
apart from "the database is broken": the first is a `409` the caller can
act on, the second is a `500`. The only portable signal `database/sql`
offers is the error value, and the obvious shortcut — looking for
`duplicate key` or `unique constraint` in the driver's message — is wrong
in a way that never shows up in development.

PostgreSQL, MySQL, Oracle and SQL Server all translate their messages when
the server is configured in another language. A PostgreSQL server started
with `lc_messages='es_ES.utf8'` answers a rejected insert with

```
llave duplicada viola restricción de unicidad «users_email_key»
```

and MySQL with `--lc-messages=fr_FR` answers

```
Duplicata du champ 'a' pour la clef 't.u'
```

No English substring appears in either. Every such check quietly returns
false, and the branch it guards becomes dead code on that deployment —
with no error and no warning to say so.

`db.IsUniqueViolation(err)` classifies on the code the driver reports,
through `errors.As`: SQLSTATE `23505` on PostgreSQL, `1062` on
MySQL/MariaDB, `2627`/`2601` on SQL Server, `ORA-00001` on Oracle, and the
`SQLITE_CONSTRAINT_UNIQUE` / `PRIMARYKEY` extended codes on SQLite. It is
unaffected by the server's locale and by wording changes between driver
releases, and because `errors.As` walks the unwrap chain, an error your own
code has wrapped still classifies.

PostgreSQL is matched through an assertion on the `SQLState() string`
method rather than on a concrete driver type, so it covers pgx — the driver
`pkg/db` registers — and lib/pq alike, without either import. The SQL
Server and Oracle branches live behind the `mssql` and `oracle` build tags
that register those drivers, so a default build links neither.

It deliberately does not report foreign-key, not-null or check violations.
Code acting on "unique" wants to point at one field, and widening the
predicate later would silently change what that branch catches.

## v1.17.2 (2026-08-29)

Release machinery only; no change to the framework's behaviour.

The LDAP provider module now requires the release before this one, rather
than one from two releases back. The module depends on the root of its own
repository, an edge that can never be perfectly current — any release that
contains the requirement is by definition later than it — and the suite's
manifest tolerates exactly one release of lag before calling it staleness.
v1.17.1 was a documentation-only release and left the requirement where it
was, which put it two behind and made the suite unable to certify a set.
Re-pinning it here restores that, and the release checklist now carries the
step explicitly, marked as applying to patch releases too: skipping it
leaves that release certifiable and the next one not.

## v1.17.1 (2026-08-29)

Documentation only; no change to the framework's behaviour.

Two defects in the published documentation site, both of which were only
visible from outside the repository.

Every archived snapshot announced a version that was not its own — the
documentation kept for v1.14.0 said the current release was v1.13.0, and so
on for five of them. The snapshot freezes the pages exactly as they stand
before a release is cut, and at that moment the version marker still names
the previous one. It said so on the page the site serves at its root, since
the newest archive is what a reader gets by default. Each snapshot now
states its own version, the release script sets it when cutting, and a check
fails the build if one ever drifts again.

The marker that carries the version was also being picked up as the page
description. It was invisible on the page itself, which is why it went
unnoticed for so long, and visible exactly where a first impression is
formed: in a search result, or in the preview shown when someone shares the
link.

## v1.17.0 (2026-08-29)

Two new seams for third-party code, and a defect in the one that already
existed.

**Federated sign-in.** A directory is a username and a password, and an
identity provider is not: the person leaves for it, authenticates there,
and the answer arrives at a different URL some time later. That does not
fit the authentication backend contract, so it has its own. An operator
declares identity providers in the configuration file, naming each one and
the protocol that implements it — two providers of the same protocol, a
corporate tenant and a partner one, is an ordinary thing to want, so the
name is yours and the protocol is a separate field. Settings live under
`auth.<name>.*`, the same place a credential backend reads them.

Nucleus keeps the anti-forgery state itself. A provider never sees it: the
framework issues it, holds the pending sign-in and refuses a callback that
does not carry it back, before the provider is consulted at all. That is
the part of a redirect flow that works perfectly well when it is missing,
right up until somebody attacks it, so the author of a provider is not
given the chance to leave it out. No provider ships in this release; the
seam does.

**Request interceptors.** Middleware could always be attached, but only by
whoever assembles the application. A package that an application merely
imports had no way in, so an interceptor could not be shipped — only
pasted into somebody's bootstrap. Interceptors now register by name like
every other extension, and the operator places them:

```yaml
http_interceptors: [audit, tenant-guard]
```

The list is ordered and the order is the behaviour — first is outermost.
A name nobody registered fails at boot, naming what is registered, because
a typo in a list of request interceptors must not resolve to one fewer
protection.

**Fixed: watching SQL used to stop other watchers.** The process-wide SQL
observer was a single slot, so installing one replaced whatever was there.
Nucleus installs its own at startup to feed the observability bus, which
means an application that watched SQL turned that bus off by doing it —
with no error and no log, visible only as a live view that stopped
updating. Observers now accumulate. The call is unchanged and existing
code keeps working; it just no longer displaces anyone.

**Also:** the LDAP provider module implements the contract from its leaf
package, which takes what it compiles from 235 third-party packages down
to 11, and runs the authentication conformance suite against itself.

## v1.16.1 (2026-08-29)

Release machinery only; no change to the framework's behaviour.

A provider module and the framework release it requires are now cut from
the same commit. They were not: the framework's release configuration
excluded the `providers/` tree, so a change to `providers/ldap` could never
produce a framework release alongside it — and the suite manifest, which
requires a module's tag to be an ancestor of the framework's, had no way to
certify a set once the two drifted apart. `providers/ldap` v0.1.2 requires
this release, and both were published from one commit.

## v1.16.0 (2026-08-28)

Writing an authentication backend no longer means importing the whole
framework, and no longer means reading the contract carefully enough to
infer its security properties.

### The contract a backend implements is its own package

`pkg/auth/backend` holds the interface, the identity it returns, the two
sentinel errors, the configuration a backend reads and the registry — and
nothing else. Implementing three methods used to mean importing
`pkg/auth`, which links session stores, JWT, Redis, Prometheus and
OpenTelemetry: none of them needed to answer "do these credentials belong
to a real user".

```go
import "github.com/jcsvwinston/nucleus/pkg/auth/backend"

func init() {
    backend.Register("acme", New)
}
```

The same move for the other two registries that had it: `pkg/storage/provider`
(a storage backend no longer inherits the AWS, Azure and Google Cloud SDKs
that the built-in implementations need) and `pkg/auth/sessionstore`. Mail
already was a leaf and did not change.

**Nothing breaks.** The names in `pkg/auth` and `pkg/storage` are now
aliases of the same types, so existing code compiles unchanged and
`errors.Is` still matches across the boundary.

### A conformance suite for your backend

The parts of the contract that are easy to get wrong are expensive to get
wrong, so they are now checkable instead of only described:

```go
backendtest.Run(t, backendtest.Suite{
    New:           func() (backend.Backend, error) { return New(cfg) },
    ValidUser:     "ana",
    ValidPassword: "correcta",
    UnknownUser:   "nadie",
})
```

It checks that a wrong password and an unknown user produce the *same*
rejection — a caller that can tell them apart can enumerate your users —
that an empty password is refused, and that a source your backend cannot
reach reports *unavailable* rather than a rejection, which is what keeps
the chain falling through to the local account someone needs on the
morning the directory is down.

It checks the contract, not the behaviour: passing does not mean your
backend talks to the right directory.

### Fixed

- **An empty password is refused even when your user provider would not.**
  The adapter that wraps an application's `UserProvider` used to delegate
  that decision. Pointed at a provider that returns a user without
  comparing — a legacy row with an empty stored hash, or a bug — it
  authenticated. The framework is in that path, so it now stops there.

## v1.15.1 (2026-08-28)

Packaging only; no change to the framework's behaviour.

The release machinery was holding `providers/ldap` at version `0.1.0`
permanently — a leftover pin from cutting its first release meant every
subsequent proposal came out as `0.1.0` again, so the module could never
publish a fix. It can now, and `providers/ldap` v0.1.1 is the first.

## v1.15.0 (2026-08-28)

Nucleus can now authenticate against an LDAP directory, and the seam that
makes that possible stopped being something you have to read the source to
use.

### LDAP authentication

The directory client ships with Nucleus, as its own module — an
application that does not authenticate against a directory should not
download an LDAP library:

```bash
go get github.com/jcsvwinston/nucleus/providers/ldap
```

```go
import _ "github.com/jcsvwinston/nucleus/providers/ldap"
```

```yaml
auth_backends: [ldap, local]

auth:
  ldap:
    url: "ldaps://dc.corp.local:636"
    base_dn: "ou=people,dc=corp,dc=local"
    bind_dn: "cn=svc-nucleus,ou=services,dc=corp,dc=local"
    bind_password: "${LDAP_BIND_PASSWORD}"
```

The order is the feature: the directory answers first, and the local
account still works on the morning the directory does not.

What the backend refuses to do is as much of the design as what it does.
An empty password is rejected before a connection is opened — a bind with
a name and no password is *unauthenticated* under the LDAP specification,
and a directory may answer it with success. A username is escaped before
it reaches the search filter. A search that matches several entries is a
rejection rather than a choice made by directory ordering. And only a
directory that actually says "wrong credentials" produces a rejection:
an unreachable host, a service account whose password was rotated, a
misconfigured base — all of those report the backend as *unavailable*, so
the chain falls through to the account that can still get you in.

### A backend brings its own configuration

`auth.<backend>.*` belongs to the backend named in `auth_backends`. The
framework checks only that the section belongs to a backend that exists;
the backend declares and validates its own settings.

Two situations now fail at startup instead of quietly doing nothing:

- **A key the backend does not declare.** A misspelled directory URL would
  otherwise sit unnoticed until the day the setting mattered.
- **A section for a backend the chain does not name.** `auth.ldap.*`
  without `ldap` in `auth_backends` is read by nobody, so the application
  boots clean and the login page never consults the directory you
  configured.

### Errors that tell you what to do

Naming a backend that Nucleus publishes but that nothing has imported no
longer answers "unknown backend". It answers with the two lines that fix
it — the `go get` and the import — because the name came from the
documentation and the missing piece is the import, not the spelling.

`nucleus doctor --check auth` reviews the chain from the configuration:
a backend declared with no settings of its own, and a chain whose every
entry depends on an outside system, which is the deployment where nobody
can log in while the directory is down, including whoever would fix it.

### Fixed

- **The same file no longer gets two verdicts.** A configuration using a
  storage provider that Nucleus does not ship loaded correctly when the
  application started and was rejected as malformed by `nucleus check`,
  `doctor` and `config print` — the command-line tools did not know about
  the provider's configuration section. Both paths now share one rule.

## v1.14.0 (2026-08-26)

Two releases ago the parts you are most likely to replace became
pluggable. This one makes them configurable, and connects the
authentication seam to something you can actually declare.

- **A provider brings its own configuration.** A storage backend that
  Nucleus does not ship could be selected by name and then had nowhere to
  read its endpoint from — its settings died as unknown keys before it ever
  ran. A registered provider now owns a configuration subtree:

  ```yaml
  storage:
    provider: ceph
    ceph:
      endpoint: http://ceph.internal
      pool: 32
  ```

  The framework validates that the section belongs to a provider you
  actually registered, and the provider validates its contents. A
  misspelled section still fails as an unknown key — the exemption is for
  registered names, not for the whole namespace — and a key your own struct
  does not declare fails too. Provider configuration is exactly the place a
  typo would otherwise sit unnoticed until the day the setting mattered.

- **The authentication chain is declared in configuration**, and your own
  user table takes its place in it:

  ```yaml
  auth_backends: [ldap, local]
  ```

  Implement `auth.UserProvider` — the interface that has described how to
  reach your users for as long as the framework has existed — register it
  with `WithUserProvider`, and it answers as `local`. Modules reach the
  assembled chain through `rt.AuthChain()`, so a module that owns a sign-in
  page authenticates through the order you declared instead of going
  straight to the user table. Declaring an order matters little if it only
  applies to the doors the framework happens to own.

  A backend named in that list that nobody registered fails at **boot**,
  naming what is registered. A typo in an authentication list should not
  wait until the first person tries to log in.

- **What an extension may rely on is now frozen.** `app.Extension` receives
  the whole application object, and the contract used to say an extension
  could *set* fields on it. That was a blank cheque: whatever an extension
  reached for became part of the API in practice while being covered by
  nothing anyone could promise across versions. No extension ever used it.
  An extension now reads framework services, mounts routes and registers
  middleware — and what it may read is frozen like the rest of the stable
  surface, so adding to it is a deliberate promise and removing from it is
  a break somebody has to see.

## v1.13.0 (2026-08-26)

The release where the parts of the framework you are most likely to need to
replace stop being fixed.

Until now, exactly one subsystem could be extended from outside: mail. Every
other one picked its backend from a closed list, so running Ceph instead of
S3, or authenticating against a corporate directory, meant forking the
framework. That is the thing that stops an ecosystem from existing, and it
is what this release changes.

- **Storage backends are registered by name.** A backend Nucleus has never
  heard of is selectable from configuration:

  ```go
  func init() {
      storage.RegisterProvider("ceph", New)
  }
  ```

  Everything the framework layers on top — the circuit breaker, tenant
  prefixing, the public-URL mapper — is applied around whatever your factory
  returns, so a provider never reimplements any of it. The four built-ins
  register through this same call, because a registry whose built-ins take a
  private shortcut is one that drifts.

  A side effect worth knowing: an unknown provider name now fails, naming
  the registered ones. It used to fall through to the local filesystem, so a
  typo wrote your uploads to disk and said nothing.

- **Session stores are registered by name.** Same shape, plus an optional
  shutdown hook for a store that holds a connection pool. The contract is a
  framework interface with standard-library types only, so writing a store
  does not mean depending on whatever session library Nucleus uses inside.

- **Authentication backends, as an ordered chain.** This is the one that
  makes a corporate directory possible without the framework shipping an
  LDAP client:

  ```go
  chain, err := auth.NewChain("ldap", "local")
  ```

  The order is the feature. A backend returns one of three answers — this
  user is authenticated, these credentials are certainly wrong, or *I could
  not reach my directory*. The third is why the chain exists: when the
  directory is down, the local account you keep for exactly that morning
  still works.

  The distinction survives to the end. If every backend rejected, you get
  "invalid credentials", because that is what happened. If any backend was
  unreachable, you get an error that says so — "wrong password" and "the
  directory is down" send you to very different places at three in the
  morning.

  If you write a backend, one rule matters more than the rest: reject an
  unknown user and a wrong password identically, and in the same time. A
  backend that answers faster for a user who does not exist has published
  your user list.

  The chain is built in Go for now. Declaring it from `nucleus.yml`, and
  giving each backend its own configuration subtree, is the next piece of
  work — this release is the seam, not the whole road.

## v1.12.1 (2026-08-25)

A patch release from an external audit of the shipped framework. Six
findings, every one of the class "it reports success and does not do what
it says".

- **A module can declare its own root**. Mounting a module was
  supposed to stop it answering a mute 403 until the operator hand-edited
  the policy file. It held for every path except the one a module is most
  likely to serve: its own. The shortest object you could declare was
  `"/"`, which resolved to `"<prefix>/"` — and the enforcer treats
  `/consola` and `/consola/` as different paths, neither implying the
  other. `Object: "/"` now means the root *and* the subtree; `Object: ""`
  means the root alone. CSRF exemptions had the mirror image, since their
  matcher is a raw prefix rather than a path match, and a trailing slash
  left a collection POST unexempted.

- **A module can no longer switch CSRF off for the whole application**
 . A module *without* a prefix exempting `"/"` — the natural
  way to say "my routes" when there is no prefix — disabled CSRF
  everywhere, its sibling modules included, with no operator veto and no
  line in the boot log. That declaration now fails startup, and every
  module's exemptions are logged with their resolved paths. Mounting a
  module means trusting its routes; it was never an agreement to let it
  unprotect everyone else.

- **`Delete` reaches the public bucket**. With a
  `public_bucket` configured, deleting a public object returned success and
  left the object in place — so a public object could not be deleted
  through the store's own API. The loop moved to the second bucket only on
  a not-found error, but removal is idempotent and never reports one.
  Retention, user-requested deletion and attachment cleanup were all
  affected.

- **A malformed `trusted_proxies` entry fails to load**. It
  used to be discarded in silence: with one entry and a typo the list came
  out empty, and forwarding headers were never read. It failed in the safe
  direction, which is exactly why nobody noticed — three separate tools
  reported the configuration as written.

- **`doctor --check security` judges the proxy ranges together**
 . It looked at one entry at a time, so a catch-all split in
  two passed clean while covering the same address space as the one it
  rejects. The message also named the wrong header: under a catch-all it
  is `X-Real-IP` that becomes attacker-controlled, not `X-Forwarded-For`.

- **A test can pin its own database from the builder**. The
  test kit told you to set `Databases` in the config, and the builder had
  no way to do it. `WithDatabases` adds one, and it beats the `NUCLEUS_*`
  environment layer — because a call written in code is not a file, and a
  test that pins its database should not have it swapped by whatever your
  shell exports. The kit now warns when it sees such a variable set.

- **`nucleus version` reports the version it was installed at**
 . A binary from `go install …@vX.Y.Z` answered `dev`, because
  only a release build stamps the version through linker flags. The binary
  knew all along — its build info carries the module version — the command
  simply never asked.

## v1.12.0 (2026-08-25)

A minor release about knowing where you stand: the security posture stops
being folklore, a configuration that will not boot says so at every entry
point, and the checks that guard the documentation stop crying wolf.

- **The default security posture is frozen, and measured.** A test boots a
  real application, sends it a real request, and records what comes back —
  every security header, the attributes of every cookie, what a
  cross-origin caller receives — for both a development and a production
  profile, then compares it byte for byte against a checked-in baseline.
  Nothing in that file is transcribed, so it cannot claim a protection the
  framework does not emit. The comparison is exact in both directions: a
  loosened default is a regression, a tightened one changes behavior for
  deployments that relied on the old posture, and both have to be
  deliberate.
- **`nucleus doctor --check security`.** A new subject for the settings
  that load fine, boot fine, and expose you anyway: a wildcard
  `cors_origins` (fatal when combined with credentials, which the Fetch
  standard forbids outright), a catch-all `trusted_proxies` range that
  hands `X-Forwarded-For` to the caller, a `jwt_secret` that is long enough
  to pass a length check and still guessable, `csrf_insecure_cookie` in
  production, and rate limiting left off. It does not repeat
  `health --deploy`.
- **Cookie name prefixes are judged when the file is read.** `__Host-` and
  `__Secure-` are enforced by the browser, which silently drops a cookie
  whose attributes contradict its name. Those rules lived only in the
  session builder, so a contradictory configuration loaded clean and killed
  the application at boot. They now run with the rest of the referential
  validation, where the file is judged.
- **`config print` tells you when what it prints will not boot.** It was
  the last CLI surface that read a configuration without validating it.
  It still renders an invalid file — that is what you reach for when
  something is wrong — and writes the loader's own rejection to stderr, so
  `--json` stays pipeable.
- **The documentation guards stop crying wolf.** Keys under `modules:`
  belong to the module's own config type, and the child of a user-keyed
  section (`databases: primary:`) is a name the operator invents; neither
  can ever appear in the framework's key registry. The page that teaches
  module configuration was permanently flagged for teaching it correctly.

## v1.11.0 (2026-08-24)

A minor release about the edges: a misconfiguration that used to pass
silently now fails loudly, the test kit can finally check what reached the
database, and shutdown stops abandoning work in flight.

- **One configuration file, one verdict.** Value and cross-field validation
  (`log_level: verbose`, `mail_driver: smtp` without a host) ran only when
  the application booted. Every `nucleus` command loaded the same file
  without those checks, so a config the app rejected sailed through the
  CLI. Both layers now run wherever configuration is loaded; the error
  names the key, the value and the accepted set.
- **`serve` and `testserver` say what they serve.** Both build an
  application from configuration alone — the modules compiled into your
  binary are not mounted, so their routes answer 404 there. They print
  that before starting, the way `routes` already did.
- **The test kit reaches the database.** `nucleustest` gains `DB()` and
  `Runtime()` (the same handle a module receives), `TempSQLite` for a
  database per test, and `MigrateDir` to apply your project's migrations
  through the real migrator — ledger and checksums included, so a second
  call is a no-op. A test can now assert that a `POST` actually persisted.
- **Credential shapes work for every storage secret.** The documented
  `{env_var: …}` form was unusable for `storage.s3.access_key_id`,
  `secret_access_key`, `session_token` and `gcs.credentials`: the loader
  only accepted a plain string, so the shape the guide prescribes was
  rejected at boot. Both forms now load, and a plain string keeps working.
- **The outbox finishes what it started.** Stopping used to cancel the
  dispatcher outright, abandoning a delivery mid-attempt and leaving its
  message claimed until the lease expired. Shutdown now lets the pass in
  flight finish, and only cancels when the caller's deadline (or five
  seconds) runs out — with a warning when it comes to that.
- **The documentation archive is alive again.** The site serves a snapshot
  per published minor so readers pinned to an older release get the
  matching documentation. That archive had frozen at 1.2.0; it resumes
  here, and a check now fails when a minor ships without its snapshot.

## v1.10.0 (2026-08-21)

A minor release: the vertical-slice module arc. A mounted module can now
carry everything its feature needs, and a generator emits that shape.

- **Modules declare their own policy rows and CSRF exemptions.**
  `Module.Policies` contributes RBAC rows (same shape as a
  `rbac_policy.csv` row, objects relative to the module `Prefix`) to the
  default-deny enforcer, and `Module.CSRFExempt` rides the same
  pre-startup window as the automatic webhook-prefix exemption. Rows join
  the live in-memory ruleset only — the host's policy file is never
  written, and a `deny` row in the host's CSV overrides any module
  `allow`. Malformed declarations fail boot with
  `ErrInvalidModulePolicy` naming the module and entry.
- **Embedded module migrations are applicable.** One deliberate call —
  `rt.ApplyModuleMigrations()`, typically in `OnStart` — applies
  `Module.Migrations` through the real pipeline: the module-scoped ledger
  (`<module>/<id>`) with checksum tracking, idempotent across restarts.
  Application boot still never mutates the schema on its own; the boot
  warning about declared-but-unapplied migrations now names the call and
  goes quiet once the module uses it. Under the hood:
  `db.NewModuleFSMigrator`, an `fs.FS`-backed migrator usable directly.
- **Embedded module templates.** `Module.Templates` registers a module's
  `.html` files under the `<module-name>/` namespace;
  `app.WithTemplatesFS(prefix, fsys)` is the general accumulating
  extension point behind it. On a name collision the host's
  `templates_dir` parses last and wins.
- **`nucleus generate module <name>`.** One self-contained package under
  `internal/<name>/` — model + storage for the configured dialect,
  controller, and a module carrying its policy rows, CSRF exemption,
  embedded migrations and page template. Mounting it is the whole
  integration: no `rbac_policy.csv` or `nucleus.yml` edits, no manual
  migrate step, pinned by an executable-scaffold test that asserts the
  policy file stays byte-for-byte untouched.

## v1.9.2 (2026-08-19)

A patch release that corrects documentation which promised more than the code
delivered. No runtime behavior changes.

- **`EnsureBucket`'s documented scope now matches what the code can do.** The
  godoc suggested it could provision "the" bucket after construction, but
  `NewS3Store` verifies the configured bucket(s) up front and refuses to
  construct when one is missing — so a store whose own bucket is absent can
  never exist to call `EnsureBucket` on. The contract now states the real
  scope: provisioning buckets *other than* the store's own (exports,
  per-tenant spaces, scratch areas); self-provisioning the configured bucket
  is exactly what `storage.s3.create_bucket_if_missing` is for, at
  construction time. The storage guide says the same, and a live MinIO
  contract test (`TestS3Live_EnsureBucketProvisionsAnotherBucket`) pins it.

## v1.9.1 (2026-08-18)

A patch release from the SSR arc's re-verification.

- **The outbox dispatcher starts after extensions attach.** The dispatcher's
  first pass is immediate, and `Extension.Attach` — which ran later — is
  the supported way to register bridges: a message already durable in the
  table could be leased with an empty route registry and fail with "no
  bridge route matched", consuming a retry and dirtying
  `attempts`/`last_error`. The dispatcher now starts only after every
  extension has attached; a pre-existing pending message delivers on
  attempt 1. Durability semantics are unchanged.
- **The template extension point is reachable from the documented
  builder.** `nucleus.New()` gains `WithTemplateFuncs`, `WithTemplates`
  and `WithOpenAuthz` (plus package-level re-exports) — v1.9.0's template
  options only existed on `app.New`, which the builder wraps. A parity
  test now enforces that every public application option has a builder
  counterpart, so this class of gap fails the suite instead of the next
  release.
- **API additions must regenerate the frozen baseline in the same
  change.** The baseline only failed on removals, so v1.9.0's new symbols
  shipped unlisted and external coverage denominators undercounted the
  public surface. Regenerated, and a new check fails on unlisted
  additions.

## v1.9.0 (2026-08-17)

A feature minor: the server-side render layer works the way the
documentation prescribes. Found by the external coverage demo's first real
MVC application (session login, CSRF forms, full CRUD).

- **Modules with a `Prefix` receive the template engine and the session
  manager.** The prefix sub-router was built from scratch and never
  inherited either — so the documented way of declaring a module answered
  every render with "template engine is not configured" and every session
  helper with "session manager is not configured". The three composition
  paths (`Group`, `With`, `Route`) now share a single derivation function,
  so a future Mux-level dependency cannot be forgotten by one of them.
  Auditing this also surfaced that nothing ever handed the session manager
  to the router tree at all: `app.New` now does, and the `Context` session
  helpers (`SessionPutString` & co.) work everywhere.
- **Template functions and prebuilt bases.** `app.WithTemplateFuncs`
  registers a `template.FuncMap` available to every template the startup
  loader parses, and `app.WithTemplates` injects a prebuilt
  `*template.Template` as the parse base — presentation logic (date
  formats, percentages, pagination URLs) belongs in templates, not
  precomputed in Go. Order: registered functions → recursive parse of
  `templates_dir` → the engine is wired into the router. See the routing
  guide.
- **`csrf_insecure_cookie`** (development-only, default `false`) disables
  the Secure attribute on the CSRF cookies, mirroring
  `session_cookie_secure: false` — without it the double-submit flow was
  unreachable for plain-HTTP non-browser clients such as Go's cookie jar
  against `http://127.0.0.1`.
- **An SSR conformance suite now runs in CI**: a module WITH a prefix,
  served over real HTTP, must render a loaded template by name, keep
  session state (write → read → destroy), enforce CSRF (419 without the
  token, 200 with it), serve module-mounted statics, and apply registered
  template functions — the five things any server-rendered application
  needs on day one, none of which had a test before.

## v1.8.2 (2026-08-17)

A patch release: three findings from the external coverage demo's first
server-rendered MVC application.

- **The scaffold renders with the framework's own engine.** `app.New` used
  to load templates with a flat glob while `nucleus startapp` scaffolds its
  template into a subdirectory — on a fresh project no template loaded and
  every render answered "template engine is not configured", with no
  startup warning. The loader now walks `templates_dir` recursively; each
  template registers under its path relative to that directory
  (`fieldservice/index.html`), root files keep their flat name
  (`base.html`), and `{{define}}` blocks keep their declared names — flat
  layouts keep resolving unchanged. Startup logs `templates loaded` with
  the count, a present-but-empty directory logs a WARN, and the
  render-without-engine error now says what to check. A new
  executable-scaffold test boots a scaffolded project in CI and demands the
  generated page renders over HTTP — the class of generator/runtime
  disagreement that produced this finding (and the SQLite-DDL one before
  it) now fails the suite instead of the first user.
- **Outbox: per-instance lease owner and a routing-policy knob.** Every
  process used to write lease rows as the same literal `nucleus-app`, so a
  co-tenant process could lease — and fail — messages another instance was
  able to deliver, untraceably. The default owner is now derived per
  instance (`nucleus-<hostname>-<pid>`); `outbox.lease_owner` sets a stable
  identity and `outbox.missing_route_policy` (`error`, the previous
  behavior, or `ignore`) controls what happens when a leased topic has no
  registered bridge. Startup logs both.
- **`SessionCache.Flush` no longer panics.** Flushing sliced every session
  key to a fixed length, panicking on unrelated keys of 8–13 bytes —
  ordinary session data. It now uses prefix checks and touches only
  cache-prefixed entries.

## v1.8.1 (2026-08-17)

A patch release closing the loop on S3 bucket provisioning.

- **`storage.s3.create_bucket_if_missing` is reachable from `nucleus.yml`.**
  The missing-bucket startup error recommends that exact key, but the
  strict configuration validator rejected it: the key existed in
  `pkg/storage` and not in the application config schema — a circular
  repro with no operator exit (the environment form did not work either).
  The key now loads from the file and from
  `NUCLEUS_STORAGE__S3__CREATE_BUCKET_IF_MISSING`, and reaches the storage
  constructor. Opt-in with default `false`: a missing bucket without the
  flag still fails startup loudly, exactly as before.
- **A parity test keeps the two config surfaces in sync.** The application
  schema mirrors `pkg/storage`'s own config structs; every storage key must
  now be mirrored or carry an explicit, reasoned exclusion — so a future
  divergence fails the suite instead of becoming another
  advertised-but-rejected key.

## v1.8.0 (2026-08-16)

The developer-experience minor: the scaffolding, testing and configuration
gaps measured by the 2026-08-16 developer-experience audit, on top of the
fixes already landed in earlier patches.

- **`generate resource` (and `startapp`) emit a mountable module wired to a
  real repository.** The scaffold now produces a `nucleus.Context`
  controller implementing the REST Resource sub-interfaces, a repository
  running real SQL against the framework-managed `*sql.DB` (statements
  rendered for the configured dialect — RETURNING on PostgreSQL, OUTPUT
  INSERTED on SQL Server, OUT bind on Oracle, LastInsertId elsewhere), and
  `internal/modules/<name>_module.go`: `nucleus.New().Mount(modules.<Name>Module())`
  is the whole integration. The `*router.Mux` handler that forced a
  hand-written adapter and the in-memory map repository are gone; `startapp`
  shares the same artifacts and its module also serves the scaffolded page.

- **In-process test kit — `pkg/nucleustest` (experimental).**
  `nucleus.RunContext(ctx, app)` gives the run loop a caller-owned
  lifetime (cancel = graceful shutdown), and the kit wraps it:
  `nucleustest.Start(t, builder)` boots the app on a free loopback port,
  waits for `/healthz`, stops via `t.Cleanup`, and `MintToken` issues
  bearer tokens against the app's own `jwt_secret`. E2E suites no longer
  need `go build` + `exec.Command` + hand-rolled polling.

- **`profile: dev` boots a realistic config with zero backing services.**
  A production `nucleus.yml` naming PostgreSQL (+ replica), two Redis
  endpoints, S3 and SMTP boots unchanged with `profile: dev` (or
  `NUCLEUS_PROFILE=dev`): in-memory sessions and jobs, local filesystem
  storage, the no-op mailer, and SQLite (an already-SQLite URL is kept;
  extra database aliases are dropped). Unknown profile values fail config
  load. Identical on `app.LoadConfig` and the fluent loader.

## v1.7.0 (2026-08-16)

A feature minor: the security composition the docs describe now works end
to end, and object storage can provision itself.

- **The global default-deny authorization layer sees JWT claims.** When
  JWT signing material is configured, a bearer is decoded ahead of the
  global enforcement and the request's subjects are tried in order — the
  token's user id, its role, then `anonymous`. Role-based CSV policies
  (`p, admin, /api/admin/*, read, allow`) finally work at the global
  layer without re-implementing RBAC per module. Strictly
  non-restrictive: requests that passed before still pass (the anonymous
  fallback preserves bootstrap grants for authenticated callers);
  requests that were wrongly denied now succeed.
- **S3 bucket bootstrap.** `storage.s3.create_bucket_if_missing: true`
  provisions the configured bucket(s) at startup; `S3Store.EnsureBucket`
  is the programmatic, idempotent form. **Behaviour change:** without the
  opt-in, a missing bucket now fails the constructor loudly (with an
  actionable message) instead of booting green and failing on the first
  upload.
- **`ServiceRegistration.Health` is wired into `/healthz`** as the check
  `service:<name>`; a failing service flips the endpoint to 503. New
  building blocks: `health.FuncProbe` and `app.RegisterHealthProbe` for
  application-owned checks.

## v1.6.2 (2026-08-16)

A patch release: `dumpdata`/`loaddata` round-trip on schemas with foreign
keys, plus documentation honesty fixes.

- **`loaddata` inserts in FK-dependency order.** The load plan is now
  ordered topologically by the foreign-key graph introspected from the
  target database, so a fixture produced by `dumpdata` (which lists tables
  alphabetically) restores in a single invocation instead of failing on the
  first child table. The caller's order — the file's, or an explicit
  `--tables` — is the stable tie-break: a valid explicit order passes
  through unchanged (it used to be silently re-sorted alphabetically), an
  FK-invalid one is repaired. Self-references are skipped and FK cycles
  fall back to the given order rather than failing the load; the dry-run
  plan shows the real order.
- Documentation: the health-probe comment no longer denies the mail probe
  the code performs; the storage guide's import path is current; the auth
  guide's policy examples use the CRUD action vocabulary the middleware
  actually enforces (`read`/`create`/`update`/`delete`), with the mapping
  spelled out; `ServiceRegistration.Health` now states loudly that it is
  accepted but not yet wired into `/healthz`.
- The showcase example re-pins the current sibling tags (nucleus v1.6.1
  at cut time, agent v0.5.7, server v0.9.2, quarkbridge v0.3.7,
  quarkdatasource v0.2.8, quark v1.4.1).

## v1.6.1 (2026-08-15)

A patch release: the resource scaffolder targets your real database.

- **`generate resource` / `startapp` emit migration DDL for the configured
  database** instead of unconditional SQLite. A project configured against
  PostgreSQL used to receive `"id" INTEGER PRIMARY KEY AUTOINCREMENT` and
  `DATETIME` columns, and `nucleus migrate` then failed with a syntax error
  on the very migration the CLI had produced. The scaffold now resolves the
  dialect from the project's config (the same config `nucleus migrate`
  reads, `NUCLEUS_*` overrides included), and both commands accept
  `--dialect` / `--config` / `--database` to override it. A fresh project
  with no config keeps the sqlite default.
- New helpers for the same decision in your own tooling:
  `db.SystemFromURL` (URL → SQL system, no connection) and
  `model.BuildMigrationScaffoldForSystem` (dialect-dispatched scaffold).
- The generated repository is an in-memory placeholder and now says so — a
  note in the generated file and in the command output replaces the
  previous silence about the migration's table going unused.
- Security: Go 1.26.6 (stdlib CVEs), `grpc` v1.82.1, `otel` v1.44.0; the
  `nucleus new` scaffold inherits the same directives, and the showcase
  example re-pins quark v1.4.1.

## v1.6.0 (2026-07-22)

Defense-in-depth hardening of the webhook surface, from the first directed
security review of the continuous-audit regime. No behaviour changes for
correctly-configured apps; the wire is unchanged.

### Added

- **Webhook registration rejects non-canonical mounts.** A module webhook
  registered with `path == "/"` (which would mount a catch-all subtree) or a
  module name containing `..`/`/` (which would shift the mount point) now
  fails boot instead of mounting something surprising. Canonical paths and
  names are unaffected.
- **Outbox payload-encoding header is informational by design.** The bridge
  signs the body only — byte-for-byte the module-webhook scheme, so one
  verifier covers both surfaces — and `X-Outbox-Payload-Encoding` is now
  documented as unsigned/informational. New consumer helper
  `outbox.CheckPayloadEncoding` decodes by the encoding a consumer expects
  and rejects a mismatch (`ErrPayloadEncodingMismatch`), rather than trusting
  the request header. The signed wire is unchanged from v1.5.0.

### Upgrade notes

Drop-in. If a module registered a webhook at `/` or with a slash in its
name, that was already broken (unreachable or mis-mounted) and now fails
loudly at boot — give it a real path. Outbox consumers should verify the
body-only signature with the module-webhook verifier and check the payload
encoding against their own config (see `CheckPayloadEncoding`), not the
request header.

## v1.5.0 (2026-07-22)

Signs and versions the outbox webhook contract, hardens module webhooks
(canonical paths, opt-in anti-replay), and fixes Oracle pagination and S3/GCS
not-found detection. Drop-in: the outbox wire is unchanged by default and the
new webhook behaviour is opt-in.

### Added

- **The outbox bridge webhook has a signed, versioned contract.** With
  `outbox.bridges.<n>.config.secret`, every delivery carries an HMAC-SHA256
  signature over the body in `X-Nucleus-Signature` (`sha256=<hex>`) — the same
  scheme module webhooks verify, so one verifier covers both. Every delivery
  also declares its payload shape in `X-Outbox-Payload-Encoding: json|base64`,
  so a consumer never guesses. The wire is **byte-for-byte the v1.4.0 default
  (base64)**; `payload_encoding: json` opts into embedding the payload as
  JSON. A body-level contract test compares the emitted webhook byte for byte
  per variant — the gap the symbol-only freeze cannot see. Without a secret,
  deliveries are unsigned and a boot WARN says so. See
  [Storage & background tasks](../features/storage-and-tasks.md).
- **Webhook anti-replay (opt-in).** `WebhookSpec.TimestampTolerance > 0`
  requires an `X-Nucleus-Timestamp` header inside the signed material
  (`SignWebhookBodyWithTimestamp`), rejecting stale or tampered timestamps.
  The default (tolerance 0) keeps the body-only scheme unchanged. The absence
  of anti-replay in the default scheme is now documented as a limit, with
  event-ID dedup as the recommended pattern.

### Fixed

- **Oracle pagination emits valid SQL.** `FindAll` and `FindByID` in
  `pkg/model` used `LIMIT` on Oracle (ORA-00933); they now use
  `OFFSET … FETCH NEXT … ROWS ONLY` / `FETCH FIRST 1 ROWS ONLY`, the twin of
  the earlier MSSQL fix. The admin-user CLI lookup is fixed the same way.
  Exercised against a real Oracle in CI.
- **Webhook paths must be canonical.** A module webhook registered with a
  non-canonical path (`..`, `.`, doubled or trailing slash) now fails boot
  instead of mounting an unreachable route.
- **S3/GCS not-found by SDK type, not error text.** `Get`/`Exists` of a
  missing key now map to `storage.ErrNotFound` against real endpoints
  (previously matched on the error string, which a real S3 endpoint does not
  produce). A real-MinIO CI lane covers it.
- **Security:** `golang.org/x/text` bumped to v0.39.0 (GO-2026-5970).

### Upgrade notes

Nothing to change. If a bridge was relying on the base64 payload wire, it is
unchanged; opt into `payload_encoding: json` when your consumer is ready.
Configure `outbox.bridges.<n>.config.secret` to start signing deliveries.

## v1.4.0 (2026-07-20)

Module jobs and webhooks are now executed, not just declared: the `Jobs`
and `Webhooks` closures a module registers run for real, backed by the
existing task runtime and the application router. Also fixes a stop-path
bug in the Asynq task provider and rejects, on request, primary keys
assigned by HTTP clients. Drop-in upgrade; the new surfaces are opt-in.

### Added

- **Module jobs run on a real scheduler.** `JobRegistry.Register(name,
  spec)` schedules background work declared by a module: `Every` for
  fixed intervals or `Cron` for 5-field cron expressions and descriptors
  (`@hourly`, `@every 90s`), validated at boot and identical on every
  provider; per-run `Timeout`; and `Singleton` to skip a tick while the
  previous run is still executing. The `jobs_provider` key selects the
  runtime — `memory` (default, in-process) or `asynq` (Redis-backed,
  durable, with `jobs_redis_url` and `jobs_concurrency`). A broken
  registration (duplicate name, invalid cron, missing handler) fails boot
  instead of silently never running. See
  [Module jobs and webhooks](../features/storage-and-tasks.md#module-jobs-and-webhooks).
- **Module webhooks mount real routes.** `WebhookRegistry.Register(path,
  spec)` mounts an inbound receiver at `<webhooks_prefix>/<module><path>`
  behind a method allow-list (405), a body cap (413, default 1 MiB) and —
  when `Secret` is set — constant-time HMAC-SHA256 verification of the
  `X-Nucleus-Signature` header, rejecting unsigned or mis-signed requests
  with 401 before your handler runs. `nucleus.SignWebhookBody` produces
  the signature for senders and tests. With `csrf_enabled: true` the
  webhook prefix is exempted automatically — webhooks authenticate by
  signature, not CSRF token. A webhook registered without a `Secret` is
  flagged at boot.
- **`RejectClientPK`.** A per-model opt-in that rejects entities arriving
  through `Create` with a client-assigned primary key
  (`model.ErrClientAssignedPK`), for apps that bind request bodies
  straight into models. The check runs before hooks, so server-side key
  assignment in `BeforeCreate` keeps working.

### Fixed

- **The Asynq task worker stops when you stop it.** `Manager.Run` waited
  on OS signals internally, so cancelling its context (or calling
  `Close`) shut the server down but never unblocked `Run` — an embedded
  worker could not be stopped through the API. `Run` now returns promptly
  on context cancellation and on `Close`.
- **Boot no longer warns about declared jobs and webhooks.** The
  "background execution is not yet wired" readiness warning is gone —
  both surfaces execute. The warning for embedded migrations stays:
  Nucleus is SQL-first and never auto-applies them.

### Upgrade notes

Nothing to change in existing apps. If a module already declared `Jobs`
or `Webhooks` closures (previously inert), they now execute on the next
boot: review those closures before upgrading, set `jobs_provider` if you
want durability over the in-process default, and note that invalid
registrations that were silently ignored before now fail startup — which
is the point.

## v1.3.3 (2026-07-19)

A correctness patch: client-assigned primary keys work through `Create`,
unsupported engines fail at startup instead of at runtime, and two more
surfaces emit valid T-SQL. Drop-in for most apps — read the upgrade notes
if you point the `sql` session store or the outbox at SQL Server or Oracle.

### Fixed

- **A pre-assigned primary key now travels in the `INSERT`.**
  Client-generated keys (UUIDs, natural keys) were silently dropped from
  the insert: SQLite stored a row with a `NULL` primary key without any
  error, and PostgreSQL/SQL Server failed with a `NOT NULL` violation.
  A non-zero key is now included in the statement, and the read-back /
  back-fill machinery is skipped, so the entity keeps exactly the key you
  set. A zero-value key keeps the previous behavior: the column stays out
  of the `INSERT` and the database generates the key. See
  [Models & database](../concepts/models-and-database.md#how-create-treats-the-primary-key)
  — including the security note on accepting keys from HTTP clients.
- **The SQL session store and the outbox refuse unsupported engines at
  startup.** Both subsystems speak SQLite, PostgreSQL and MySQL only, but
  an MSSQL or Oracle database URL used to be silently treated as SQLite —
  the failure surfaced later, mid-request, as invalid SQL. Construction
  now fails at startup with an error naming the supported engines.
- **`not null` is matched exactly in `db:` tags.** `db:"not null unique"`
  (a space where a `;` was intended) used to mark the field required and
  silently lose the `unique`; the malformed directive now falls through to
  the startup `WARN` introduced in v1.3.2 instead of half-applying.
- **By-id operations reject models without a primary key.** `FindByID`,
  `Update` and `Delete` on a model that declares no primary key return an
  explicit "model has no primary key" error (check with `errors.Is`)
  instead of guessing a phantom `id` column, and the default list ordering
  falls back to a real column of the model.
- **`nucleus createuser` and `nucleus changepassword` emit valid T-SQL.**
  Their admin-user lookups used a `LIMIT` clause SQL Server does not
  accept; on MSSQL they now use `SELECT TOP 1`.

### Upgrade notes

If your configuration points the `sql` session store or the outbox at an
MSSQL or Oracle database, the app now stops at startup with a clear error
instead of failing later with invalid SQL. That configuration never worked
— it silently ran SQLite-flavored SQL against the wrong engine — but a
deployment that "started fine" before the upgrade will now refuse to boot
until those subsystems point at a supported engine (SQLite, PostgreSQL,
MySQL).

## v1.3.2 (2026-07-19)

A correctness patch focused on the model layer's `db:` tags and on
`Create` across database engines. Drop-in.

### Fixed

- **Unknown `db:` tag directives now warn at startup.** A directive the
  parser does not recognize was — and still is — applied as nothing; the
  difference is that the app now logs one startup `WARN` per affected
  field, naming the unrecognized tokens and the supported syntax, instead
  of leaving you trusting a constraint that never existed. `db:"-"` now
  excludes a field from persistence.
- **`Create` only reads back the generated key when it actually can.**
  The `RETURNING` / `OUTPUT INSERTED` read-back is now emitted only for
  models that declare a real, integer primary-key field. Models with
  string/UUID keys or without a declared primary key previously got a
  read-back query that could fail (for example against tables with no
  `id` column); they now take the plain insert path, matching
  SQLite/MySQL behavior.
- **List pagination on SQL Server emits valid T-SQL.** Paginated list
  queries used a `LIMIT` clause SQL Server does not accept; they now use
  the `OFFSET … FETCH` form. The whole CRUD surface is exercised against a
  real SQL Server (and Oracle) in release validation.
- **The version pinned by `nucleus new` can no longer go stale.** The
  framework version written into generated `go.mod` files is maintained by
  the release tooling and cross-checked in CI on every build.

### Upgrade notes

Nothing to change. If your startup logs show new `WARN` lines about `db:`
tags, those tags were already being ignored — fix the tag syntax, don't
silence the log. See
[the FAQ](../faq.md#my-startup-log-warns-about-unrecognized-db-tag-directives)
for the supported directives.

## v1.3.1 (2026-07-15)

A one-fix patch. Upgrade if `Create` should hand you the generated primary
key on PostgreSQL or SQL Server.

### Fixed

- **`Create` backfills the generated primary key on PostgreSQL and SQL
  Server.** Those drivers do not implement `LastInsertId`, so the entity's
  ID field silently stayed at zero after a successful insert. `Create` now
  uses `RETURNING` (PostgreSQL) / `OUTPUT INSERTED` (SQL Server) to
  populate it. Oracle remains a declared gap — see
  [Support & compatibility](../architecture/compatibility.md#databases).

## v1.3.0 (2026-07-13)

A minor release that completes the v1.2.0 security hardening pass and
rounds out observability.

### New

- **Opt-in driver-level SQL instrumentation** (`sql_driver_instrumentation`).
  Off by default (zero hot-path cost); when enabled, direct
  `QueryContext`/`ExecContext` statements that bypass the model layer —
  session stores, outbox dispatch, migrations, raw SQL — also reach the
  observability live SQL feed, without double-recording CRUD statements.
- **The observability package and its hooks are now stable**, covered by
  the same compatibility promise as the rest of the framework.

### Security

- **CSRF protection as a config switch.** `csrf_enabled: true` mounts
  origin verification (`Sec-Fetch-Site`) with a double-submit token
  fallback; `csrf_exempt_paths` excludes Bearer-only subtrees. The `mvc`
  scaffold enables it by default.
- **`metrics_public: false`** takes `/metrics` out of the anonymous
  allow-list and puts it behind the default-deny RBAC enforcer.

### Upgrade notes

Both new switches default to the previous behavior (`csrf_enabled: false`,
`metrics_public: true`); nothing changes until you opt in.

## v1.2.0 (2026-07-12)

A security-hardening minor. **Existing deployments can notice these changes
at upgrade time** — read the notes below.

### Security

- **`jwt_secret` must be at least 32 bytes.** Any non-empty value used to
  be accepted; a shorter secret is now a boot error. Generate a proper one
  (`openssl rand -base64 32`) or move to `jwt_keys[]`.
- **Proxy headers are no longer trusted by default.** `X-Forwarded-For` /
  `X-Real-IP` are ignored unless the immediate peer is listed in the new
  `trusted_proxies` key; otherwise the TCP peer address is the client IP
  for rate limiting and logs.
- **HSTS is emitted only over TLS or when explicitly forced**
  (`env: production`) — plain-HTTP development runs are no longer pinned
  to HTTPS by a stray header.

### Upgrade notes

- Short `jwt_secret` values fail the boot — rotate the secret before
  upgrading.
- If Nucleus runs behind a load balancer, set `trusted_proxies` to its
  address ranges or rate limiting will see every request as coming from
  the balancer.

## v1.1.0 (2026-07-11)

### New

- **SQL events report rows affected.** The observability feed's SQL events
  carry the driver-reported `RowsAffected`. Additive; drop-in.

## v1.0.0 (2026-07-10)

The first stable release. The compatibility promise starts here: stable
surfaces are pinned by contract freeze tests and change only through the
documented deprecation policy.

### Breaking

- **Cross-origin requests are denied by default.** The implicit allow-all
  CORS default is gone: an empty `cors_origins` now emits no CORS headers
  at all. Deployments that relied on allow-all must opt in explicitly —
  a real origin allow-list, or `cors_origins: ["*"]` to keep the old
  behavior.

### Upgrade notes

If browsers suddenly report CORS errors after this upgrade, set
`cors_origins` to the exact origins your frontend uses. Everything else in
v1.0.0 is the certification of surfaces that already existed in v0.12.x.
