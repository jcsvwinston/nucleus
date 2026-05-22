# Iteration archive — 2026-05-22 ADR-010 Phase 3a (effective-config inspection)

> Archived 2026-05-22 as part of the session-end `/handoff`. Landed as the
> feature commit `7a416ce` (`feat(nucleus): effective-config inspection
> (ADR-010 Phase 3a)`) on `main`, followed by the usual `chore(state): close`
> state commit. ADR-010 Phase 3 was split: 3a (this iteration) ships the
> tooling; the `/_/config` endpoint is Phase 3b and env/file:line provenance
> is Phase 3.1.

## Goal

Ship the CLI/API half of ADR-010 Phase 3 (compliance #6, #13): a way to see
the effective merged configuration with each value's source, redacted.

## Scope

### In
- `pkg/nucleus/config.go`: `loadFromFiles` refactored into a thin wrapper over
  a new `loadMerged(paths, opts) (*koanf.Koanf, map[string]ConfigSource, error)`
  that tracks per-key provenance by snapshot-and-diff (default vs the file
  that last changed a key; a `null` revert points back at the default).
  New STABLE `pkg/nucleus` API: `LoadEffective(paths, extraKeys...)`,
  `ConfigSource{Kind, Path}`, `EffectiveValue{Key, Value, Source, Redacted}`,
  `EffectiveConfig{Values}`. Redaction reuses the canonical
  `observe.DefaultRedactedKeys()` plus a parent-aware rule mapping
  `databases.<alias>.url`/`.dsn` onto canonical entries (no second list).
- `internal/cli/configcommands.go` + `root.go`: new `config print --effective`
  command (repeatable `--config`, `--json`; text format `key = value [kind:path]`).
- `pkg/observe/redact.go`: canonical set extended with the framework's compound
  secret keys (`jwt_secret`, `admin_bootstrap_password`, `admin_cluster_token`,
  `session_redis_url`, `admin_cluster_redis_url`, `secret_access_key`,
  `account_key`) — the Phase 3a security fix.
- Freeze baseline `api_exported_symbols.txt` rebaselined additively (+11).
- Docs: ADR-010 §Phase 3, CHANGELOG, CLI_CONTRACT_MATRIX (`config` =
  `transitional`), API_CONTRACT_INVENTORY, CLI_BEST_PRACTICES, website
  cli/overview + concepts/configuration.

### Out (deferred)
- **Phase 3b:** auth-gated `/_/config` runtime endpoint (compliance #12).
- **Phase 3.1:** env/flag/programmatic-layer source attribution and `file:line`
  provenance.

## Owner-confirmed scope decisions
- Provenance = source-kind + path only (env layer not wired into the nucleus
  loader path; `file:line` not emitted) → both deferred to Phase 3.1.
- Gate the future `/_/config` on the admin subsystem being active
  (`App.Admin != nil`), NOT a `WithAdmin()` toggle (which the ADR §5/#12
  assume but which does not exist in the codebase).

## Acceptance criteria — all met
- [x] `go test ./...` green; `gofmt`/`go vet` clean; contract-freeze script and
  website drift guard (`--strict`) pass.
- [x] Behaviour-preserving loader refactor (existing Phase 2 tests untouched).
- [x] Additive freeze rebaseline (+11 `pkg/nucleus`, zero removals).
- [x] Loop: architect WARN→addressed, code-reviewer NITS, security-auditor
  WARN→**fixed**, contract-guardian WARN→addressed, test-runner PASS.

## The security fix (loop finding)
The security-auditor found that flat compound secret keys — `jwt_secret`
(the JWT signing key!), `admin_bootstrap_password`, `admin_cluster_token`,
`session_redis_url`, `admin_cluster_redis_url`, and the nested
`storage.s3.secret_access_key` / `storage.azure.account_key` — would print in
**cleartext** because their leaf segment is not an exact match for the short
atomic canonical keys (`secret`, `password`, `token`). Fixed by extending the
canonical `observe.DefaultRedactedKeys()` set (one surface; benefits log
redaction too). MED on the local CLI today; would have been HIGH once the
`/_/config` HTTP endpoint ships in 3b — caught before that.

## Outcome
Landed as feature commit `7a416ce`. Operators get `nucleus config print
--effective`; the framework gets a reusable `LoadEffective` API that Phase 3b's
endpoint will reuse.

## Follow-ups opened
- **Phase 3b** — `/_/config` endpoint: mount from the nucleus layer onto
  `App.Router`, gate on `App.Admin != nil`, wrap with
  `admin.NewDatabaseAdminAuth(App.DefaultDB(), App.Session, App.Config.AdminPrefix)`;
  thread the effective snapshot from the builder into `Run`; integration tests.
- **Phase 3.1** — env-layer attribution + `file:line` provenance.
- When `config`'s surface stabilises (after 3b), add it to the
  `cli_primary_commands.txt` freeze baseline (left `transitional` for now).
