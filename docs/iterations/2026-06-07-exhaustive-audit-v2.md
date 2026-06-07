> Archived: 2026-06-07 (iteration complete; report landed via PR audit/2026-06-07-exhaustive-v2).

# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

Exhaustive audit v2 (2026-06-07): verify framework functionality and
documentation fidelity (internal `docs/*` + public `website/`) against the
code, and grade enterprise-class readiness. Deliverable: audit report.

## Scope

- in: full Go lane executed (build/vet/test/contracts/CLI on root module,
  SQLite); website `npm run build` + drift guard; §9 verification of all 24
  internal docs + 15 website pages; security + governance posture; enterprise
  scorecard. Report at `docs/audits/2026-06-07-exhaustive-audit-v2.md`.
- out: code/doc fixes (roadmap only); `admin/*` sub-modules and DB-matrix
  engines (ci-delegated); benchmarks; pentest.

## Acceptance criteria

- [x] Go build/vet/test executed and graded first-hand.
- [x] Contract freeze + firewall + harness executed.
- [x] CLI smoke-tested end-to-end.
- [x] §9 verification of internal docs and website body content.
- [x] Website build + drift guard run.
- [x] Enterprise scorecard with prioritized roadmap.
- [x] Report written; supersedes the uncommitted scheduled-task draft.
- [ ] Report landed on `main` via PR (pending: no gh/creds in sandbox).

## Status

### Done
- All six audit lanes complete; report at
  docs/audits/2026-06-07-exhaustive-audit-v2.md.
- Functional lane GREEN on root module / SQLite (build rc=0, vet rc=0, suite
  passing except 3 network-only scaffold build-smoke tests).
- Contracts: freeze + firewall PASS; harness READY 100% (core-build only).
- CLI: version/help/new/generate/doctor/migrate/config/routes/openapi all rc=0.

### In progress
- Landing the report via PR (branch prepared; push + gh pr create by Carlos).

### Blocked
- (none)

## Key findings (severity-ordered)

- F-3 P1 (P0 for required engine) - pkg/model/crud.go uses `?` on raw *sql.DB,
  no per-dialect rebind; pgx needs $N; CRUD tests SQLite-only mask PG/Oracle
  breakage. Repo already rebinds in auth/session_store_sql.go:253 + outbox.
- F-4 P1 - contracts/firewall_test.go blind to unaliased /vN imports; real
  leaks pass: authz.Enforcer embeds *casbin.Enforcer; auth.Claims embeds
  jwt.RegisteredClaims (both frozen public types).
- SEC-1 P0-before-v1.0 - default CORS reflects any Origin WITH credentials
  (router.go:103-106 + corsmw.go:71-79; app.go:353-363 only restricts when
  cors_origins set). Violates SPEC 2.4.
- DOC-1/DOC-2 P0 - RATE_LIMITING (rate_limit: vs rate_limit_requests) and
  MULTISITE (list vs map; phantom app.*/db.* symbols) teach config the loader
  rejects. 8/24 internal pages fail section 9.
- WEB-1 P0 - website storage-and-tasks.md:62 storage.Metadata does not compile
  (-> storage.PutOptions).
- CLI-V2-1 P2 - scaffold pins advisory toolchain go1.26.3 (new.go:140-141).
- F-13 P3 - CLAUDE.md:52 says cmd/goframe/; real is cmd/nucleus/.
- SEC-3 admin CSRF gap; GOV-1 SLO promotion drift; full list in the report.

## Files of interest

- docs/audits/2026-06-07-exhaustive-audit-v2.md (report)
- pkg/model/crud.go (F-3); contracts/firewall_test.go (F-4)
- pkg/router/router.go, pkg/router/corsmw.go, pkg/app/app.go (SEC-1)
- docs/guides/RATE_LIMITING_GUIDE.md, docs/guides/MULTISITE_GUIDE.md (DOC-1/2)

## Notes / decisions log

- 2026-06-07 - Audit-only iteration; no fixes. Go lane executed with the
  maintainer's offline module cache (go1.26.4; GOCACHE on local disk to dodge
  the virtiofs FD limit). admin/* + DB matrix ci-delegated.
