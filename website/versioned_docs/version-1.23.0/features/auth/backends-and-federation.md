---
sidebar_position: 5
title: Backends & federated sign-in
covers:
  - pkg/auth.BackendConfig
  - pkg/auth/backend.Config.Bind
  - pkg/auth/backend.Backend
  - pkg/auth/backend.Config
  - pkg/auth/backend.Register
  - pkg/auth/backend/backendtest.Run
  - pkg/auth/backend/backendtest.Suite
  - pkg/auth.ChainConfig
  - pkg/auth.NewChainFrom
config_keys: []
---

# Backends & federated sign-in

Authentication backends answer one question — do these credentials belong
to a real user — and are consulted as an **ordered chain**. Federated
identity providers (OIDC, SAML) are a separate contract, because there are
no credentials to hand to a chain. This page covers both.

## Authenticating against a directory

Authentication backends are selected **by name**, so a backend lives in its
own module and you import it. The framework itself carries no LDAP client,
no SAML service provider and no OIDC client — an application that does not
authenticate against any of them should not download them.

### LDAP

LDAP ships with Nucleus, as a separate module:

```bash
go get github.com/jcsvwinston/nucleus/providers/ldap
```

Import it for its side effect — the shape `database/sql` drivers use — and
name it in the chain:

```go
import _ "github.com/jcsvwinston/nucleus/providers/ldap"
```

```yaml
# nucleus.yml
auth_backends: [ldap, local]

auth:
  ldap:
    url: "ldaps://dc.corp.local:636"
    base_dn: "ou=people,dc=corp,dc=local"
    bind_dn: "cn=svc-nucleus,ou=services,dc=corp,dc=local"
    bind_password: "${LDAP_BIND_PASSWORD}"
```

Every setting the backend takes is documented in [its README][ldap-readme],
along with what it refuses to do — an empty password is rejected before a
connection is opened, a username is filter-escaped before it reaches the
query, an ambiguous search is a rejection rather than a coin toss, and only
a directory that actually says "wrong credentials" produces a rejection.

[ldap-readme]: https://github.com/jcsvwinston/nucleus/tree/main/providers/ldap

Naming a backend you have not imported fails at boot with the two lines
that fix it — the `go get` and the `import`. It is not left for the first
person who tries to log in.

### Anything else

The same seam is open to you:

```go
package acmeauth

import "github.com/jcsvwinston/nucleus/pkg/auth/backend"

func init() {
    backend.Register("acme", New)
}
```

### Check your backend against the contract

The contract has parts that are easy to get wrong and expensive to get
wrong. Point the conformance suite at your backend and it will tell you:

```go
func TestConformance(t *testing.T) {
    backendtest.Run(t, backendtest.Suite{
        New:           func() (backend.Backend, error) { return New(cfg) },
        ValidUser:     "ana",
        ValidPassword: "correcta",
        UnknownUser:   "nadie",
        // Optional, and the most valuable: the same backend pointed at a
        // source it cannot reach. Only you know how to break your own
        // connection.
        Unavailable: func() (backend.Backend, error) { return New(deadCfg) },
    })
}
```

It checks that a wrong password and an unknown user produce the *same*
rejection — a caller that can tell them apart can enumerate your users —
that an empty password is refused, and that an unreachable source reports
*unavailable* rather than a rejection, which is what keeps the chain
falling through to the local account someone needs on the morning the
directory is down.

It checks the contract, not the behaviour: passing does not mean your
backend talks to the right directory.

Import `pkg/auth/backend`, not `pkg/auth`. It is a leaf package holding the
contract and nothing else — the interface, the identity it returns, the two
sentinel errors, the configuration subtree and the registry — so writing a
backend does not drag in session stores, JWT, Redis, Prometheus and
OpenTelemetry to implement two methods. The names remain available from
`pkg/auth` as aliases, so existing code keeps compiling.

A backend answers one question — do these credentials belong to a real
user — and returns one of three things: the user, `ErrInvalidCredentials`
when the answer is certainly no, or `ErrBackendUnavailable` when it could
not reach the directory at all.

That third case is why backends are consulted as an **ordered chain**
rather than a set:

```yaml
auth_backends: [ldap, local]
```

The directory answers first; the local user table answers second. When the
directory is unreachable, the chain records it and moves on — so the
break-glass account you keep for exactly that morning still works. A set
could not express that, and neither could a single configured backend.

The distinction survives to the end. If every backend rejected, the caller
gets "invalid credentials", because that is what happened. If any backend
was unreachable, the caller gets an error saying which — "wrong password"
and "the directory is down" send an operator to very different places, and
guessing between them wastes the hour that matters.

### Each backend reads its own settings

A backend named in `auth_backends` owns the `auth.<name>.*` subtree. The
framework validates only that the section belongs to a registered name and
hands the contents over; the backend declares their shape and validates
them:

```go
func New(bc auth.BackendConfig) (auth.Backend, error) {
    var cfg struct {
        URL     string        `koanf:"url" validate:"required"`
        Timeout time.Duration `koanf:"timeout" default:"5s"`
    }
    if err := bc.Bind(&cfg); err != nil {
        return nil, err
    }
    // …
}
```

Two things fail rather than pass quietly:

- **A key the backend does not declare.** A misspelled directory URL would
  otherwise sit unnoticed until the day the setting mattered.
- **A section for a backend the chain does not name.** `auth.ldap.*`
  without `ldap` in `auth_backends` is read by nobody — the chain is its
  only consumer — so it boots clean and the login page never consults the
  directory you configured. There is no reading of that file under which it
  does something, so it is an error.

A misspelled section under `auth.` is still an unknown key, exactly as
before: the exemption is per registered name, never for the namespace.

### Your own user table is a backend too

Implement `auth.UserProvider` — the interface that describes how to reach
your users — and register it:

```go
nucleus.New().
    FromConfigFile("nucleus.yml").
    WithUserProvider(myUserStore)
```

It appears in the chain as `local` (or under a name you choose with
`WithUserProviderNamed`). Modules reach the assembled chain through
`rt.AuthChain()`, so a module that owns a sign-in page authenticates
through the order the operator declared rather than going straight to the
user table — the point of declaring an order is that it applies everywhere.
The full worked slice — table, provider, login route — is
[Your first login](./your-first-login.md).

A backend named in `auth_backends` that nobody registered fails at **boot**,
naming what is registered — and, when it is one Nucleus publishes, naming
the `go get` and the import that would register it. A typo in an
authentication list should not wait until the first person tries to log in.

`nucleus doctor --check auth` reviews the chain from the configuration
alone: a backend declared with no settings of its own, and a chain whose
every entry depends on an outside system — which is the deployment where
nobody can log in on the morning the directory is down, including whoever
would fix it.

## Federated sign-in (OIDC, SAML)

A directory is a username and a password. An identity provider is not: the
person leaves for it, authenticates there, and the answer arrives at a
different URL some time later. There are no credentials to hand to the
chain, so federated providers are declared separately and implement a
separate contract of their own.

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

**`name` is yours, `provider` is the protocol.** They are different fields
because two identity providers of the same protocol is ordinary — a
corporate tenant and a partner one, staging and production. `name` is what
appears in the URL, what the settings hang off (`auth.<name>.*`, the same
subtree a credential backend reads), and what the sign-in button says when
`display_name` is empty.

**`public_base_url` is required** whenever `auth_federated` is non-empty,
and it is the address the **browser** uses — behind a reverse proxy or in
a container that is not the address the process binds. The callback URL is
derived from it, and it is the one value you have to register with your
identity provider:

```
https://app.example.com/auth/corp/callback
```

Nucleus logs each one at startup for exactly that reason. A callback that
does not match is a sign-in that only fails in production.

**Your application mounts the two routes; the framework does not.** Nucleus
builds the providers and owns the flow — it issues the anti-forgery state,
holds the pending sign-in and refuses a callback that does not carry the
state back — but what happens after a successful callback is yours to
decide: which session manager, which landing page, which account gets
linked. So the handlers are yours:

```go
set := a.AuthFederated // *auth.FederatedSet, built from auth_federated

// Mount on a.Router — the mux the server serves. http.Handle registers
// on http.DefaultServeMux, which the nucleus server never consults, so
// those routes would 404.
a.Router.HandleFunc(auth.FederatedStartPath("corp"),
    func(w http.ResponseWriter, r *http.Request) {
        redirectURL, state, err := set.Begin(r.Context(), "corp")
        if err != nil {
            http.Error(w, "sign-in unavailable", http.StatusBadGateway)
            return
        }
        // Store `state` in a short-lived, HttpOnly cookie and hand it back
        // to Complete on the callback.
        http.SetCookie(w, &http.Cookie{
            Name: "fed_state", Value: state, Path: "/",
            HttpOnly: true, MaxAge: 300,
        })
        http.Redirect(w, r, redirectURL, http.StatusFound)
    })

a.Router.HandleFunc(auth.FederatedCallbackPath("corp"),
    func(w http.ResponseWriter, r *http.Request) {
        c, err := r.Cookie("fed_state") // the cookie the start step set
        if err != nil {
            http.Error(w, "missing state", http.StatusBadRequest)
            return
        }
        user, err := set.Complete(r.Context(), "corp", c.Value, r)
        if err != nil {
            http.Error(w, "sign-in failed", http.StatusUnauthorized)
            return
        }
        // Establish your session and send the browser where you want it.
        _ = user
    })
```

Use `auth.FederatedStartPath` and `auth.FederatedCallbackPath` rather than
writing the paths by hand: the callback URL logged at startup is derived
from the same functions, so the URL you register with the identity provider
is the one your route actually serves.

**The sign-in routes must answer an unauthenticated browser.** With the
default-deny RBAC middleware mounted, every request without claims resolves
to the `anonymous` subject, so the start route returns **403 before `Begin`
ever runs** unless you grant `anonymous` access to exactly these two paths:

```go
for _, p := range []string{
    auth.FederatedStartPath("corp"),
    auth.FederatedCallbackPath("corp"),
} {
    _ = a.Authorizer.AddPolicy("anonymous", p, "*")
}
```

An app built with `app.WithOpenAuthz()` skips this — there is no enforcement
to open a hole in — but any app on the default stack needs it.

A runnable end-to-end version of all of the above — provider registration,
route mounting, the anonymous policy, and a start→callback round-trip — is
kept as an executable example in `pkg/app/federated_routes_example_test.go`,
so the documented path is exercised in CI rather than only described here.

**The framework keeps the anti-forgery state.** A provider never sees it:
Nucleus issues it, holds the pending sign-in, and refuses a callback that
does not carry it back, before the provider is consulted at all. A state is
single use and bound to the instance that issued it, and a sign-in left
unfinished expires. That division is deliberate — the `state` parameter is
the part of a redirect flow that works perfectly well when it is missing,
right up until somebody attacks it, so a provider author is not given the
chance to leave it out.

A subtree for an instance you did not declare in `auth_federated` is an
**unknown key**, not a silently ignored one. The exemption is per declared
instance and never for the whole `auth.` namespace.

Federated sign-in and `auth_backends` are independent, and most
applications want both: the identity provider for everybody, and a local
account or directory that still works the morning the identity provider
does not.

One rule for anyone writing a backend: reject an unknown user and a wrong
password **identically**, and in the same time. A backend that answers
faster for a user that does not exist has published a list of your users,
and because the chain stops on rejection, it publishes it for every
backend behind it too.
