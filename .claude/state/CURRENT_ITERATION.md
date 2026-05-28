# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

<one-sentence goal of the active iteration>

## Scope

- in: …
- out: …

## Acceptance criteria

- [ ] …
- [ ] …
- [ ] …

## Status

### Done
- (none yet)

### In progress
- …

### Blocked
- (none)

## Files of interest

- pkg/…
- internal/cli/…
- docs/…

## Notes / decisions log

- YYYY-MM-DD — …

---

## Carry-forward backlog (from 2026-05-28-cut-v0.8.0)

### Framework bugs (P1/P2)

- **P1 — `WithoutDefaults()` leaks the admin bootstrap user.**
  `pkg/app/app.go:~272` calls `admin.EnsureBootstrapAdminUser`
  UNCONDITIONALLY, before the `if !o.skipDefaults` guard. Fix: move call
  inside the `!o.skipDefaults` branch.

- **P2 — `Router.Resource("")` under a module `Prefix` panics at startup.**
  `pkg/nucleus/router.go` — `joinPath` should yield `/` (not `""`) when
  prefix+path are both empty.

### ADR-010 §2 layer 5 (module-specific config binding/validation)

Completes the five-layer validator; layer 4 (referential) shipped 2026-05-26.

### Governance (escalated)

- **Branch protection / required gate is NOT enforced on `main`.** Direct
  pushes bypass the gate; main was pushed directly twice in the v0.8.0 session
  (release prep + workflow fix). Red CI can still land on main. Must be fixed
  before the next regression.

### Other carry-forward

- Cloud Secrets Provider plugin extraction (AWS/GCP/Azure/Vault out of core).
- SchemaDrift column-type comparison + `docs/guides/MODELING_MULTI_DATABASE.md`.
- `go mod tidy` unblock — resolve the `admin/proto` replace-directive.
- `tasks.Manager` struct→interface DEP (optional DEP-2026-004).
- Audit §7 minors: 503 path test for `/healthz`; endpoints-parity doc-parsing;
  `pkg/health/{db,redis,storage}.go` tests.

### Deferred carry-forwards (not blocking)

_Oracle multi-block AutoMigrate (2026-05-24):_
- Route admin-bootstrap PL/SQL through `db.ExecScript`. (architect NIT.)
- Oracle DDL auto-commit vs the Migrator transaction.

_session_cookie_secure (2026-05-23):_
- Startup validation: `SameSite=None` requires `Secure=true`. (security-auditor
  LOW.) `pkg/auth/session.go` should reject the combo.

_Oracle identifier-casing (2026-05-23):_
- CI governance reconciliation (mssql + oracle): required vs exploratory.
- Oracle reserved-word + dotted-identifier hardening.

_Phase 3b / observability (2026-05-22/23):_
- GCS credential-redaction forward-compat.
- Reverse-proxy hardening note for `/_/config`.
- Relocate `pkg/observability` to `internal/` post-v1.0.
- `Runtime.AutoMigrate` production guard.

_ADR-010 Phase 1 (still open):_
- Service-shutdown timeout — `nucleus.Run`'s `wg.Wait()` has no deadline.
- `Lifecycle.OnShutdown` context deadline — no bound.
- `joinPath` double-slash collapse — `routerAdapter.joinPath`.

_Internal-docs (low-priority):_
- `DETAILED_TUTORIAL.md` flat-handler style predates `nucleus.Module` pattern.
- `DEVELOPER_MANUAL.md §5.3` references `internal/contracts`.
