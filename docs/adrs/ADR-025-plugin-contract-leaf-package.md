# ADR-025: The plugin contract lives in a leaf package

Reference date: 2026-08-28.
Status: Accepted.
Related: [ADR-023](ADR-023-provider-registries.md) (the registries this
splits), [ADR-024](ADR-024-ldap-provider-module.md) (whose follow-up section
recorded the measurement that started this), [ADR-015](ADR-015-authz-hardening.md)
(the dependency firewall).

## Context

ADR-024 shipped the first real provider and, in its follow-up, recorded a
number: `pkg/auth` — the package a backend author must import to implement
`Backend` — links **115 third-party packages**. Session stores, JWT, Redis,
Prometheus, OpenTelemetry, the gRPC gateway. None of them are needed to
answer "do these credentials belong to a real user".

In practice this costs a provider author nothing today, because a provider
is only ever used alongside the framework that already has all of it. What
it costs is *optionality*: the contract a third party implements is welded
to the framework's entire runtime, so it cannot be depended on
independently, cannot be implemented by something that is not already a
Nucleus application, and cannot be frozen without freezing everything it
drags behind it.

The timing is the argument. There is **one** provider module today. ADR-023
made the case for closing this class of window before launch — after it,
every contract change costs a major with a deprecation window — and the
same logic applies with more force here: with three providers, this
extraction costs three migrations instead of one.

## Decision

**The contract a third-party authentication backend implements lives in
`pkg/auth/backend`, a leaf package.** It holds `Backend`, `User`, `Config`
(with `Bind`), `Factory`, the two sentinel errors, and the registry itself
— because a backend that must call `auth.RegisterBackend` to exist would
still import the heavy package for the privilege.

It links **two packages from one module** (the configuration decoder),
guarded by `contracts.TestPluginContract_StaysLight`. That guard is the
substance of this ADR: a ceiling that nobody enforces is a number that
drifts. Raising it is a decision about what every plugin author pays, so it
belongs in a review rather than in whatever import made it necessary.

**Nothing breaks.** The names in `pkg/auth` become type ALIASES, not copies,
so `auth.User` and `backend.User` are the same type, `auth.ErrInvalidCredentials`
is the same error value, and `errors.Is` still matches across the boundary.
`auth.RegisterBackend` delegates to `backend.Register` — one registry, two
doors. `contracts.TestPluginContract_AliasesKeepSourceCompatibility` asserts
all of it, including the assignment that only compiles if the alias is an
alias.

**The package is frozen** (`stable`, `frozen: true`, `firewalled: true`).
That is the other half of Arco H: ADR-024 noted that the provider's surface
was covered by no baseline at all. The contract now is.

## Consequences

**The freeze baseline records a move, not a removal.** Ten entries —
`User`'s fields, `Backend`'s methods, `Config`'s field set and its `Bind`
method — now appear under `pkg/auth/backend` instead of `pkg/auth`, because
go/doc attributes members to the package that defines them. The *names*
users write (`auth.User`, `auth.Backend`, `auth.BackendConfig`) remain in
the baseline under `pkg/auth` as type entries, and every line of existing
code still compiles. This is why the baseline was regenerated deliberately
rather than a symbol being restored: nothing was taken away, it was
relocated, and the freeze test correctly refuses to let that pass
unnoticed.

**`providers/ldap` does NOT migrate in this change.** Its module requires
nucleus v1.15.0, which has no `pkg/auth/backend`; it can only import the
leaf once a release containing it exists. It is the same "the module and
the API it consumes shipped in the same train" constraint that ADR-024
recorded, and the migration is the first task after the next cut.

**What this does NOT do**: split the other registries. Storage, mail and
session-store providers have the same shape of problem and are not measured
here. Doing them is worth its own pass; doing them as a side effect of this
one would make a large diff whose parts could not be judged separately.
