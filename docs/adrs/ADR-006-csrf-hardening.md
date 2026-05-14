# ADR-006: CSRF Hardening — Constant-Time Comparison and Mandatory Encryption Key

**Status:** Accepted
**Date:** 2026-05-14
**Superseded:** No

## Context

The 2026-05-14 post-sprint readiness audit (`docs/audits/2026-05-14-post-sprint-readiness.md` §5, §7) flagged two real security defects in `pkg/router/csrf.go`:

1. **Non-constant-time token comparison.** The middleware compared the submitted token against the expected token with `submitted != token` — a byte-by-byte string comparison that short-circuits on the first mismatched byte. The wall-clock time of the comparison leaks how many leading bytes the attacker guessed correctly, which is the classic input for a timing attack against a secret.

2. **Silently-derived `EncryptionKey`.** `CSRFOptions.defaults()` filled an empty `EncryptionKey` with `sha256(CookieName)`. The `EncryptionKey` is the AES-256 key used to encrypt the `XSRF-TOKEN` cookie when `EnableXSRFCookie` is set. Deriving it from the cookie name — a value that is public, defaults to the constant `"_csrf"`, and is frequently left at its default — means every deployment that enables the XSRF cookie without explicitly setting a key ends up with a **globally-predictable encryption key**. An attacker can decrypt and forge the `XSRF-TOKEN` cookie offline.

   The defect compounds: `encryptToken` / `decryptToken` slice the key with `key[:32]`. A user-supplied key shorter than 32 bytes panics at *request time* (a denial-of-service from a config typo); a key longer than 32 bytes is silently truncated (the operator believes they configured AES-256 with their full key, but only the first 32 bytes are used).

`CSRFMiddleware`, `CSRFOptions`, and every `CSRFOptions` field are on the **stable** contract surface (`contracts/baseline/api_exported_symbols.txt`). The middleware is **opt-in** — it is not wired into `App.New`, so an operator constructs `CSRFOptions` and calls `CSRFMiddleware` directly. That rules out fixing the second defect with a config-validation hook in `pkg/app`; the enforcement has to live in `pkg/router` itself.

The audit anticipated this ADR: §7 item 5 said "accompany with an ADR if the decision breaks `WithoutDefaults()` paths." `WithoutDefaults()` is not actually in scope (CSRF is not an `App.New` default), but the principle holds — this is a deliberate behaviour change on a stable surface and is recorded here.

## Decision

### 1. Constant-time comparison

The token comparison switches to `crypto/subtle.ConstantTimeCompare`. The pre-check `submitted == ""` stays as a non-constant-time guard — it only tests whether the client sent *anything*, not whether they sent the *right thing*, so it leaks nothing about the secret. The secret-bearing comparison is constant-time.

### 2. Mandatory, well-formed `EncryptionKey`

`CSRFOptions.defaults()` no longer derives a key. The weak `sha256(CookieName)` fallback is removed outright.

`EncryptionKey` is validated at **middleware construction time**, not request time:

- When `EnableXSRFCookie` is `false`, `EncryptionKey` is unused and unvalidated — operators who do not use the JS-framework XSRF cookie are not forced to configure a key.
- When `EnableXSRFCookie` is `true`, `EncryptionKey` MUST be exactly 32 bytes (AES-256). Anything else — empty, short, long — is a configuration error.

### 3. Two constructors — `NewCSRFMiddleware` (additive) and `CSRFMiddleware` (`Must`-style)

`CSRFMiddleware` cannot grow an `error` return without breaking the frozen signature. The Go-idiomatic resolution is the `Must` split:

- **New, additive:** `NewCSRFMiddleware(opts CSRFOptions) (func(http.Handler) http.Handler, error)` — validates the options and returns an error on misconfiguration. This is the constructor for callers who want to handle the error gracefully (surface it through their own config validation, fall back, etc.).
- **Existing, unchanged signature:** `CSRFMiddleware(opts CSRFOptions) func(http.Handler) http.Handler` — now delegates to `NewCSRFMiddleware` and **panics** on error, exactly like `regexp.MustCompile`. The panic fires once, at application startup / middleware-chain construction, never on the request path. A misconfigured CSRF key is a programming/deployment error that should crash the process immediately and loudly, not degrade silently.

This keeps the contract baseline intact (no removed or renamed symbol; `NewCSRFMiddleware` is purely additive) while eliminating the silent weak-key path.

### Alternatives considered

- **Change `CSRFMiddleware` to return an error.** Rejected — breaks the frozen `stable` signature; would need a deprecation cycle for no benefit over the `Must` split.
- **Keep deriving a key, but from a better source (e.g. random per process).** Rejected — a per-process random key makes the `XSRF-TOKEN` cookie un-decryptable across a multi-replica deployment and across restarts. The key must be operator-supplied and stable; the only correct move is to require it.
- **Silently disable `EnableXSRFCookie` when no key is set, with a WARN.** Rejected — silently dropping a security feature the operator explicitly asked for is the same class of bug as the weak default. Fail loud.

## Consequences

### Positive

- The timing side-channel on the CSRF token is closed.
- No deployment can run with a predictable CSRF encryption key. The failure mode for a missing/short/long key moves from "silent vulnerability" or "request-time panic" to "loud, immediate, startup-time error" — the operator finds out before the app serves a single request.
- `NewCSRFMiddleware` gives error-aware callers (and the framework itself, if CSRF is ever wired into `App.New`) a non-panicking path.
- The contract baseline is untouched except for one additive symbol.

### Negative

- **Behaviour change on a stable surface.** An existing application that called `CSRFMiddleware` with `EnableXSRFCookie: true` and no `EncryptionKey` previously started successfully (with a weak key); it now panics at startup. This is intentional — that application had no real XSRF-cookie security — but it is a hard failure on upgrade. It is documented in `CHANGELOG.md` under a `BREAKING` note with the one-line fix (set a 32-byte `EncryptionKey`).
- Applications passing a non-32-byte `EncryptionKey` (previously: silent truncation or request-time panic) now fail at startup. Same intentional "fail loud" trade-off.
- Operators who want the XSRF cookie must now generate and manage a 32-byte key. `CSRF_GUIDE.md` documents the generation command.

### Neutral

- `EnableXSRFCookie: false` deployments — the common case — see no behaviour change at all. `EncryptionKey` stays optional and unvalidated for them.
- The `Secure: false` cookie default is a separate weak-default concern and is explicitly out of scope for this ADR.

## Compliance

After this ADR is accepted:

1. `pkg/router/csrf.go` compares tokens with `crypto/subtle.ConstantTimeCompare`.
2. `CSRFOptions.defaults()` does not populate `EncryptionKey`.
3. `NewCSRFMiddleware` exists, validates `EncryptionKey` (32 bytes iff `EnableXSRFCookie`), and returns an error on misconfiguration.
4. `CSRFMiddleware` delegates to `NewCSRFMiddleware` and panics on error; its signature is unchanged.
5. `encryptToken` / `decryptToken` cannot panic on a short key — construction-time validation guarantees a 32-byte key reaches them, and they are defensive regardless.
6. `NewCSRFMiddleware` is added to `contracts/baseline/api_exported_symbols.txt`.
7. `docs/guides/CSRF_GUIDE.md` documents the mandatory key, the 32-byte requirement, a key-generation command, and the `NewCSRFMiddleware` vs `CSRFMiddleware` choice.
8. `CHANGELOG.md` records the constant-time fix under `Security` and the mandatory-key behaviour change under a `BREAKING` note.

## Related

- [`pkg/router/csrf.go`](../../pkg/router/csrf.go) — the middleware.
- `docs/audits/2026-05-14-post-sprint-readiness.md` §5 risk 5, §7 item 5 — the audit findings this ADR acts on.
- `docs/guides/CSRF_GUIDE.md` — operator-facing CSRF documentation.
- ADR-001: stdlib-first runtime — `crypto/subtle` is the stdlib answer; no dependency added.
- `SPEC.md` §"security-by-default" — the framing this ADR makes concrete for CSRF.
