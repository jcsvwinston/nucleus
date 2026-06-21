# Iteration Archive — Admin→orbit extraction: clean break (ADR-019, complete)

> Archived: 2026-06-21.
> Status: COMPLETE. All acceptance criteria met.
> Preceded by: docs/iterations/2026-06-20-orbit-extraction-foundation.md
>   (nucleus-side prerequisites + orbit scaffold — Slices 1a/1b/1c +
>   Router.Mount + orbit health probe).

## Goal

Complete the admin→orbit extraction that began in the foundation iteration:
move the admin SPA and its backend logic from the nucleus core into
`github.com/jcsvwinston/orbit`, embed the SPA via `go:embed`, verify live
serving, self-register orbit's mount prefix in the RBAC default-deny list,
and then perform the clean break in nucleus — removing `pkg/admin`,
`App.Admin`, `App.MountAdmin()`, `App.RegisterAdminModels()`,
`AdminAgentConfig`, the `admin_*` config keys, and the `GET /_/config`
endpoint (ADR-010 Phase 3b). Close fleetdesk finding #9. Flip ADR-019
from Proposed to Accepted.

## Acceptance criteria (all met)

- [x] orbit module builds and passes `go test ./...` against nucleus post-clean-break HEAD.
- [x] SPA assets live in orbit under `ui/dist`; `go:embed all:ui/dist` wires them;
      serving confirmed live (1.27 MB Vite bundle delivered by a real `nucleus.Run` app).
- [x] Panel mounts via `r.Mount(prefix, panel.Handler())` using `Router.Mount`.
- [x] Panel construction uses only the Runtime accessor API (no direct import of
      nucleus internals).
- [x] `<prefix>/_orbit/health` probe responds 200 when mounted.
- [x] Bootstrap allow-list seeded in orbit; orbit self-registers its mount prefix
      in the framework's default-deny RBAC (reachable without `WithOpenAuthz`).
- [x] `go:embed` wires the SPA assets; fleetdesk finding #9 closed.
- [x] Nucleus breaking removals land with deprecation notice and migration doc
      (`contract-guardian` + `migration-assistant`). Config key rename
      `admin_rbac_policy_file` → `rbac_policy_file` tracked as DEP/MA-2026-004.
- [x] ADR-019 status flipped from Proposed to Accepted.
- [x] Full iteration loop green for each nucleus-touching PR; orbit `go test ./...` green.

---

## What shipped in this iteration

### orbit commits (private repo github.com/jcsvwinston/orbit)

| Commit    | Description |
|-----------|-------------|
| `9b4a078` | Embed React/Vite admin SPA via `go:embed all:ui/dist`; serve SPA shell + bundle; live-verified against a real `nucleus.Run` app. Closes fleetdesk finding #9. |
| `f6b2349` | orbit self-registers its mount prefix in the framework's default-deny RBAC; orbit routes reachable without `WithOpenAuthz`. |
| `f68c64f` | Bump nucleus pin to `v0.9.1-0.20260621031917-8714882cc7f9` (post-clean-break nucleus HEAD `8714882`); orbit builds + tests GREEN against nucleus without in-core admin. |

orbit HEAD at close: `f68c64f`
orbit nucleus pin at close: `v0.9.1-0.20260621031917-8714882cc7f9`

### nucleus PR #155 — clean break (`8714882`, squash-merged to main)

**Commit subject:** feat(nucleus): remove in-core admin, clean break
(ADR-019 Slice 2.4, PR #155)

**Removals (BREAKING, guarded by deprecation):**

- `pkg/admin` package — entirely removed.
- `App.Admin` field — removed.
- `App.MountAdmin()` method — removed.
- `App.RegisterAdminModels()` method — removed.
- `AdminAgentConfig` type — removed.
- All `admin_*` config keys — removed.
- `GET /_/config` endpoint — removed (ADR-010 Phase 3b).

**Config key rename (deprecated alias):**

- `admin_rbac_policy_file` → `rbac_policy_file`. Deprecated alias kept with
  startup WARN logged. Migration document: DEP/MA-2026-004.

**Contract baseline:** rebaselined in PR #155. Removals are intentional and
tracked; contract-guardian verdict: BREAKING-OK/deliberate (covered by
deprecation + migration doc).

**Full iteration loop ran for PR #155:**

- architect-reviewer — WARN → addressed (ADR-019 Accepted, removal justified).
- contract-guardian — BREAKING-OK (deprecation notice + DEP/MA-2026-004).
- security-auditor — PASS (no open surface remaining from removed endpoints).
- code-reviewer — PASS.
- changelog-writer — entry under Unreleased.
- doc-updater — internal guides updated for removal.
- migration-assistant — DEP/MA-2026-004 written.
- ADR-019 flipped to Accepted.

---

## Fleetdesk findings ledger at close

- **Finding #9** — CLOSED (SPA embedded in orbit; served live from
  `go:embed all:ui/dist`).
- Ledger: **23 FIXED / 11 OPEN** (net +1 fix from this iteration).

---

## Files of interest (post-clean-break nucleus)

- `docs/adrs/ADR-019.md` — status Accepted.
- `contracts/baseline/api_exported_symbols.txt` — rebaselined (removals recorded).
- `docs/migrations/DEP-MA-2026-004.md` — `admin_rbac_policy_file` → `rbac_policy_file`.
- `CHANGELOG.md` — Unreleased entries for PR #155.
- `docs/reference/API_CONTRACT_INVENTORY.md` — updated for removals.
- `docs/reference/CONFIG_KEY_REGISTRY.md` — `admin_*` keys removed; `rbac_policy_file` added.
- `~/GolandProjects/orbit` — orbit repo root.
- `~/GolandProjects/orbit/ui/dist` — embedded SPA assets.
- `~/GolandProjects/orbit/go.mod` — nucleus pin `8714882`.

---

## Open follow-up chips (spawned as background tasks, NOT part of this iteration)

These items were identified during the iteration but are explicitly out-of-scope
here. Each has a task chip ID for tracking.

| Task ID       | Description | Priority |
|---------------|-------------|----------|
| task_b8cbc177 | Update public Docusaurus site (`website/docs/**`) for the admin→orbit break. Advisory CI check "Website Docs Drift" currently failing on main (non-blocking). Invoke `website-curator` subagent. | HIGH — clears failing advisory CI check |
| task_2e0651af | Move orphaned nucleus CLI commands (`nucleus createuser`, `nucleus changepassword`) and top-level `admin/agent`, `admin/proto`, `admin/server` modules out of nucleus into orbit. | MEDIUM |
| task_6822ff25 | Grow `orbit.Config` to expose cluster/live/trace/auth-DB options currently missing (parity gap with the old in-core admin config). | MEDIUM |

**Recommended next session:** task_b8cbc177 (Docusaurus) — most visible, clears CI.

---

## Standing notes carried forward

- nucleus main is PR-only (`enforce_admins=true`, required check "CI Required Gate"
  strict=true). Direct `git push origin main` is REJECTED.
- `govulncheck` pinned `@v1.3.0` in `.github/workflows/ci.yml`. Do NOT upgrade to
  `@latest`: `x/vuln v1.4.0` + `golang.org/x/tools v0.46.0` panics on
  `"ForEachElement called on type containing *types.TypeParam"` under Go 1.26.4
  generics. Unpin when x/tools publishes a fix.
- Go floor: `1.26.4` (set in `go.mod`; use `GOTOOLCHAIN=go1.26.4` locally).
- v0.9.0 is the latest published tag (2026-06-09). nucleus main is ahead; any
  consumer of main pins a pseudoversion. Bump `defaultPinnedFrameworkVersion` in
  `internal/cli/new.go` after each new tag.
- orbit is a PRIVATE repo; local clone at `~/GolandProjects/orbit`.
