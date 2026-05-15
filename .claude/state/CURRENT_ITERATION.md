# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

No active iteration. The structured-logger secret-redaction iteration is
complete and archived at
`docs/iterations/2026-05-14-slog-secret-redaction.md` (PR #62). Awaiting
owner direction for the next iteration.

## Scope

- in: (TBD — owner to confirm from the queue below)
- out: (TBD)

## Acceptance criteria

- [ ] (TBD)

## Status

### Done

- **v0.7.0 released** (PRs #56–#59), archived at
  `docs/iterations/2026-05-14-v0.7.0-release-and-es256.md`.
- **CSRF hardening** (PR #60, ADR-006), archived at
  `docs/iterations/2026-05-14-csrf-hardening.md`.
- **Structured-logger secret redaction** (PR #62, ADR-007), archived at
  `docs/iterations/2026-05-14-slog-secret-redaction.md`. Default-on
  redaction; `NewLoggerWithRedaction` + `RedactionConfig` for explicit
  control; `log_redact_extra_keys` config key (extend-only — no
  config-level disable). Two MED security follow-ups folded in
  (bootstrap-password → stderr; denylist expansion).

### In progress

- (none)

### Blocked

- (none)

## Candidate next steps (priority order, pending owner confirmation)

1. **Live-DB integration tests for `AutoMigrate`** —
   Postgres/MySQL/MSSQL/Oracle. The dialect scaffolds shipped with
   string-match tests only; the `db-matrix-required` lane already brings
   up containers. Audit §7 item 7. Self-contained and concrete.
2. **Schema-level drift detection** — `information_schema` introspection
   vs migrations. The 2026-05-14 checksum drift was the file-level half.
   Audit §7 item 8.
3. **CSRF review follow-ups** (from PR #60 review):
   (a) plumb a logger into the CSRF middleware for encrypt/decrypt-error
   observability;
   (b) decide `CSRFOptions.EncryptionKey` `string` → `[]byte`;
   (c) `Secure: true` cookie default. Smaller than a full iteration —
   could pair with another item.
4. **slog redaction review follow-ups** (from PR #62 review):
   (a) defensive slice-copy pass over `mergeDefaults`;
   (b) governance note on the extend-yes / disable-no config-split
   precedent;
   (c) allocation-free ASCII case-fold for mixed-case attr keys.
   Smaller still — fits a half-day.
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

- `docs/iterations/2026-05-14-slog-secret-redaction.md` — archived iteration.
- `docs/iterations/2026-05-14-csrf-hardening.md` — prior archived iteration.
- `docs/iterations/2026-05-14-v0.7.0-release-and-es256.md` — release archive.
- `docs/audits/2026-05-14-post-sprint-readiness.md` — the audit driving the
  candidate-next-steps list.
- `pkg/db/`, `pkg/model/migration_scaffold_*.go` — the AutoMigrate test
  surface (candidate #1).

## Notes / decisions log

- 2026-05-14 — three iterations shipped in a single day on top of v0.7.0
  (CSRF hardening, slog redaction, plus the v0.7.0 release prep). All
  three followed the same ADR + CHANGELOG-Changed governance trail for
  pre-v1.0 stable-surface behaviour changes; contract-guardian
  consistently confirmed no DEP entry needed for behaviour-only changes.
- 2026-05-14 — established the "extend = config-reachable, disable =
  code-only" precedent (ADR-007). Architect-reviewer noted this is worth
  generalising in `docs/governance/` if it surfaces in other subsystems.
