# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

No active iteration. The CSRF hardening iteration is complete and
archived at `docs/iterations/2026-05-14-csrf-hardening.md` (PR #60).
Awaiting owner direction for the next iteration.

## Scope

- in: (TBD — owner to confirm from the queue below)
- out: (TBD)

## Acceptance criteria

- [ ] (TBD)

## Status

### Done

- **v0.7.0 released** (PRs #56–#59), iteration archived at
  `docs/iterations/2026-05-14-v0.7.0-release-and-es256.md`.
- **CSRF hardening** (PR #60, ADR-006) — constant-time comparison,
  mandatory `EncryptionKey`, `NewCSRFMiddleware`, defensive crypto
  fixes. Iteration archived at
  `docs/iterations/2026-05-14-csrf-hardening.md`.

### In progress

- (none)

### Blocked

- (none)

## Candidate next steps (priority order, pending owner confirmation)

1. **Secrets redaction in `slog`** — `slog.HandlerOptions.ReplaceAttr`
   that vacates `authorization` / `cookie` / `set-cookie` / `password` /
   `token` / `secret` / `api_key`. `pkg/observe/logger.go:26` configures
   the handler with `Level` only — no `ReplaceAttr` today. Audit §7
   item 6; the sibling security item to CSRF. Self-contained, high
   leverage, additive — likely fits one session with ADR + tests + docs.
2. **Live-DB integration tests for `AutoMigrate`** — Postgres/MySQL/MSSQL/
   Oracle. The dialect scaffolds shipped with string-match tests only;
   the `db-matrix-required` lane already brings up containers.
3. **Schema-level drift detection** — `information_schema` introspection
   vs migrations. The 2026-05-14 checksum drift is the file-level half.
4. **CSRF review follow-ups** — (a) plumb a logger into the CSRF
   middleware for encrypt/decrypt-error observability; (b) decide
   `CSRFOptions.EncryptionKey` `string` → `[]byte`; (c) `Secure: true`
   cookie default. From PR #60's review loop. Smaller than a full
   iteration — could pair with another item.
5. **`go mod tidy` unblock** — fix the `admin/proto` replace-directive
   issue so the AWS SDK modules carry correct `// direct` annotations.
6. **Phase 4 — AWS SDK opt-in** — build tag / plugin so `pkg/app` does
   not link the AWS SDK unconditionally (~3-5 MB).
7. **Future secret-manager resolvers** — GCP Secret Manager, Azure Key
   Vault, HashiCorp Vault. The `secrets.Resolver` interface is the seam.
8. **`tasks.Manager` struct→interface DEP** — optional DEP-2026-004 for
   the binary-incompatible type-identity change (contract-guardian
   advisory from the v0.7.0 release prep).
9. **503 path test for `/healthz`**, endpoints-parity doc-parsing,
   `pkg/health/{db,redis,storage}.go` individual tests — smaller audit
   §7 items.

## Files of interest

- `docs/iterations/2026-05-14-csrf-hardening.md` — archived CSRF iteration.
- `docs/iterations/2026-05-14-v0.7.0-release-and-es256.md` — prior archived iteration.
- `docs/audits/2026-05-14-post-sprint-readiness.md` — the audit driving the
  candidate-next-steps list.
- `pkg/observe/logger.go` — the `slog` handler that needs `ReplaceAttr`
  (candidate #1).
- `pkg/router/csrf.go` — CSRF middleware; the review follow-ups (#4) live here.

## Notes / decisions log

- 2026-05-14 — CSRF hardening iteration completed and merged (PR #60).
  No `DEP-` entry required (behaviour change on an existing symbol, not a
  removal); ADR-006 + CHANGELOG `BREAKING` note is the governance trail.
- 2026-05-14 — three CSRF review follow-ups deferred (logger, key type,
  Secure default) — see the archived iteration's follow-ups section.
