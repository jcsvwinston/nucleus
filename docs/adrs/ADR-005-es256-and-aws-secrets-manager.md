# ADR-005: ES256 JWT Signing and AWS Secrets Manager Key Resolution

**Status:** Accepted
**Date:** 2026-05-14
**Superseded:** No

## Context

The post-ADR-004 audit (`docs/audits/2026-05-14-post-sprint-readiness.md`) re-confirmed two items that were P0 before the ADR-004 integration sprint deprioritised them:

1. **ES256 was a stub.** `pkg/auth/jwt.go` advertised an extensible `SigningAlgorithm` switch but the ES256 branch fell through to `nil`. An operator could write `algorithm: ES256` in `nucleus.yml` and `App.New` would fail late and unhelpfully. RS256 was the only asymmetric option, which forces 2048-bit-or-larger keys and larger tokens on deployments that have standardised on EC.

2. **Key material only loads from env vars or files.** `JWTKeySpec` resolves HMAC secrets from `secret_env` and PEM material from `pem_path` / `pem_env`. There is no path to a managed secret store. Every other enterprise framework Nucleus benchmarks against can pull signing keys from a cloud secret manager; Nucleus cannot. `pkg/storage.CredentialSource` has a `secret_manager` field but the 2026-05-13 audit confirmed it only accepts `env:` references — it is a placeholder, not an integration.

The owner approved an **MVP scope** on 2026-05-14: ES256 with the P-256 curve only, plus an AWS Secrets Manager resolver only. Explicit non-goals for this ADR: P-384 (ES384), P-521 (ES512), Ed25519, GCP Secret Manager, Azure Key Vault, HashiCorp Vault.

The hard constraint is the **dependency firewall** (`contracts/firewall_test.go`): the AWS SDK must not leak its types into any stable `pkg/*` surface, and per `CLAUDE.md` §3 a new third-party dependency requires this ADR plus a `dependency-impact` review.

## Decision

### Part 1 — ES256 (P-256 only), pure stdlib

`pkg/auth` gains an `ES256` `SigningAlgorithm` and a `SigningKey.ECDSAPrivate *ecdsa.PrivateKey` field. The existing `validate` / `signingMethod` / `signMaterial` / `verifyMaterial` / `toJWK` switches each gain an `ES256` case. `validate` rejects any curve other than `elliptic.P256()` — a P-384 key with `algorithm: ES256` fails loudly at `App.New` rather than producing tokens with an `alg` header that does not match the key.

The JWK wire shape (`pkg/auth.JWK`) gains `Crv`, `X`, `Y` fields with `omitempty` tags. An EC JWK carries `kty: "EC"`, `crv: "P-256"`, and the fixed-length (32-byte, left-padded) big-endian coordinates per RFC 7518 §6.2. RSA JWKs are unchanged. HMAC keys remain excluded from JWKS output.

`pkg/app/jwt_setup.go` gains an `ES256` case in `loadJWTKey` and a `parseECDSAPrivateKey` that accepts both SEC1 (`EC PRIVATE KEY`) and PKCS#8 (`PRIVATE KEY`) PEM blocks — matching the dual-format tolerance the RS256 path already has. The shared PEM plumbing (`loadPEMBytes`, `decodeSinglePEMBlock`) is factored out so both algorithms reject trailing PEM content identically.

**No new dependency.** ES256 is `crypto/ecdsa` + `crypto/elliptic` from the standard library, and `github.com/golang-jwt/jwt/v5` (already a direct dependency) ships `jwt.SigningMethodES256`.

### Part 2 — AWS Secrets Manager resolver, behind a framework interface

A new package `pkg/auth/secrets` defines:

```go
// Resolver turns an opaque reference string into raw secret bytes.
type Resolver interface {
    Resolve(ctx context.Context, ref string) ([]byte, error)
}
```

Two implementations:

- `EnvResolver` — resolves `env:VARNAME` references from the process environment. Zero dependencies. This generalises the `env:`-only behaviour `pkg/storage.CredentialSource` already documents, so the pattern is consistent framework-wide.
- `AWSSecretsManagerResolver` — resolves `aws-sm:<secret-id>` (optionally `aws-sm:<secret-id>#<json-key>`) references via the AWS Secrets Manager `GetSecretValue` API. The AWS SDK is an **implementation detail of this type** — the constructor returns the `Resolver` interface, and no exported symbol in `pkg/auth/secrets` (or anywhere in `pkg/*`) names an AWS SDK type.

The SDK surface the resolver depends on is narrowed to a one-method interface inside the package:

```go
// secretsManagerAPI is the slice of the AWS SDK the resolver needs.
// The real *secretsmanager.Client satisfies it; tests substitute a fake.
type secretsManagerAPI interface {
    GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput,
        optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}
```

This keeps the resolver unit-testable without an AWS account or network, and confines the SDK import to exactly one package.

### Part 3 — wiring into JWTKeySpec

`JWTKeySpec` gains no new fields. Instead, the existing `SecretEnv` and `PemEnv` fields are reinterpreted: a value of the form `aws-sm:<id>` is routed through the `AWSSecretsManagerResolver` instead of `os.Getenv`. A bare name (no `aws-sm:` prefix) keeps the current env-var behaviour. This avoids a config-surface change and keeps `nucleus.yml` files that use env vars working untouched.

`App.New` constructs a resolver chain — `EnvResolver` always, `AWSSecretsManagerResolver` when any `jwt_keys[]` entry uses an `aws-sm:` reference — and `jwt_setup.go` resolves key material through it. The AWS resolver is lazy: if no key references `aws-sm:`, the SDK client is never constructed and no AWS credential chain is touched.

## Consequences

### Positive

- ES256 deployments work end to end from `App.New`: config → key load → sign → validate → JWKS publication. The audit's "ES256 not implemented" finding is closed.
- Operators can keep JWT signing keys in AWS Secrets Manager instead of mounting them as files or env vars — the credential never lands on disk or in the process environment in plaintext.
- The `Resolver` interface is the seam for GCP / Azure / Vault later: a follow-up adds a new implementation without touching `pkg/auth/jwt.go` or `JWTKeySpec`.
- The dependency firewall stays intact — `dependency-impact` review (run as part of the iteration loop) confirms no AWS SDK type appears in a stable `pkg/*` signature.

### Negative

- **New third-party dependency.** `github.com/aws/aws-sdk-go-v2/config` and `github.com/aws/aws-sdk-go-v2/service/secretsmanager`, plus their transitive `aws-sdk-go-v2` core, `smithy-go`, and HTTP/JSON machinery. This is the first cloud-vendor SDK in the tree. It is gated to the `pkg/auth/secrets` package and only linked when an operator actually references `aws-sm:`. The `dependency-impact` review records the exact transitive set and the binary-size delta.
- The `aws-sm:` reference syntax is new surface operators must learn. It is documented in `docs/guides/AUTH_GUIDE.md` and `docs/reference/CONFIG_KEY_REGISTRY.md`, and the `algorithm` / `*_env` field semantics are extended rather than replaced, so no existing config breaks.
- Reusing `SecretEnv` / `PemEnv` for non-env references is mild semantic overloading. The alternative — a dedicated `secret_ref` field — was rejected because it doubles the config surface and forces every operator to learn two ways to point at key material. The prefix convention (`aws-sm:`) is unambiguous and self-documenting.

### Neutral

- The MVP curve restriction (P-256 only) is enforced in code, not just documentation. Adding P-384 later is a `validate` change plus a `signingMethod` case — a deliberate, reviewable expansion, not a silent capability.
- `pkg/storage.CredentialSource` is left untouched by this ADR. Unifying it with `pkg/auth/secrets.Resolver` is a reasonable future cleanup but out of scope here — the storage credential path is `stable` and changing it needs its own compatibility review.

## Compliance

After this ADR is accepted:

1. `pkg/auth/jwt.go` has an `ES256` algorithm with P-256-only `validate`, and `JWK` carries `Crv` / `X` / `Y`.
2. `pkg/app/jwt_setup.go` loads ES256 keys from SEC1 or PKCS#8 PEM and rejects non-P-256 curves.
3. `pkg/auth/secrets` exists with the `Resolver` interface, `EnvResolver`, and `AWSSecretsManagerResolver`; no exported symbol names an AWS SDK type.
4. `contracts/firewall_test.go` passes — no AWS SDK type in a stable `pkg/*` signature.
5. A `dependency-impact` review is recorded for the AWS SDK addition (transitive set + size delta).
6. `docs/reference/CONFIG_KEY_REGISTRY.md` documents the `aws-sm:` reference syntax for `secret_env` / `pem_env`; `docs/guides/AUTH_GUIDE.md` documents ES256 and the AWS Secrets Manager path.
7. `CHANGELOG.md` records ES256 support and the AWS Secrets Manager resolver under `### Added`, and notes the new (optional, lazily-linked) dependency.
8. The AWS SDK is imported only by `pkg/auth/secrets`; `go list` confirms no other package pulls it.

## Related

- [`pkg/auth/jwt.go`](../../pkg/auth/jwt.go) — JWT manager + JWKS.
- [`pkg/app/jwt_setup.go`](../../pkg/app/jwt_setup.go) — `JWTKeySpec` → `auth.SigningKey`.
- `docs/audits/2026-05-14-post-sprint-readiness.md` §7 — the audit recommendation this ADR acts on.
- ADR-001: stdlib-first runtime — ES256 honours it (pure stdlib); the AWS SDK is the deliberate, gated exception this ADR documents.
- ADR-004: Casbin default-deny — the integration sprint that deprioritised this work.
- `CLAUDE.md` §3 — the rule requiring an ADR + `dependency-impact` review for new third-party dependencies.
