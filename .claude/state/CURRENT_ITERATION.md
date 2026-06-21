# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).
>
> Status: NO ACTIVE ITERATION — previous iteration complete as of 2026-06-21.
> Three follow-up chips are open; the next session should pick one.
>
> Previous iterations archived to:
>   docs/iterations/2026-06-18-runtime-jwt-accessor.md
>   docs/iterations/2026-06-18-fleetdesk-repin-rt-jwt.md
>   docs/iterations/2026-06-18-openapi-security-schemes.md
>   docs/iterations/2026-06-18-router-with-per-route-middleware.md
>   docs/iterations/2026-06-18-csrf-session-key-and-fleetdesk-adoption.md
>   docs/iterations/2026-06-19-authz-ssr-denial-handler.md
>   docs/iterations/2026-06-19-authz-subject-action-resolvers.md
>   docs/iterations/2026-06-20-orbit-extraction-foundation.md
>   docs/iterations/2026-06-21-admin-orbit-extraction-clean-break.md

## Goal

(none active — select a follow-up chip below and fill this section in)

## Candidate next iterations (follow-up chips)

### task_b8cbc177 — Docusaurus site update for admin→orbit break (RECOMMENDED)

Update `website/docs/**` to reflect that `pkg/admin` and all `admin_*` config
keys are gone from nucleus, and that the admin functionality now lives in the
separate `orbit` module. The advisory CI check "Website Docs Drift" is currently
failing on main because of this drift; this task clears it.

Subagent: `website-curator` (mandatory — never edit `website/docs/**` without it).

Scope:
- Remove or update all pages that reference `app.MountAdmin()`, `App.Admin`,
  `AdminAgentConfig`, `admin_prefix`, `admin_title`, `admin_rbac_policy_file`,
  or `GET /_/config`.
- Add or update an orbit integration guide pointing to `github.com/jcsvwinston/orbit`.
- Note the `admin_rbac_policy_file` → `rbac_policy_file` rename (DEP/MA-2026-004).
- Run `npm run build` in `website/` to verify no broken links.

### task_2e0651af — Move orphaned nucleus CLI commands into orbit

The `nucleus createuser` and `nucleus changepassword` CLI commands, and the
top-level `admin/agent`, `admin/proto`, `admin/server` modules, remain in the
nucleus repo after the clean break but conceptually belong in orbit. Move them.

Subagents: `contract-guardian` (CLI surface change), `migration-assistant`
(deprecation path for the nucleus-side commands), `doc-updater`.

### task_6822ff25 — Grow orbit.Config (parity with old in-core admin)

The old in-core admin had config options for cluster mode, live reload, tracing,
and an auth-DB override. `orbit.Config` currently exposes only `Prefix`. Close
the parity gap by adding those options to `orbit.Config`.

Subagents: orbit-side work only (no nucleus contract surface touched).

---

## Acceptance criteria

(fill in when a chip is selected as the active iteration)

## Status

### Done

(nothing yet — new iteration not started)

### In progress

(nothing)

### Blocked

(none)

## Files of interest

- `website/docs/` — Docusaurus source (task_b8cbc177 target)
- `docs/adrs/ADR-019.md` — Accepted; reference for orbit integration docs
- `docs/migrations/DEP-MA-2026-004.md` — config key rename migration doc
- `docs/reference/CONFIG_KEY_REGISTRY.md` — updated in PR #155
- `CHANGELOG.md` — Unreleased section (update when iteration ships)
- `~/GolandProjects/orbit` — orbit repo root (HEAD `f68c64f`)
- `~/GolandProjects/orbit/go.mod` — nucleus pin `8714882`
- `internal/cli/` — CLI commands (task_2e0651af target)
- `docs/reference/CLI_CONTRACT_MATRIX.md` — (task_2e0651af: update on move)
