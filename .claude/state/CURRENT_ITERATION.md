# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

**CSRF hardening** — close the two CSRF security gaps from the 2026-05-14
audit §7: the non-constant-time token comparison (`pkg/router/csrf.go`,
the `submitted != token` check) and the silently-derived `EncryptionKey`
(`defaults()` hashes the cookie name into an AES key when the operator
leaves `EncryptionKey` empty — a globally-predictable key).

## Scope

- in:
  - Constant-time CSRF token comparison via `crypto/subtle`.
  - Remove the weak-key derivation from `CSRFOptions.defaults()`.
  - Mandatory, well-formed `EncryptionKey` (exactly 32 bytes for AES-256)
    whenever `EnableXSRFCookie` is true — fail loud at middleware
    construction, not silently at request time.
  - New additive `NewCSRFMiddleware(opts) (func(http.Handler) http.Handler, error)`
    constructor for callers who want graceful error handling;
    `CSRFMiddleware` becomes the `Must`-style wrapper that panics on a
    misconfiguration.
  - ADR-006 documenting the stable-surface behaviour change.
  - Tests, CSRF_GUIDE.md, CHANGELOG.
- out:
  - Wiring CSRF into `App.New` (it is opt-in today; that is a separate,
    larger design question).
  - The `Secure: false` cookie default (a separate hardening item).
  - Secrets redaction in `slog` (audit §7 item 6 — next iteration).

## Acceptance criteria

- [ ] Token comparison uses `crypto/subtle.ConstantTimeCompare`; no
      short-circuiting byte compare against the secret.
- [ ] `CSRFOptions.defaults()` no longer derives an `EncryptionKey` from
      the cookie name.
- [ ] `NewCSRFMiddleware` returns an error when `EnableXSRFCookie` is true
      and `EncryptionKey` is not exactly 32 bytes.
- [ ] `CSRFMiddleware` panics (at construction, like `regexp.MustCompile`)
      on the same misconfiguration — no silent weak-key path remains.
- [ ] `encryptToken` / `decryptToken` can no longer panic on a short key
      (`key[:32]` slice).
- [ ] ADR-006 cut; `CSRF_GUIDE.md` and `CHANGELOG.md` updated.
- [ ] Contract freeze green; `NewCSRFMiddleware` added to the baseline.
- [ ] `go test ./...` green.

## Status

### Done

- **v0.7.0 released** (PRs #56–#59). Tag at `ed5689b`. Full release-prep
  gates green; iteration archived at
  `docs/iterations/2026-05-14-v0.7.0-release-and-es256.md`.
- **CSRF hardening implemented** — constant-time compare, mandatory
  `EncryptionKey`, `NewCSRFMiddleware`, ADR-006, tests, docs. Iteration
  review loop ran: architect PASS (1 WARN), code-review NITS, security
  PASS (2 LOW), contract-guardian PASS — no blockers. In-scope review
  fixes applied (dead `OriginOnly` branch, X-XSRF header guard, comment
  accuracy, tamper tests).

### In progress

- CSRF hardening — committing + PR.

### Blocked

- (none)

### Review follow-ups (deferred — out of this iteration's scope)

- **CSRF middleware has no logger.** `encryptToken` / `decryptToken`
  errors are silently swallowed (security outcome is still correct — a
  failed decrypt → empty `submitted` → rejected). Adding observability
  needs a logger plumbed into the middleware: either a new `Logger`
  field on `CSRFOptions` (a stable-surface addition) or a
  context-logger pattern. Separate small enhancement.
- **`CSRFOptions.EncryptionKey` is `string`, not `[]byte`** (architect
  WARN). The `string` type couples the 32-byte invariant to text
  encoding — an operator who base64-encodes a 32-byte secret passes a
  44-char string and hits the validator confusingly. `CSRF_GUIDE.md`
  documents the raw-bytes requirement; changing the field type is a
  frozen-field contract break and deferred to a deliberate decision
  (pre-v1, no external users — low-risk window, but owner's call).
- **`Secure: false` cookie default** — pre-existing weak default,
  explicitly out of ADR-006's scope. CSRF/XSRF cookies ship without the
  `Secure` flag unless the operator sets it. Worth a follow-up hardening.

## Candidate next steps (priority order, pending owner confirmation)

1. **CSRF hardening** — `subtle.ConstantTimeCompare` for token comparison
   + mandatory `EncryptionKey` in production. Security gap from the
   2026-05-14 audit §7 (was the highest-leverage open item after the
   sprint). `pkg/router/csrf.go:184` (`!=`) and `:63-67` (key default
   from cookie-name hash).
2. **Secrets redaction in `slog`** — `slog.HandlerOptions.ReplaceAttr`
   that vacates `authorization` / `cookie` / `set-cookie` / `password` /
   `token` / `secret` / `api_key`. `pkg/observe/logger.go:26` has no
   `ReplaceAttr` today.
3. **Live-DB integration tests for `AutoMigrate`** — Postgres/MySQL/MSSQL/
   Oracle. The dialect scaffolds shipped with string-match tests only;
   the `db-matrix-required` lane already brings up containers.
4. **Schema-level drift detection** — `information_schema` introspection
   vs migrations. The 2026-05-14 checksum drift is the file-level half.
5. **`go mod tidy` unblock** — fix the `admin/proto` replace-directive
   issue so the AWS SDK modules carry correct `// direct` annotations.
6. **Phase 4 — AWS SDK opt-in** — build tag / plugin so `pkg/app` does
   not link the AWS SDK unconditionally (~3-5 MB).
7. **Future secret-manager resolvers** — GCP Secret Manager, Azure Key
   Vault, HashiCorp Vault. The `secrets.Resolver` interface is the seam.
8. **`tasks.Manager` struct→interface DEP** — optional DEP-2026-004 for
   the binary-incompatible type-identity change (contract-guardian advisory).
9. **503 path test for `/healthz`**, endpoints-parity doc-parsing,
   `pkg/health/{db,redis,storage}.go` individual tests — smaller audit
   §7 items.

## Files of interest

- `docs/iterations/2026-05-14-v0.7.0-release-and-es256.md` — archived iteration.
- `docs/audits/2026-05-14-post-sprint-readiness.md` — the audit driving the
  candidate-next-steps list.
- `docs/adrs/ADR-005-es256-and-aws-secrets-manager.md` — ES256 + SM design;
  names the deferred resolver work.
- `pkg/auth/secrets/` — the resolver package; extend here for new providers.
- `docs/reports/dependency_impact_aws_sdk_2026-05-14.md` — AWS SDK review
  with the two follow-up notes.

## Notes / decisions log

- 2026-05-14 — Iteration executed autonomously. Owner approved all three
  parked decisions (v0.7.0 tag, ES256+SM MVP scope, stability drill),
  asked one at a time. v0.7.0 tag was cut early then moved to the #57
  merge commit with owner approval.
- 2026-05-14 — `panic(` count 4→0 since `b1e497e` is unconfirmed
  (incidental size-delta-agent finding). Worth a confirmation pass.
- 2026-05-14 — Stale remote branch `claude/interesting-ishizaka-d51a45`
  (pre-#56 history) was never force-updated — classifier blocked the
  force-push. Cosmetic; content is squash-merged into main. Release and
  feature work went through fresh branches.
