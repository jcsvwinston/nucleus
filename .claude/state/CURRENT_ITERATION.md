# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

No active iteration. The 2026-05-14 iteration — post-ADR-004 queue sweep,
v0.7.0 release, and the ES256 + AWS Secrets Manager MVP — is complete and
archived at `docs/iterations/2026-05-14-v0.7.0-release-and-es256.md`.
Awaiting owner direction for the next iteration.

## Scope

- in: (TBD — owner to confirm from the queue below)
- out: (TBD)

## Acceptance criteria

- [ ] (TBD)

## Status

### Done

- **v0.7.0 released.** Tag points at `ed5689b` (PR #57 merge). All
  release-prep gates green: contract freeze, compatibility harness (3/3),
  compatibility report (8/8), governance (release-strict), `go test ./...`.
- **Post-ADR-004 queue swept** (PR #56) — audit, Casbin CSV migrator +
  DEP/MA-2026-003, checksum drift detection, MSSQL/Oracle AutoMigrate
  scaffolds, ADR-004 E2E test, `pkg/storage` contract baseline, MAIL_GUIDE.
- **MSSQL/Oracle post-sprint drill** — 10/10 + 10/10, no regression.
- **ES256 + AWS Secrets Manager MVP** (PR #58, ADR-005) — ES256 (P-256)
  end to end, `pkg/auth/secrets` resolver package, AWS SDK behind a
  one-method interface, `dependency-impact` review recorded.

### In progress

- (none)

### Blocked

- (none)

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
