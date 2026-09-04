# ADR-023: Registros de proveedores — los subsistemas dejan de elegir backend con un switch cerrado

Reference date: 2026-08-26.
Status: Accepted (Arco A landed 2026-08-26 in v1.13.0; Arco B and the
authentication wiring landed 2026-08-26 in v1.14.0).
Related: [ADR-010](ADR-010-fluent-api-v2-pkg-nucleus.md) §115 (override-friendly
collections are maps keyed by name — this ADR is that principle applied to
providers), [ADR-015](ADR-015-firewall-vn-resolution-and-leak-dispositions.md) (the dependency firewall,
which shaped every signature here), [ADR-022](ADR-022-vertical-slice-modules.md)
(the module contract, whose two defects found by an external audit were the
evidence that started this work).

## Context

An audit of the framework's extension surface found the asymmetry plainly:
of the six subsystems that select an external service, exactly **one** —
mail — could be extended from outside. Storage, session stores, task
providers, authorization and authentication all chose their backend from a
closed `switch` over constants compiled into the framework.

The consequence is not subtle. An operator running Ceph instead of S3, or
authenticating against a corporate directory, had one path: fork the
framework and carry the patch forever. That is what prevents an ecosystem
from forming around a product, and it is the thing a framework must get
right before anyone depends on it — after launch, every contract change
costs a major with a deprecation window.

Two further facts shaped the decision:

- **The pattern already existed in the suite.** `mail.RegisterProvider`
  worked, and quark had been registering dialects, typed scanners and
  seeders for far longer. Nothing had to be invented; nucleus was the
  laggard.
- **`app.Extension` was a blank cheque.** Its contract said an extension
  "may set fields on App". Whatever an extension reached for became API in
  practice while being covered by nothing that could be promised across
  versions — and no extension ever used the permission. It cost stability
  and bought nothing.

## Decision

**1. Every subsystem that selects a backend does so through a registry.**
`RegisterX(name, factory)` plus `RegisteredX()`, with the built-ins
registering through the same public call. That last part is not symmetry
for its own sake: a registry whose built-ins take a private shortcut drifts,
because the path third parties use stops being the one that gets exercised
on every run.

Registering a name that is already taken is an **error**, never a silent
replacement. Two packages claiming `s3` would otherwise make the effective
backend depend on import order — a bug that only ever appears in someone
else's deployment.

**2. A registered provider owns a configuration subtree.** Being selectable
by name is one step short of useful if the provider has nowhere to read its
endpoint from. The framework validates that a section belongs to a
**registered** name; the provider validates its contents. The exemption is
per registered name and not for the namespace, so a misspelled section is
still an unknown key — otherwise the mechanism would become a place where
any typo passes unseen.

**3. Authentication is an ORDERED chain, and a backend has three answers.**
Accepted, certainly rejected, or *could not reach its source*. The third is
why the chain is a list and not the map the backends live in: `[ldap,
local]` means the directory answers first and the local account still works
the morning the directory does not. The distinction survives to the caller
— every backend rejecting yields invalid credentials, any backend being
unreachable yields an error that says so, because "wrong password" and "the
directory is down" send an operator to very different places. An unexpected
error counts as unavailable: a backend failing in a way nobody anticipated
must not be able to lock every user out.

**4. An extension reads; it does not reassign.** What it may read is frozen
in `contracts/baseline/extension_surface.txt`, alongside the exported
symbols, the CLI commands and the config keys. Adding a field is a
deliberate promise to plugin authors; removing one is a break somebody has
to see.

**5. Backends with heavy third-party dependencies live outside the core.**
The seam is in the framework; LDAP, SAML and OIDC are separate modules.
Whoever does not use them does not pay for them, and the dependency
firewall never has to be argued about.

## Consequences

Every public signature in these registries uses first-party or standard
library types. This is not decoration: the first version of the session
registry returned the session library's own interface, and the dependency
firewall caught it — every author of a third-party session store would have
inherited a dependency on a library that is our internal implementation
detail. The framework now defines its own interface and adapts internally.

The unknown-provider error names the registered ones, because that error is
the only place a plugin author discovers the registry exists.

A side effect worth recording: routing storage selection through the
registry revealed that an unrecognised provider name used to fall through to
the local filesystem. A typo wrote uploads to disk while reporting success —
the same "exit 0 without the effect" class an external audit had just found
five instances of elsewhere. It now fails, naming what is registered.

What this ADR does NOT do: turn rate limiting on, change the precedence of
the environment layer over files, or ship any federated backend. It also
does not give a third party a way to intercept the request lifecycle —
observing SQL and HTTP is possible today, intercepting is not, and that is
the next piece of work rather than an oversight.

## Amendment — 2026-08-27 (Arco D §1)

Two things were found while building the first backend that actually
consumes this ADR, and both are recorded here rather than in a successor
because neither reverses a decision: they finish one and repair its
implementation.

**Decision 2 had landed for storage only.** The authentication registry
shipped in the same line with a factory that took no arguments — a
directory backend had nowhere to read its URL, its base DN or its TLS
settings from, and `auth.ldap.url` died as an unknown key before the
backend ever ran. The gap was hard to see because the godoc on
`BackendFactory` already described the subtree it did not have; prose that
describes something which does not happen is the one defect class no guard
in this repository catches.

`BackendFactory` is now `func(cfg BackendConfig) (Backend, error)`, in line
with the other three registries, and `NewChainFrom(ChainConfig)` carries
each backend's subtree from configuration to the factory.

This is a source-breaking change to a stable, frozen package, taken
deliberately and without a deprecation window. The reasoning: the symbol
was one day old (v1.14.0), no consumer outside this repository could exist,
orbit consumes the `*auth.Chain` type rather than the constructor, and the
contract baseline records symbol names rather than signatures — so no guard
would have objected either way, which makes writing the reason down the
only thing that keeps it honest. A deprecated twin would have permanently
disfigured the surface the whole extensibility plan rests on. `NewChain`
keeps its signature as the convenience form.

**The exemption rule lived in two places, and only one had it.** The
builder path exempted a registered provider's subtree from the unknown-key
guard; `app.LoadConfig` — the path every CLI command takes — did not. A
deployment on a third-party storage backend therefore booted a healthy
server whose own `nucleus check`, `doctor` and `config print` all called
the running configuration malformed. That is the "same file, two verdicts"
class this framework has closed twice before, reintroduced by teaching one
validator a rule and not the other.

The rule now has exactly one implementation (`internal/providerns`), read
by both validators and by both capture paths, and the regression test asks
both about the same file and requires the same verdict.

Where this does NOT reach: mail providers and session stores take a typed
struct the framework owns, so there is no open-ended subtree to exempt for
them. A third-party provider in either registry that needs settings of its
own would need the same treatment, and that is a change of scope rather
than a bug in this one.
