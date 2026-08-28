# ADR-026: The storage contract lives in a leaf package; the other registries, measured

Reference date: 2026-08-28.
Status: Accepted.
Related: [ADR-025](ADR-025-plugin-contract-leaf-package.md) (the same move
for authentication, which declared this pass), [ADR-023](ADR-023-provider-registries.md)
(the registries), [ADR-015](ADR-015-authz-hardening.md) (the firewall).

## Context

ADR-025 extracted the authentication contract and deliberately stopped
there, recording that storage, mail and the session store "have the same
shape of problem and are not measured here". This is that measurement, and
it found that they are **not** the same problem:

| Package a provider must import | Third-party packages inherited |
|---|---|
| `pkg/storage` | **301** |
| `pkg/auth` (session store registry) | **115** |
| `pkg/mail` | **0** |

Storage is three times worse than the case that justified ADR-025. The
reason is visible in the file listing: `s3.go`, `gcs.go` and `azure.go` live
in the same package as the `Store` interface, so implementing that interface
for Ceph, for Swift, or for an internal object store means inheriting the
AWS, Azure and Google Cloud SDKs — every one of them dead weight for the
backend being written.

Mail is the surprise worth recording: the registry that ADR-023 identified
as *the only one that was already extensible* turns out to be the only one
that was already clean. Its contract imports nothing but the standard
library. **No change is needed there, and that is a finding rather than an
omission** — it is the shape the other registries are being moved towards.

## Decision

**`pkg/storage/provider` holds the contract**: the `Store` interface and its
value types, the configuration a factory receives, and the registry. It
links two packages from one module, the same floor as the authentication
contract, and the same guard now covers both.

**The built-in providers register from OUTSIDE the package that owns the
registry.** ADR-023 established that built-ins must register "through the
same door as anyone else", because a registry whose built-ins take a private
shortcut drifts. This is the strongest available version of that rule: with
the registry in the leaf and the S3/GCS/Azure implementations above it, the
path a third party takes is literally the only path there is.

**Aliases, again, so nothing breaks.** `storage.Store` and `provider.Store`
are the same interface, `storage.Config` the same struct;
`storage.RegisterProvider` delegates. Everything that compiled still
compiles.

## Consequences

**The extension surface baseline changes one line**: `App.Storage` is
recorded as `provider.Store` rather than `storage.Store`. The field, the
type and what an extension can do with it are unchanged — only the package
that *defines* the name moved, and the baseline prints the defining package.
It is a re-spelling that the freeze correctly refuses to let pass silently.

**Seventy-five symbols relocate** in the API baseline, and every name a user
writes — `storage.Store`, `storage.Config`, `storage.S3Config`,
`storage.RegisterProvider` — remains under `pkg/storage`. Verified, not
assumed.

**The session store is NOT done here.** Its registry still lives in
`pkg/auth` and still costs 115. It is a smaller, separate move and gets its
own change, for the same reason this one was separated from ADR-025: a diff
whose parts cannot be judged separately is a diff nobody judges.

**What none of this changes**: the providers themselves still need a release
of the framework that contains the leaf before they can import it. The
migration of `providers/ldap` — and, when it exists, of any storage provider
— follows the next cut.
