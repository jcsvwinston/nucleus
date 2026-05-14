# Iteration — CSRF Hardening

> Archived: 2026-05-14
> Branch: main
> PR: #60 (merged as 643aee7)
> ADR: ADR-006
> Status: COMPLETE — merged, all acceptance criteria met.

---

## Goal

Close the two CSRF security defects flagged by the 2026-05-14 post-sprint
readiness audit §5 / §7:

1. Non-constant-time CSRF token comparison (`submitted != token`) — a
   timing side-channel.
2. `CSRFOptions.defaults()` deriving the AES `EncryptionKey` from
   `sha256(CookieName)` — a globally-predictable key that made the
   `XSRF-TOKEN` cookie forgeable.

This was the highest-leverage open security item after the v0.7.0
release.

---

## What shipped (PR #60)

- **Constant-time comparison** — `crypto/subtle.ConstantTimeCompare`
  replaces the short-circuiting `!=`.
- **Weak-key derivation removed** — `defaults()` no longer touches
  `EncryptionKey`. When `EnableXSRFCookie` is true the key is mandatory
  and must be exactly 32 bytes (AES-256), validated at construction.
- **`NewCSRFMiddleware`** — new additive, error-returning constructor
  (`(func(http.Handler) http.Handler, error)`), returns
  `ErrCSRFEncryptionKey` on misconfiguration. `CSRFMiddleware` keeps its
  signature and becomes the `regexp.MustCompile`-style wrapper that
  panics at construction time.
- **Defensive crypto fixes** — `encryptToken`/`decryptToken` pass the
  key to `aes.NewCipher` (no more `key[:32]` panic / silent truncation);
  a too-short ciphertext now returns a real error instead of
  `("", nil)`; `generateCSRFToken` panics on a `crypto/rand` failure
  rather than issuing a low-entropy token.
- **`X-XSRF-TOKEN` header** is only read when `EnableXSRFCookie` is true.
- **ADR-006** cut; `CSRF_GUIDE.md` and `CHANGELOG.md` updated; contract
  baseline gained `NewCSRFMiddleware` + `ErrCSRFEncryptionKey`.

## Acceptance criteria — all met

- [x] Constant-time comparison via `crypto/subtle`.
- [x] `defaults()` no longer derives `EncryptionKey`.
- [x] `NewCSRFMiddleware` returns an error on a bad/missing key.
- [x] `CSRFMiddleware` panics at construction on the same misconfig.
- [x] `encryptToken`/`decryptToken` cannot panic on a short key.
- [x] ADR-006 cut; `CSRF_GUIDE.md` + `CHANGELOG.md` updated.
- [x] Contract freeze green; baseline updated.
- [x] `go test ./...` green.

## Review loop

architect-reviewer PASS (1 WARN), code-reviewer NITS, security-auditor
PASS (2 LOW), contract-guardian PASS — no blockers. In-scope review
fixes were applied in the same PR: removed the dead `OriginOnly`
status-code branch, guarded the `X-XSRF-TOKEN` header read, corrected an
inaccurate `EnableOriginCheck` godoc comment, added tamper-rejection
tests.

## Notes / decisions log

- 2026-05-14 — contract-guardian confirmed no `DEP-` entry is required:
  the panic-on-misconfiguration change is a behaviour change on an
  existing symbol, not a removal or rename. ADR-006 + a `CHANGELOG`
  `BREAKING` note is the correct governance trail for a pre-v1.0
  security-driven behaviour change.
- 2026-05-14 — chose the `Must`-split (`CSRFMiddleware` panics,
  `NewCSRFMiddleware` returns an error) over changing `CSRFMiddleware`'s
  frozen signature. Panic fires at construction / middleware-chain
  assembly, never on the request path.

## Follow-ups carried into future iterations

1. **CSRF middleware has no logger.** `encryptToken` / `decryptToken`
   errors are silently swallowed (the security outcome is still correct
   — a failed decrypt leaves `submitted` empty and the request is
   rejected — but there is zero operational observability). Adding a
   logger needs either a new `Logger` field on `CSRFOptions` (a
   stable-surface addition) or a context-logger pattern.
2. **`CSRFOptions.EncryptionKey` is `string`, not `[]byte`** (architect
   WARN). The `string` type couples the 32-byte invariant to text
   encoding. Changing the field type is a frozen-field contract break —
   deferred to a deliberate owner decision (pre-v1, no external users —
   a low-risk window, but the owner's call). `CSRF_GUIDE.md` documents
   the raw-bytes requirement in the meantime.
3. **`Secure: false` cookie default** — pre-existing weak default,
   explicitly out of ADR-006's scope. CSRF / XSRF cookies ship without
   the `Secure` flag unless the operator sets it.
