# ADR-008: CSRF Middleware Follow-ups — Logger, Key Type, Secure-By-Default Cookie

**Status:** Accepted
**Date:** 2026-05-15
**Supersedes:** No
**Related:** ADR-006 (CSRF hardening — constant-time compare + mandatory `EncryptionKey`).

## Context

ADR-006 closed two security defects in `pkg/router/csrf.go` (non-constant-time token comparison and a silently-derived `EncryptionKey`). Three follow-ups were deferred from the PR #60 review loop:

1. **No observability for encrypt/decrypt errors.** `encryptToken` / `decryptToken` failures are silently swallowed: a server-side encryption failure drops the `XSRF-TOKEN` cookie with no log, and a decrypt failure on a tampered/garbage `X-XSRF-TOKEN` header leaves `submitted` empty so the request falls through to the constant-time compare. Both are *behaviourally correct* — the middleware does not crash and does not accept the bad input — but operators see nothing in `slog` when a real KMS outage or a sustained probing campaign hits the endpoint.

2. **`CSRFOptions.EncryptionKey string`.** AES-256 key material is a 32-byte raw blob; `string` is a misleading type for it. Hex/base64-encoded keys read from config end up `[]byte` after decoding, then re-wrapped in `string` solely to populate the field. The internal code already converts back to `[]byte` at the `aes.NewCipher([]byte(key))` boundary. Holding raw bytes in a `string` is also a minor footgun for accidental logging (the string-form is more likely to slip into a structured log than a `[]byte`, which slog renders as a base64 blob).

3. **`Secure: false` cookie default.** The CSRF cookie (`_csrf`) and the XSRF cookie (`XSRF-TOKEN`) are set with `Secure: opts.Secure`, where `opts.Secure` defaults to `false`. The docstring on the field admits *"default: false, should be true in production"*. Security-by-default (SPEC.md §2 principle 4) inverts the polarity: the safe path is the zero-value path, the opt-out is explicit.

The audit (`docs/audits/2026-05-14-post-sprint-readiness.md` §5 risk 5, §7 task 5) flagged all three as items to fold into a single follow-up ADR rather than three independent breaking changes spaced over weeks.

## Decision

### 1. `Logger *slog.Logger` field on `CSRFOptions`

Add an optional `Logger *slog.Logger` field. `CSRFOptions.defaults()` populates it with `slog.Default()` when nil, so callers who do not set it get the process-wide logger transparently. `router.DefaultStack` plumbs the router's logger into `CSRFOptions.Logger` so that operators who configure `app.New(cfg).Router` get CSRF logs annotated with the same handler (redaction, attributes, sink) as the rest of the app.

Log policy:

- **Encrypt failures** are logged at `WARN`. They indicate a real server-side problem (RNG failure, key rejected by `aes.NewCipher`, GCM construction error) and the cookie has been dropped — an operator wants to know.
- **Decrypt failures on the `X-XSRF-TOKEN` header** are logged at `DEBUG`. They are public-endpoint noise: every attacker probe and every browser stuck with a stale-key cookie produces one. Visible in development, opt-in for production (set log level to `DEBUG` if you want them).

Log fields: `method`, `path`, `error` (already redacted by ADR-007's `slog` handler if `error` ever leaks a secret, which today it does not).

### 2. `CSRFOptions.EncryptionKey []byte` (was `string`)

The field type changes from `string` to `[]byte`. The internal usage already operates on `[]byte` (`aes.NewCipher([]byte(key))`); this change removes one redundant string→bytes conversion and aligns the type with the semantics of "raw 32-byte AES-256 key material".

Validation in `validate()` is unchanged in spirit (length must be 32 when `EnableXSRFCookie` is set); the only adjustment is `len(o.EncryptionKey)` already returns the byte count for both types, so no logic moves.

This is a **breaking change on a stable surface**. The contract baseline entry for `CSRFOptions.EncryptionKey` reflects the field name, not its type, but the type is part of the call signature for anyone constructing the struct. Pre-`v1.0` SemVer (per `docs/governance/COMPATIBILITY_SLO.md`) permits the change; the migration is mechanical:

```go
// Before
EncryptionKey: "0123456789abcdef0123456789abcdef",

// After
EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
```

### 3. `CSRFOptions.Secure bool` → `CSRFOptions.InsecureCookie bool` (polarity flip)

Replace the `Secure bool` field with `InsecureCookie bool`. Default zero-value (`InsecureCookie: false`) means the CSRF and XSRF cookies are issued with `Secure: true` — the production-safe path is now the default. Operators on plain-HTTP local-dev who *need* the cookie over HTTP set `InsecureCookie: true` explicitly; their config now documents the security choice instead of accidentally inheriting it.

Internal implementation: the cookie code reads `!opts.InsecureCookie` where it previously read `opts.Secure`. No tri-state pointers, no defaults-time guesswork — the field meaning is "do you want the cookie to be insecure?" and zero-value answers "no".

This is also a **breaking change on a stable surface**. Migration:

```go
// Before
Secure: true,

// After (no field — the default IS secure)
// — or —
InsecureCookie: false,  // explicit, equivalent to the default

// Before
Secure: false,  // intentional insecure-dev path

// After
InsecureCookie: true,
```

### Alternatives considered

- **Keep three independent breaking changes spaced across releases.** Rejected — three close-together breaking changes on `CSRFOptions` is more disruptive than one bundled change with a single migration note. The audit explicitly recommended bundling.
- **Add `Logger` as a method parameter (`NewCSRFMiddlewareWithLogger`).** Rejected — that splits the constructor surface and forces callers to choose between the new logger-aware constructor and the old one. A field on `CSRFOptions` keeps one canonical entry point.
- **Keep `EncryptionKey string` and convert at the boundary.** Rejected — the conversion adds nothing; raw key material is `[]byte` everywhere else in the stdlib (`crypto/aes`, `crypto/hmac`, `crypto/rand`) and in the rest of Nucleus (`pkg/auth` JWT keys, `pkg/secrets`).
- **Tri-state `Secure *bool`.** Rejected — pointers in option structs are awkward to read in YAML configs and force every direct constructor to use `ptrBool(true)` helpers. Inverting polarity is cleaner.
- **`Secure: false` default + a startup WARN.** Rejected — a WARN that fires on every restart in dev is log-spam; production users who set `Secure: true` silence it and then the WARN never warns the people who need it.

## Consequences

### Positive

- **Observability:** encrypt failures (server-side, real outage signal) and decrypt failures (public-endpoint noise, opt-in via log level) flow through the structured logger with the same redaction and attribute conventions as the rest of the app.
- **Security-by-default:** the zero-value `CSRFOptions{}` literal — the path most users take through `WithCSRF()` — now issues `Secure: true` cookies on all CSRF surface.
- **Type clarity:** `EncryptionKey []byte` matches the rest of the framework's key-material conventions.
- **One coordinated migration note** instead of three.

### Negative

- **Breaking change on `CSRFOptions`** in two places (`EncryptionKey` type, `Secure` → `InsecureCookie`). Documented under `BREAKING` in `CHANGELOG.md` with the one-line migration for each.
- **Contract baseline rebaseline:** removes `field:CSRFOptions.Secure`, adds `field:CSRFOptions.InsecureCookie` and `field:CSRFOptions.Logger`. `EncryptionKey` field name unchanged. Coordinated with `contract-guardian`.

### Neutral

- The `Logger *slog.Logger` field is optional; callers who omit it pick up `slog.Default()` exactly as if no logging plumbing existed.
- The `NewCSRFMiddleware` / `CSRFMiddleware` signature split from ADR-006 is unchanged.

## Compliance

After this ADR is accepted:

1. `pkg/router/csrf.go`:
   - `CSRFOptions.Logger *slog.Logger` exists; `defaults()` populates it with `slog.Default()` when nil.
   - `CSRFOptions.EncryptionKey` has type `[]byte`.
   - `CSRFOptions.Secure` is removed; `CSRFOptions.InsecureCookie bool` exists in its place.
   - `setCSRFCookie` and the XSRF cookie writer read `!opts.InsecureCookie` for the `Secure` flag.
   - `encryptToken` / `decryptToken` accept `[]byte` keys directly (no `[]byte(key)` wrapping).
   - Encrypt failures log at `WARN` with method, path, and error; XSRF-header decrypt failures log at `DEBUG`.
2. `pkg/router/middleware.go` `DefaultStack` plumbs its logger into the constructed `CSRFOptions`.
3. `contracts/baseline/api_exported_symbols.txt`:
   - removes `field:CSRFOptions.Secure`
   - adds `field:CSRFOptions.InsecureCookie`
   - adds `field:CSRFOptions.Logger`
   - keeps `field:CSRFOptions.EncryptionKey` (name unchanged, type tracked separately by the contract-firewall test set).
4. `docs/guides/CSRF_GUIDE.md` documents the new field types and the `InsecureCookie` opt-out path.
5. `CHANGELOG.md` under `Unreleased`:
   - `Added` — `CSRFOptions.Logger *slog.Logger`.
   - `BREAKING` — `CSRFOptions.EncryptionKey` is now `[]byte` (was `string`).
   - `BREAKING` — `CSRFOptions.Secure bool` replaced by `CSRFOptions.InsecureCookie bool` (polarity flipped; default is now secure).

## Related

- [`pkg/router/csrf.go`](../../pkg/router/csrf.go) — the middleware.
- ADR-006 — CSRF hardening (constant-time compare + mandatory key); this ADR is its direct follow-up.
- `docs/audits/2026-05-14-post-sprint-readiness.md` §5 risk 5, §7 task 5 — the audit recommending all three changes.
- `docs/guides/CSRF_GUIDE.md` — operator-facing CSRF documentation.
- SPEC.md §2 principle 4 — security-by-default, the framing this ADR makes concrete for the cookie `Secure` flag.
