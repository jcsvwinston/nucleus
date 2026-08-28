# ADR-024: The LDAP backend is a separate module inside this repository

Reference date: 2026-08-27.
Status: Accepted.
Related: [ADR-023](ADR-023-provider-registries.md) (the registries this
consumes, and decision 5, which said backends with heavy third-party
dependencies live outside the core), [ADR-015](ADR-015-authz-hardening.md)
(the dependency firewall), [ADR-017](ADR-017-admin-login-timing-equalization.md)
(the timing discipline the login path already follows).

## Context

ADR-023 built the seam and left the first real backend unbuilt. LDAP is the
one everybody asks for first: it is what "authenticate against the
corporate directory" means for most of the organisations that would deploy
this framework.

Decision 5 of ADR-023 said such backends live "outside the core" as
separate modules. That left the interesting question unanswered: separate
module in *which repository*, and how integrated does the experience get to
be? The two readings differ a lot in practice — one produces a plugin
somebody has to find, the other produces a battery that happens to be
packaged separately.

Two facts were measured rather than assumed before deciding.

**The dependency argument does not hold for LDAP specifically.** A minimal
bind-and-search program links five non-standard-library packages, and three
of them are new modules here: `go-ldap/ldap/v3`, `go-asn1-ber/asn1-ber` and
`Azure/go-ntlmssp` (all MIT). `google/uuid` and `golang.org/x/crypto` are
already in the framework's tree. Against a `go.mod` that already carries
the AWS, Azure and Google Cloud SDKs, five packages do not move the needle.

**It does hold for what comes next.** The same measurement for SAML plus
OIDC gives 32 modules in the graph and 18 third-party packages linked. The
seam has to stay whatever we decide about LDAP, or the Arco E backend
forces the argument again with a much worse hand.

## Decision

**The backend is a separate Go module, in this repository:
`github.com/jcsvwinston/nucleus/providers/ldap`.**

That reading of ADR-023 decision 5 is the one that maximises integration
without spending the dependency: it is a separate module for the compiler
and for `go.mod`, and one product for everything else — the same release
train, the same CI, the same documentation, the same repository. The
framework does not carry an LDAP client; the person who runs `go get` on
the provider is not sent to a third-party project of uncertain lifetime.

What was rejected, and why:

- **In the framework's `go.mod`.** Would have been the least friction for
  an operator and would have set the precedent that makes Arco E expensive.
  It also needs no seam, and the seam is the thing worth keeping.
- **Behind a build tag.** Does not work: a build tag removes the package
  from the binary, not the requirement from `go.mod`. Everyone would still
  download the client.
- **Written against the standard library.** `encoding/asn1` is DER; LDAP is
  BER. It means owning security-critical protocol code to avoid three MIT
  dependencies.
- **The out-of-process plugin model** this framework already uses for mail
  providers. A password would cross a process boundary on every login.

## Consequences

**The security properties are in the code and each one has a test that
fails without it.** Rather than restate them here, the short list: an empty
password is rejected before a connection is opened (RFC 4513 §5.1.2 makes
that bind *unauthenticated*, and a directory may answer it with success);
the username is filter-escaped before substitution; an ambiguous search is
a rejection rather than a coin toss; an absent user still costs a bind, so
this code adds no timing difference an attacker could measure from the
login form; and exactly one LDAP result code — 49 — is treated as a
rejection, everything else being unavailable so that a directory-side
problem cannot lock out the local account behind this backend.

That last one is worth stating as a rule and not as an implementation
detail: **erring towards unavailable costs a fallthrough, erring towards
rejection costs the outage.**

**The tests are in two layers, following what this repository already does
for S3 and Redis.** An injected connection drives every branch; a real
OpenLDAP proves the protocol. The live lane is required, because a backend
that authenticates people is the wrong place to save on a container.
Each security property was also verified by mutation — removing the
guard makes its test fail — which is the only thing that distinguishes a
test that asserts a property from a test that merely exercises the path.

**The provider's surface is NOT in the framework's contract baseline.** The
freeze test walks the root module, and this is a different one. Freezing
plugin contracts is Arco H of the extensibility plan; until then the
provider's API is covered by nothing, which is stated here rather than
assumed to be obvious.

**The repository becomes multi-module.** Release-please gains a second
package with a component tag (`providers/ldap/vX.Y.Z`, the form Go requires
for a nested module) and the first cut is pinned to `0.1.0`: the default
1.0.0 would claim a maturity a day-old backend does not have. Deliberately
NOT copied from orbit's configuration: `separate-pull-requests`. That flag
is what makes orbit's release PRs conflict with each other in the shared
manifest and cost extra rounds every train; grouped release PRs have no
such cascade.

**The provider pins a pseudo-version of the framework until the next tag.**
It requires the API added in the same arc, which no release carries yet.
The pin becomes a real version at the next cut, in the same step the
showcase example is re-pinned.

## Follow-up found while wiring the integrated experience (2026-08-27)

The claim "whoever does not use it does not pay for it" is true in the
direction that matters for the framework's users: nucleus does not carry an
LDAP client. It is worth writing down that the reverse direction is not
symmetric.

A backend has to implement `auth.Backend`, so it imports `pkg/auth` — and
`pkg/auth` alone links **117 third-party packages** (session stores, Redis,
JWT, OpenTelemetry, Prometheus). Measured, not estimated. In practice this
costs a provider's author nothing, because a provider is only ever used
alongside the framework that already has all of it. It does mean the
plugin-facing contract is currently entangled with the framework's whole
runtime tree, which is a real input for **Arco H** (freezing plugin
contracts): the interfaces a third party must implement would be better off
in a package that does not drag sessions, JWT and observability behind
them.

Not acted on here. Splitting `pkg/auth` is a stable-surface change with its
own deprecation cost, and doing it as a side effect of shipping the first
backend would be the wrong order.
