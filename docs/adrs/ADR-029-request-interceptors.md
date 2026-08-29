# ADR-029: A third party can intercept the request lifecycle

Reference date: 2026-08-29.
Status: Accepted.
Related: [ADR-023](ADR-023-provider-registries.md) (the registries, and the
sentence this ADR answers), [ADR-018](ADR-018-admin-observability-bus-migration.md) (the
bus the SQL observer feeds), [ADR-025](ADR-025-plugin-contract-leaf-package.md)
(the leaf-package shape this contract follows).

## Context

ADR-023 closed with a note rather than an omission: the framework "does not
give a third party a way to intercept the request lifecycle — observing SQL
and HTTP is possible today, intercepting is not, and that is the next piece
of work". This is that work. Two things were found on inspection, and only
one of them was the thing that was written down.

**Intercepting HTTP was possible — but not for a plugin.** `AppBuilder.Use`
takes middleware, and a mounted module can declare its own. Both require
the person ASSEMBLING the application to write the code, in the right
place, in the right order. Every other extension point in this framework
works the other way: a package registers itself in `init`, an application
imports it for the side effect, configuration names it. Storage providers,
session stores, authentication backends and federated identity providers
all follow that shape. The request path was the one registry-shaped hole
with no registry, so an interceptor could not be *distributed* — it had to
be pasted into somebody's bootstrap.

**Observing SQL was possible — and doing it broke the framework.** This was
not in the note; it was found while checking the note. `model.SetDefaultSQLObserver`
stored into a single slot:

```go
var defaultSQLObserver atomic.Pointer[SQLQueryObserver]

func SetDefaultSQLObserver(obs SQLQueryObserver) {
	defaultSQLObserver.Store(&obs)   // the second caller replaces the first
}
```

`pkg/app` installs one at boot — it is what feeds the observability bus,
and therefore Orbit's live SQL view (ADR-018). So a third party doing the
one thing ADR-023 says they may do turned that bus off by doing it. No
error, no log, no failing test: a live feed that simply stopped updating,
in someone else's deployment, attributable to nothing.

## Decision

### 1. `pkg/router/interceptor` is the contract, and the registry

`Interceptor` is `func(http.Handler) http.Handler` — the standard Go
middleware shape, so an existing middleware needs no adapter. `Register`
follows the `database/sql` pattern the rest of the framework uses, and
duplicate names are an error rather than a silent replacement: for
something in the request path, letting import order decide the winner
means a security control whose identity depends on an import block.

The package links 2 third-party packages and is registered in
`contracts.TestPluginContract_StaysLight` with the same ceiling as the
other four plugin contracts.

### 2. The operator declares the ORDER, because order is the behaviour

```yaml
http_interceptors: [audit, tenant-guard]
interceptors:
  audit:
    sink: stdout
  tenant-guard:
    header: X-Tenant
```

Authentication before rate limiting and rate limiting before
authentication are different systems. So an interceptor is not merely
enabled, it is *placed*: `http_interceptors` is an ordered list the way
`auth_backends` is, first is outermost, and the pairing with
`interceptors.<name>.*` mirrors `auth_backends` with `auth.<name>.*` — the
list orders, the subtree configures.

A name nobody registered fails at **boot**, naming what is registered. A
typo in a list of request interceptors must not resolve to "one fewer
protection, quietly". The same applies to a factory that errors, and to
one that returns neither an interceptor nor an error.

### 3. Interceptors mount INSIDE the framework's own middleware

They wrap after the request ID, the session and the observability hook, not
before. An interceptor that displaced those would break everything
downstream — including its own logging, which is usually the reason it
exists. Order among themselves is the operator's, because that is the part
only the operator knows; order relative to the framework is not on offer.

### 4. The SQL observer becomes a subscriber list

`SetDefaultSQLObserver` now SUBSCRIBES. The name, signature and
nil-clears-everything behaviour are unchanged, so no call site moves, and
every existing one means "I want to see SQL events" — which is what it now
does without taking that away from anyone else.

A subscriber that panics is contained and the ones after it still run. An
observer is a bystander, and a bystander does not get to decide whether the
query happened.

## What this ADR deliberately does NOT do

**SQL stays observable, not interceptable.** The obvious symmetry — let a
third party rewrite or abort a statement — is refused. An interceptor in
the request path can be reasoned about from the outside: it sees an
`http.Request` and returns a response. One in the query path sits between
the ORM and the database, where rewriting is indistinguishable from
corrupting, and where an audit would have to re-derive what SQL actually
ran from a chain of plugins. If a case for it appears it deserves its own
ADR and its own arc, not an extension of this one on grounds of symmetry.

**No interceptor ships here.** The seam is the deliverable, the same
decision and for the same reason as [ADR-028](ADR-028-federated-authentication-seam.md):
the LDAP arc showed that building a provider before its floor bakes the gap
in.

## Consequences

The four properties that matter are each pinned by a test verified by
MUTATION rather than by reading: declaration order is the request order, an
unregistered name fails naming the registered ones, two subscribers both
receive SQL events (restoring the single slot fails it), and a panicking
subscriber does not stop the others.

The registry adds a fourth namespace to `internal/providerns`, so the
exemption for `interceptors.<name>.*` follows the same rule as the others:
per registered name, never namespace-wide. A subtree under a name nobody
registered is still an unknown key, which keeps `interceptors.` from
becoming a place where typos pass unseen.
