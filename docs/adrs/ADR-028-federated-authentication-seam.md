# ADR-028: Federated sign-in is a second contract, not a wider first one

Reference date: 2026-08-29.
Status: Accepted.
Related: [ADR-023](ADR-023-provider-registries.md) (the provider registries
and the three-answer contract), [ADR-025](ADR-025-plugin-contract-leaf-package.md)
(the leaf the contract lives in), [ADR-027](ADR-027-backend-conformance-suite.md)
(the conformance suite the credential contract ships).

## Context

The extensibility plan listed SAML and OIDC as the arc after LDAP, and the
authentication seam built in Arco A carries a comment saying it is the one
that "unblocks LDAP, SAML and OIDC as external modules". That claim is true
for LDAP and false for the other two, and nothing had noticed because
nobody had tried to write one.

`backend.Backend` is `Authenticate(ctx, username, password) (*User, error)`.
LDAP fits: a bind is a username and a password against a directory. A
federated flow has no credentials to hand over at all. The application
sends the browser to an identity provider, the identity provider answers to
a DIFFERENT URL some time later, and only then is there an identity. There
are two moments, a redirect, a callback route, and state that has to
survive between them.

Three shapes were considered.

**Widen `Backend`.** Add `BeginRedirect`/`CompleteRedirect` as optional
methods, or a capability interface a chain type-asserts for. Rejected: it
makes `Authenticate` a method that federated providers implement by
returning an error, and it puts a stateful handshake behind a signature
that promises there is none. It also drags every credential backend author
past machinery they will never use.

**Let the provider own its own routes.** A provider registers HTTP handlers
and does the whole flow itself. Rejected for the reason that decides this
ADR — see below.

**A second contract in its own leaf package.** Chosen.

## Decision

**`pkg/auth/federated` is the contract a browser-redirect identity provider
implements, and `auth.FederatedSet` is the framework's custody of the flows
in progress.**

The contract is two calls: `Begin` says where to send the browser, and
`Complete` validates what came back. It links 2 third-party packages, the
same ceiling `contracts.TestPluginContract_StaysLight` holds the other
three plugin contracts to, and it is registered in that test.

**The framework keeps the anti-forgery state, and the provider never sees
it.** This is the reason the seam exists rather than a bare interface. The
`state` parameter of a redirect flow is the part that is easy to leave out
and impossible to notice missing: a flow without it works perfectly well
until somebody attacks it. `FederatedSet` issues the token, holds the
pending flow, and refuses a callback that does not carry it back — before
the provider is called at all. An author cannot forget it because they are
never handed the choice. That is also what rules out "the provider owns its
routes": it would have made the CSRF check a thing each third party
reimplements, which is the same reasoning ADR-023 used to keep the
three-answer contract in the framework.

Four properties follow, each pinned by a test verified by MUTATION rather
than by reading:

- a callback whose state this set did not issue is refused, and the
  provider is not consulted;
- a state token is single use, so replaying a callback signs nobody in
  twice;
- a token issued for one instance cannot complete a flow at another —
  otherwise starting a sign-in at a weak identity provider and finishing it
  at a strong one would be a path;
- an abandoned flow expires and is consumed, so a stale token cannot be
  probed.

The provider is given a `Nonce` for its own protocol (an OIDC id_token
nonce, a SAML RelayState) and it is deliberately NOT the state token: a
provider that echoes its nonce would otherwise publish the framework's
CSRF value.

**The identity type is `backend.User`, aliased, not a parallel type.** An
application that already knows how to receive an authenticated user does
not learn a second shape.

**The error vocabulary mirrors the credential contract.**
`ErrIdentityRejected` and `ErrProviderUnavailable` exist for the same
reason `ErrInvalidCredentials` and `ErrBackendUnavailable` do: reporting an
outage as a rejection is what locks an operator out of their own system on
the morning they most need in. A provider that returns no error and no user
is treated as unavailable, because that is a bug in the provider rather
than a decision about the person signing in.

## Configuration: instance and type are different names

An operator declares identity providers in the file:

```yaml
public_base_url: https://app.example.com
auth_federated:
  - name: corp
    provider: oidc
    display_name: Corp SSO
  - name: partners
    provider: oidc
auth:
  corp:
    issuer: https://login.corp.example/
    client_id: nucleus
  partners:
    issuer: https://idp.partners.example/
```

`name` is the INSTANCE — the URL segment, the configuration subtree, the
sign-in button — and `provider` is the registered TYPE. They are separate
because **two identity providers of the same protocol is the ordinary
case**: a corporate tenant and a partner one, a staging IdP and a
production one. The credential-backend registry keys the subtree by the
registered name, which works when the name is "ldap" and stops working the
moment somebody needs two. A registry keyed by type alone would have made
the second tenant impossible to express without publishing a second module.

Settings travel through `auth.<instance>.*`, the SAME channel a credential
backend reads, so a provider binds them with the same `cfg.Bind`.

**`public_base_url` is required when the list is non-empty**, and it is the
address the BROWSER uses rather than the one the process binds — behind a
reverse proxy or in a container those differ, and a wrong callback is a
sign-in that fails only in production. The framework derives the callback
URL from it in exactly one place and logs it at startup, because it is the
one value an operator has to copy into their identity provider.

### The exemption rule, and why it moved

A federated instance's subtree is legitimate because the operator DECLARED
it, not because anything registered that name. That makes it the first
exemption in `internal/providerns` that depends on the configuration rather
than on a registry, so `Declared` is now passed to the rule and both
validators build it the same way from the same file.

This matters more than it looks. "The same file, two verdicts" is a defect
this framework has now found three times — a server booting happily on a
configuration its own `check` called malformed. A rule that reads the
declaration would have been the fourth shape of it, because the two
validators run at different moments and only one of them naturally has the
declaration in hand. There is a test that asserts both paths accept the
same file, and a test that the exemption is per declared instance and never
namespace-wide: a subtree nobody declared is still an unknown key, or
`auth.` would become the one place in the configuration where a typo passes
unseen.

## Consequences

**No provider ships in this arc.** The seam is the deliverable, in the same
order the LDAP arc turned out to need: that arc could not start at the
backend because the factory had nowhere to read configuration from, and
building the provider first would have baked that gap in. OIDC and SAML are
separate modules under `providers/`, on the terms ADR-023 decision 5 sets
and ADR-024 measured — 32 modules and 18 third-party packages for the two
of them, which is the case where the dependency argument does hold.

**There is no conformance suite for this contract yet.** The credential
contract has one (ADR-027) and this one should, but writing it before a
real provider exists would produce checks shaped by the fake rather than by
a protocol. It is named here so the omission is a decision rather than an
oversight.

**Sign-in is not wired into a page.** `FederatedSet` gives `Begin`,
`Complete` and the route paths; mounting them and choosing where the
browser lands afterwards belongs to the application, and to Orbit for its
panel. The stale claim in `pkg/auth/backend_registry_test.go` that the
credential seam unblocks SAML and OIDC is corrected in the same change that
made it false to leave.
