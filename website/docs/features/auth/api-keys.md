---
sidebar_position: 6
title: API keys
covers:
  - pkg/auth/apikeys.Issue
  - pkg/auth/apikeys.Authenticate
  - pkg/auth/apikeys.Rotate
  - pkg/auth/apikeys.Key
  - pkg/auth/apikeys.Key.Active
  - pkg/auth/apikeys.Key.HasScope
  - pkg/auth/apikeys.Store
  - pkg/auth/apikeys.NewSQLStore
  - pkg/auth/apikeys.SQLStore
  - pkg/auth/apikeys.SQLStoreConfig
  - pkg/auth/apikeys.Middleware
  - pkg/auth/apikeys.Require
  - pkg/auth/apikeys.FromContext
  - pkg/auth/apikeys.HeaderName
  - pkg/auth/apikeys.Prefix
  - pkg/auth/apikeys.ErrInvalidKey
  - pkg/auth/apikeys.ErrNotFound
  - pkg/app.WithAPIKeys
  - pkg/nucleus.WithAPIKeys
  - pkg/nucleus.AppBuilder.WithAPIKeys
  - pkg/auth/apikeys.ScopeSubject
  - pkg/auth/apikeys.ScopeSubjectPrefix
---

# API keys

The credential a program uses to call your API: issued once, shown once,
revocable, scoped, and recognisable in a log.

A framework with only passwords and browser sessions pushes every
machine-to-machine caller into one of two bad places — a shared password in
a config file, or a JWT nobody can revoke. This is the third option.

## The shape of a key

```
nk_a1b2c3d4_zm9vymfyymf6cxv4...
│  │        └ the secret: 256 bits, never stored
│  └ the key id: what a listing shows and a log line names
└ a fixed prefix, so a leaked key is recognisable in a scan
```

The prefix is not decoration: secret scanners match on prefixes, and a key
that looks like any other base64 string is one nobody can find in the
repository they just leaked. The alphabet is base32 — letters and digits
only — so a key survives being read aloud, typed from a screenshot, and
pasted into a shell.

## Issuing

```go
store, err := apikeys.NewSQLStore(ctx, db, apikeys.SQLStoreConfig{
    Flavor: apikeys.FlavorPostgres,
})

key, secret, err := apikeys.Issue(ctx, store, apikeys.Key{
    Name:    "ci",
    OwnerID: account.ID,
    Scopes:  []string{"billing:read"},
})
// `secret` is the only time the caller can copy it.
```

Only a SHA-256 of the secret is stored — not bcrypt, deliberately: the
secret already carries 256 bits of entropy, so there is nothing to brute
force, and a per-request bcrypt on an API call would be a denial of service
its caller controls.

Or from the command line, which is where keys are actually issued:

```bash
nucleus apikey create --name ci --owner acct_123 --scopes "billing:read"
nucleus apikey list
nucleus apikey rotate --id a1b2c3d4 --grace 24h
nucleus apikey revoke --id a1b2c3d4
```

## Turning it on

`nucleus add apikeys` writes one call into the `nucleus.New()` chain of
`main.go`:

```go
nucleus.New().
    FromConfigFile("nucleus.yml").
    WithAPIKeys().
    Start()
```

The keys live in the application's default database, in the table `nucleus
apikey create --config nucleus.yml` issues into (`nucleus_api_keys`, created
on first use). The store speaks SQLite, PostgreSQL and MySQL; another default
engine fails boot with its name. The middleware is mounted next to the bearer
decode — before the rate limiter and the request interceptors — so both see
the key's owner. A route that must have a key says so:

```go
r.With(apikeys.Require("billing:read")).Get("/invoices", listInvoices)
```

## Who a key is, for a policy

On the default stack the default-deny layer authorises a request that
presented a key as the key's **owner** — the same subject a bearer token
with that user id is, and the same an account's signed-in session is, so a
key issued with `--owner <account id>` acts as that account. Each scope the
key carries is a subject too, `scope:<name>`, and `anonymous` comes last. A
request is allowed when any of them is:

```csv
# the owner, and a role it holds
p, svc-billing, /invoices*, read, allow
g, svc-audit, auditors
p, auditors, /reports*, read, allow
# every key that carries a scope, whoever owns it
p, scope:billing:read, /invoices*, read, allow
```

A key with no scopes reaches what its owner and `anonymous` reach, and no
route granted only to a scope. A route that must have both — this identity
*and* this scope — keeps `Require` on it: the policy decides who, `Require`
decides what the key has to carry.

Before, the layer saw `anonymous` for every key and `Require` was the only
gate. An application that relied on that keeps working: `anonymous` is still
tried, so a route granted to it is still reached.

## Authenticating a request

Without `WithAPIKeys`, the middleware is yours to mount:

```go
r.Use(apikeys.Middleware(store))
r.With(apikeys.Require("billing:read")).Get("/invoices", listInvoices)
```

`Middleware` accepts `X-API-Key` and `Authorization: Bearer nk_…`, and
**leaves any other bearer token alone**, so JWT and API keys share the
header without fighting. A request with no key passes through untouched:
authentication is what the middleware does, and `Require` is what refuses.

It also puts the key's owner in the observability context — which is not
bookkeeping. The rate limiter keys on the authenticated user id with the
tenant as a prefix, so a key that lands there is throttled **as an
identity** instead of sharing a bucket with everyone behind the same
address — provided the middleware runs before the limiter, which is where
`WithAPIKeys` mounts it. Mounted with `Use` after the application is built
(on the builder or on a module's router), it runs after the limiter, and
every key behind one address shares that address's bucket.

`Require("scope")` answers `403` for a key without the scope, not `401`:
the caller is authenticated, and presenting the same key again will not
help.

A key with **no** scopes carries none. An empty list never means "every
scope" — that default is how an unscoped key ends up more powerful than a
scoped one.

## Rotation

```go
replacement, secret, err := apikeys.Rotate(ctx, store, oldID, 24*time.Hour)
```

Both keys work during the grace period, and only the new one after it. A
rotation that breaks the caller at the moment it happens is a rotation
nobody performs — so the old key is given an *expiry* rather than a
revocation, and there stays exactly one rule about when a key stops
working. Pass a zero grace to cut it off immediately.

`Key.RotatedFrom` records what a key replaced, so an audit can follow the
chain.
