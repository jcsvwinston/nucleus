# ADR-027: The backend contract ships a conformance suite

Reference date: 2026-08-28.
Status: Accepted.
Related: [ADR-025](ADR-025-plugin-contract-leaf-package.md) (the leaf the
suite lives beside), [ADR-024](ADR-024-ldap-provider-module.md) (the first
backend, whose security properties this generalises), [ADR-023](ADR-023-provider-registries.md)
(the three-answer contract).

## Context

The authentication contract is three methods and two sentinel errors, and
every subtle part of it is stated in prose: that a rejection and an
unreachable source are DIFFERENT answers; that an unknown user and a wrong
password must be indistinguishable; that an empty password is not a
credential.

Prose is where those properties go to be misread. Building the first
backend needed each of them pinned by a test that fails when the guard is
removed — and the reason each one exists is not obvious from the interface.
A plugin author reading `Authenticate(ctx, user, pass) (*User, error)` has
no way to discover that returning the wrong error class locks an operator
out of their own system.

## Decision

**`pkg/auth/backend/backendtest` is a conformance suite an author points at
their own backend.** Four lines of setup replace careful reading:

```go
backendtest.Run(t, backendtest.Suite{
    New:           func() (backend.Backend, error) { return New(cfg) },
    ValidUser:     "ana",
    ValidPassword: "correcta",
    UnknownUser:   "nadie",
})
```

It asserts CONTRACT properties, not quality: passing means a backend
answers the way the chain expects. It cannot tell you the backend talks to
the right directory.

**The checks are pure functions returning errors, adapted to `*testing.T`
by `Run`.** That is not a style preference: it is what lets the suite be
tested against backends broken on purpose. **A conformance suite nobody has
watched fail is a suite nobody knows bites** — so its own tests feed each
check a backend broken in exactly the way that check exists to catch, and
require the complaint to come from the right check with a message that
names the defect. Failing for the wrong reason teaches the wrong lesson.

**One check needs the author's cooperation and may be skipped**:
`Unavailable` builds the same backend pointed at a source it cannot reach,
because only the author knows how to break their own connection. Skipping
it is allowed and **loud**: the skip message says that the single most
consequential property in the contract is going unchecked.

## Consequences

**Writing the suite found a defect in the framework's own backend.** The
adapter that wraps an application's `UserProvider` did not reject an empty
password — it delegated the decision. Pointed at a provider that returns a
user without comparing (a legacy row with an empty stored hash, or a bug),
it authenticated. The framework sits in that path, so it now stops there:
the guard costs nothing and holds whatever quality the application's
provider has. A test fabricates the unsafe provider and requires the
rejection.

That finding is the argument for the suite in one line: the framework's own
implementation passed the empty-password check only because the test's
provider happened to be careful.

**The framework runs the suite against itself.** A kit whose author does
not point it at their own implementation is a kit that has never had to be
right. `Suite.Unavailable` is deliberately absent there, and the skip
explains why: that backend wraps the application's OWN database, so a
failure is a bug in the application rather than the "the directory is down"
case — reporting it as unavailable would let the chain fall through to a
backend the operator did not intend.

**What it does NOT check**: timing. The contract asks that an unknown user
and a wrong password take similar time, and that property is real — the
LDAP backend binds against a decoy DN for exactly that reason. It is left
out because a timing assertion reliable enough to gate CI needs far more
samples than a conformance run can afford, and a flaky check on a security
property is worse than none: it gets skipped, and then nobody looks. The
suite pins the part that is deterministic — the errors are identical — and
the timing discipline stays in the contract's prose and in each backend's
tests.

**Not done here**: the same treatment for storage, mail and session-store
providers. Each has its own properties worth pinning and its own author
cooperation to define; doing them together would produce a diff whose parts
could not be judged separately.
