# Iteration Archive — Admin→orbit extraction: follow-ups complete (ADR-019)

> Archived: 2026-06-21.
> Status: COMPLETE. All three follow-up chips resolved in this session.
> Preceded by: docs/iterations/2026-06-21-admin-orbit-extraction-clean-break.md
>   (clean-break iteration: nucleus `pkg/admin` removed, orbit SPA embedded,
>   ADR-019 Accepted — three follow-up chips were left open at that close).

## Goal

Complete the three follow-up chips that were explicitly out-of-scope for the
clean-break iteration: (1) update the public Docusaurus site to reflect the
admin→orbit split, clearing the failing advisory CI check; (2) grow
`orbit.Config` to close the parity gap with the old in-core admin config; (3)
re-home the orphaned `nucleus createuser`/`changepassword` CLI commands and the
`admin/{proto,agent,server}` modules into orbit, leaving the CLI commands
guarded in nucleus. Together these bring the admin→orbit extraction (ADR-019)
to 100% completion.

## Acceptance criteria (all met)

- [x] `website/docs/**` updated: admin presented as the separate orbit module;
      `/_/config` endpoint and `admin_*` config keys removed from site; orbit
      framed as forward-looking (not yet public); "Website Docs Drift" advisory
      CI check GREEN.
- [x] `orbit.Config` gains `AuthDatabase`, `LiveExcludePatterns`,
      `Cluster{Enabled,RedisURL,Channel,NodeID,Token}`, `TraceURLTemplate`;
      `resolveAuthDB` returns the auth DB's `*sql.DB` + dialect; latent bug
      fixed (bootstrap user had used the default handle's dialect);
      `EnableLiveClusterRelay()` called when `ClusterEnabled` (fail-open).
- [x] `nucleus createuser`/`changepassword` no longer auto-create
      `nucleus_admin_users`; they guard on orbit schema pre-existing
      (dialect-aware `internal/cli/orbit_guard.go`) and error pointing to
      orbit when the schema is absent; commands intentionally remain in nucleus.
- [x] `admin/{proto,agent,server}` Go modules + `admin/ui` relocated INTO orbit
      as `orbit/{proto,agent,server}` + `orbit/ui`; go.work + module-path
      rewrites applied in orbit repo; `agent.ExtensionConfig` replaces the
      removed `app.AdminAgentConfig`.
- [x] nucleus `admin/` directory, `go.work`/`go.work.sum`, proto/UI Makefile
      targets, `Admin Observability Skeleton` CI job, and
      `scripts/gen-dev-certs.sh` removed from nucleus; governance docs,
      godoc, `docs/ADMIN_UI.md`, ADR-018/019 notes cleaned up.
- [x] Full CI (CI Required Gate) green for every nucleus-touching PR.
- [x] ADR-019 Status = Accepted; no open follow-up chips remain.

---

## What shipped in this session

### Follow-up 1 — Public Docusaurus site update

**nucleus PR #157** (`0a0af8a`, squash-merged to main):

- `website/docs/**` updated: admin functionality presented as the separate
  `orbit` module; `/_/config` endpoint and all `admin_*` config keys removed
  from site content; orbit framed as forward-looking (private, not yet
  `go get`-able); orbit integration guide added pointing to
  `github.com/jcsvwinston/orbit`.
- "Website Docs Drift" advisory CI check: GREEN after this PR.

Subagent: `website-curator`.

---

### Follow-up 2 — orbit.Config parity

**orbit commit `f3d19cc`** (orbit private repo):

- `orbit.Config` extended: `AuthDatabase`, `LiveExcludePatterns`,
  `Cluster{Enabled,RedisURL,Channel,NodeID,Token}`, `TraceURLTemplate`.
- `resolveAuthDB` rewritten to return the auth DB's `*sql.DB` + dialect
  (fixes latent bug: bootstrap user previously used the default handle's
  dialect, causing schema mismatch in multi-DB setups).
- `EnableLiveClusterRelay()` called when `ClusterEnabled`; failure is
  fail-open (log + continue) so a missing Redis does not crash startup.

orbit HEAD after this commit: `f3d19cc`.

---

### Follow-up 3a — CLI orbit-availability guard

**nucleus PR #158** (`c44ac69`, squash-merged to main):

- `internal/cli/orbit_guard.go` added: dialect-aware check that the orbit
  schema (`nucleus_admin_users` table) pre-exists before `createuser` or
  `changepassword` proceeds.
- `nucleus createuser`/`changepassword` no longer auto-create the table; they
  surface a clear error pointing the operator to orbit when the schema is
  absent.
- Owner decision: commands intentionally remain in nucleus, only effective when
  orbit is installed.

Subagents: `contract-guardian` (CLI surface), `code-reviewer`, `security-auditor`.

---

### Follow-up 3b — Cluster-agent relocation + nucleus cleanup

**orbit commit `59f2e59`** (orbit private repo):

- `admin/{proto,agent,server}` Go modules + `admin/ui` relocated INTO orbit as
  `orbit/{proto,agent,server}` + `orbit/ui`.
- go.work updated; module paths rewritten throughout.
- `agent.ExtensionConfig` replaces the removed `app.AdminAgentConfig`.

orbit HEAD after this commit: `59f2e59`.

**nucleus PR #159** (`133359e`, squash-merged to main):

- Removed from nucleus: `admin/` directory, `go.work`/`go.work.sum`, proto/UI
  Makefile targets, `Admin Observability Skeleton` CI job,
  `scripts/gen-dev-certs.sh`.
- Cleaned up: governance docs, godoc, `docs/ADMIN_UI.md`, ADR-018/019 notes
  updated to reflect full relocation.
- nucleus is now a SINGLE Go module with ZERO admin code.

---

## Final repository state at close

### nucleus

- HEAD: `133359e` on `main`
- Single Go module (`go.mod` only; `go.work` removed).
- No `admin/` directory, no `pkg/admin`, no `AdminAgentConfig`, no `go.work`.
- `nucleus createuser`/`changepassword` guarded via `internal/cli/orbit_guard.go`.
- ADR-019 Status: Accepted.
- Working tree: clean (3 intentionally-untracked `docs/audits/` files — never
  committed by policy).

### orbit (private repo `github.com/jcsvwinston/orbit`)

- HEAD: `59f2e59` on `main`
- Owns the entire admin product: panel + embedded SPA + cluster agent + proto
  + server + UI.
- `orbit.Config` has full parity with the old in-core admin config.
- orbit is PRIVATE; must be flipped to public before `go get
  github.com/jcsvwinston/orbit` works for consumers.

---

## Files of interest (post-completion)

- `docs/adrs/ADR-019.md` — Status: Accepted.
- `internal/cli/orbit_guard.go` — dialect-aware orbit schema guard (new in PR #158).
- `docs/reference/CLI_CONTRACT_MATRIX.md` — updated for createuser/changepassword
  guard behaviour.
- `~/GolandProjects/orbit` — orbit repo root (HEAD `59f2e59`).
- `~/GolandProjects/orbit/orbit/` — proto, agent, server packages (relocated).
- `~/GolandProjects/orbit/orbit/ui/` — SPA assets (relocated from admin/ui).
- `CHANGELOG.md` — Unreleased entries covering PRs #157, #158, #159.

---

## Standing notes carried forward

- nucleus main is PR-only (`enforce_admins=true`, required check "CI Required
  Gate" strict=true). Direct `git push origin main` is REJECTED.
- `govulncheck` pinned `@v1.3.0` in `.github/workflows/ci.yml`. Do NOT upgrade
  to `@latest`: `x/vuln v1.4.0` + `golang.org/x/tools v0.46.0` panics on
  `"ForEachElement called on type containing *types.TypeParam"` under Go 1.26.4
  generics. Unpin when x/tools publishes a fix.
- Go floor: `1.26.4` (set in `go.mod`; use `GOTOOLCHAIN=go1.26.4` locally).
- v0.9.0 is the latest published tag (2026-06-09). nucleus main is ahead; bump
  `defaultPinnedFrameworkVersion` in `internal/cli/new.go` after each new tag.
- orbit is a PRIVATE repo (`github.com/jcsvwinston/orbit`); local clone at
  `~/GolandProjects/orbit`. Flip to public before consumers can `go get` it.
- `nucleus createuser`/`changepassword` intentionally remain in nucleus, guarded
  on orbit schema presence; they are NOT being moved to orbit.
