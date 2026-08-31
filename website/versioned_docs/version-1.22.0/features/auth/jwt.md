---
sidebar_position: 3
title: JWT
covers:
  - pkg/auth.NewJWTManager
  - pkg/auth.NewJWTManagerFromKeys
  - pkg/auth.JWTManager.Generate
  - pkg/auth.JWTManager.Validate
  - pkg/auth.JWTManager.RotateKey
  - pkg/auth.JWTManager.RemoveKey
  - pkg/auth.JWTManager.JWKSHandler
  - pkg/app.JWTKeySpec
  - pkg/nucleus.Runtime.JWT
config_keys:
  - jwt_secret
  - jwt_expiry
  - jwt_issuer
  - jwt_keys[]
  - jwt_current_kid
---

# JWT

`pkg/auth` exposes a `JWTManager` for stateless auth. It has two modes, which
can coexist in the same process:

- **Single secret, HS256** — one shared secret, no key IDs. Good for getting
  started.
- **Multi-key keyset** — several keys with rotation and a public JWKS
  endpoint. This is the production path.

## Single-secret HS256 (quick start)

```go
mgr := auth.NewJWTManager(secret, 24*time.Hour, "my-issuer")

token, err := mgr.Generate(userID, username, role)
claims, err := mgr.Validate(token)
```

The secret comes from the `jwt_secret` config key. Set it through the
`NUCLEUS_JWT_SECRET` environment variable rather than writing it into
`nucleus.yml` — config files end up committed.

`jwt_secret` is a non-nullable security key: setting it to `null`, or
exporting an empty `NUCLEUS_JWT_SECRET`, is a boot error rather than a silent
fall-back to no secret.

Tokens in this mode carry no `kid` header.

## Multi-key with rotation (production)

`App.New` builds `App.JWT` automatically when `jwt_keys[]` is set in
`nucleus.yml`. Operators do not call `auth.NewJWTManagerFromKeys`
themselves for the common case.

```yaml
# nucleus.yml
jwt_issuer: myapp
jwt_current_kid: 2026-q2-rsa
jwt_keys:
  - kid: 2026-q2-rsa
    algorithm: RS256
    pem_path: /run/secrets/jwt-rsa-q2.pem
  - kid: legacy-hs
    algorithm: HS256
    secret_env: JWT_LEGACY_SECRET
```

## AWS Secrets Manager key references

For keys stored in AWS Secrets Manager, use the `aws-sm:` scheme in the
`secret_env` or `pem_env` field instead of a plain environment variable
name:

```yaml
jwt_keys:
  - kid: 2026-q2-rsa
    algorithm: RS256
    # Fetch the whole SecretString as the PEM document:
    pem_env: aws-sm:myapp/prod/jwt-rsa-q2

  - kid: 2026-q2-hs
    algorithm: HS256
    # Fetch the "signing" JSON key out of a JSON-object secret:
    secret_env: aws-sm:myapp/prod/jwt-secrets#signing
```

Reference forms:

| Form                                  | Resolution                                                       |
| ------------------------------------- | ---------------------------------------------------------------- |
| `aws-sm:<secret-id>`                  | The full `SecretString` of the named secret.                     |
| `aws-sm:<secret-id>#<json-key>`       | One string-valued key from a JSON-object `SecretString`.         |
| `env:NAME` or bare `NAME`             | The value of the named environment variable (existing behaviour).|

The AWS SDK client is built lazily — only when at least one `jwt_keys[]`
entry uses an `aws-sm:` reference. Deployments that do not use AWS Secrets
Manager never trigger AWS credential resolution. The SDK uses the standard
AWS credential chain: environment variables, shared config, IAM role, and so
on.

Only text-valued secrets are accepted — UTF-8 HMAC secrets or PEM documents.
Binary secrets (those with no `SecretString`) are not supported for JWT key
material, and resolving one fails at startup.

`App.New` selects the construction path automatically:

- `jwt_keys[]` non-empty: multi-key manager; `jwt_secret` is ignored.
- `jwt_keys[]` empty, `jwt_secret` set: legacy single-secret HS256 manager.
- Both unset: `App.JWT == nil` with a startup `WARN`. Tokens are never
  signed with an empty HMAC key.

For programmatic / non-config use cases:

```go
mgr, err := auth.NewJWTManagerFromKeys([]auth.SigningKey{
    {KID: "2026-q2-rsa", Algorithm: auth.RS256, RSAPrivate: priv},
}, "2026-q2-rsa", 24*time.Hour, "my-issuer")

token, _ := mgr.Generate(userID, username, role)
claims, _ := mgr.Validate(token)
```

Tokens carry a `kid` header identifying the signing key. `Validate`
looks the key up in the keyset, rejecting tokens whose `kid` is
unknown.

To rotate signing keys without invalidating outstanding tokens:

```go
// 1. Add a new key, mark it as current. New tokens are signed with it.
err := mgr.RotateKey(auth.SigningKey{
    KID: "2026-q3-rsa", Algorithm: auth.RS256, RSAPrivate: nextPriv,
}, true)

// 2. Existing tokens (signed with the previous key) keep validating
//    until they expire on their own.

// 3. After the access-token lifetime has passed, drop the old key.
err = mgr.RemoveKey("2026-q2-rsa")
```

`HS256` keys are also supported in the keyset (use `SigningKey.HMACSecret`
instead of `RSAPrivate`); the same rotation primitives apply.

## Module access via Runtime

If your module mints or verifies tokens, use the manager the framework already
built from `jwt_secret` / `jwt_keys[]`. Do not construct a second
`auth.JWTManager` from a duplicated secret. Capture it once in `OnStart`:

```go
var jwtMgr *auth.JWTManager

var tokenModule = nucleus.Module[struct{}]{
    Name:   "tokens",
    Prefix: "/tokens",
    OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
        jwtMgr = rt.JWT() // *auth.JWTManager; nil when no signing material is configured
        return nil
    },
    Routes: func(r nucleus.Router, _ struct{}) {
        r.Post("/issue", issueToken)
    },
}

func issueToken(c *nucleus.Context) error {
    if jwtMgr == nil {
        return errors.Unauthorized("JWT not configured")
    }
    token, err := jwtMgr.Generate(userID, username, role)
    if err != nil {
        return err
    }
    return c.JSON(http.StatusOK, map[string]string{"token": token})
}
```

`rt.JWT()` returns nil on an unbacked runtime AND when no signing material is
configured (`jwt_secret` unset and `jwt_keys[]` empty). Always guard before
use.

`RotateKey` and `RemoveKey` are operator-level key-lifecycle operations — they
mutate shared state and are not safe to call from per-request module code. Use
them only in admin or startup paths, exactly as with `rt.Authorizer()`'s
in-memory policy mutations.

## JWKS endpoint

Relying parties consuming RS256 tokens (other services, API gateways,
identity proxies) fetch the public key set from a well-known URL.

When at least one RS256 key is present in `jwt_keys[]`, `App.New`
auto-mounts the handler at `/.well-known/jwks.json`. The bootstrap
allow-list already permits anonymous access to that path. No
application code is needed.

For non-default paths or a programmatic manager, mount manually:

```go
a.Router.Get("/.well-known/jwks.json", router.FromHTTP(mgr.JWKSHandler()))
```

The handler emits the standard RFC 7517 / RFC 7518 shape:

```json
{
  "keys": [
    {
      "kid": "2026-q2-rsa",
      "kty": "RSA",
      "alg": "RS256",
      "use": "sig",
      "n": "<base64url(modulus)>",
      "e": "<base64url(exponent)>"
    }
  ]
}
```

`HS256` keys are intentionally excluded from the JWKS response — the
endpoint is public and HMAC keys are shared secrets. Callers using
HS256-only managers will see an empty `keys` array.
