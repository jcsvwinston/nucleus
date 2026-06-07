# Nucleus / GoFrame — Exhaustive Audit v2 (executed lanes)

> Date: 2026-06-07 · Branch at audit time: `main` @ `d42bf19` (working tree
> carried only incidental `go.sum`/`admin/*/go.mod` churn from a local
> `go mod download`; no source changes).
> Triggered by: maintainer (Carlos) — re-verify framework functionality and the
> fidelity of internal `docs/*` + the public Docusaurus site (`website/`) after
> the 2026-05-29 → 2026-06-06 remediation arc, and position the project against
> the enterprise-class bar. Explicit instruction: verify against the **code**,
> not against what the project documents about itself.
> Status: **AUDIT COMPLETE.** Supersedes the uncommitted scheduled-task draft
> `docs/audits/2026-06-07-exhaustive-audit.md`: every finding that draft tagged
> `[needs runtime]` has now been executed and graded first-hand (§0 environment,
> §12 reconciliation). Remediation not started.

---

## 0. Scope, method, environment

Six lanes, per CLAUDE.md §10 (specialist roles delegated to agents adopting the
`.claude/agents/*` prompts where Cowork exposes only generic agents):

1. **Functional** — `go build` / `go vet` / `go test` on the root module.
2. **Contracts** — freeze + firewall tests; compatibility harness.
3. **CLI** — compiled `nucleus` binary smoke-tested against the contract matrix.
4. **Internal docs** — §9 verification on 24 pages.
5. **Website** — `npm ci && npm run build`, drift guard, §9 on 15 public pages.
6. **Security + governance** — posture review + doc-vs-reality cross-check +
   enterprise scorecard.

**Environment — this pass actually executed the Go lane.** Go 1.26.4
(linux/arm64) with the maintainer's exported module cache served offline
(`GOMODCACHE` + `GOPROXY=file://`), `GOCACHE` on local disk, `GOSUMDB=off`
(integrity still pinned by `go.sum`), `GOTOOLCHAIN=local`, `CGO_ENABLED=0`. The
**root module** (`pkg/*`, `internal/*`, `cmd/nucleus`, `contracts`, `examples/
mvc_api`) was built and tested in non-workspace mode (`GOWORK=off`). The three
`admin/*` sub-modules and the DB-matrix engines (PostgreSQL/MySQL/MSSQL/Oracle)
were **not** run locally (no Docker; the offline cache lacked one `admin/server`
transitive zip) — they remain `[ci-delegated]` to the green `CI Required Gate`.

**Confidence:** `[verified]` executed/inspected first-hand this pass ·
`[reported]` specialist evidence w/ file:line · `[ci-delegated]` covered by the
required CI gate, not re-run here.

**Severity:** P0 functional break / doc falsehood a reader copies and it fails ·
P1 real defect / contract drift / security gap · P2 hygiene/latent · P3 cosmetic.

---

## 1. Executive verdict

| Question | Verdict |
|---|---|
| **Does the framework build, vet and test?** | **YES, on the root module / SQLite lane.** `go build ./...` rc=0, `go vet ./...` rc=0, full `go test ./...` green except 3 scaffold *build-smoke* tests that need network for testify's test-only deps (env artifact — the generated app itself compiles, verified via local `replace`). |
| **Are the contracts intact?** | **YES.** Freeze + firewall tests pass; harness READY 100%. **But** the firewall's green is partly hollow — see F-4. |
| **Are the docs faithful?** | **Website: yes, 3 findings** (build clean, drift guard clean, naming clean; 1 non-compiling example; 2 pages document unfrozen APIs without a tier disclaimer). **Internal guides: NO — 8 of 24 pages fail §9**, two teach config the loader rejects. The 2026-05 remediation cleaned the public site but never swept `docs/guides/*`. |
| **Enterprise-class position?** | **Strong governance chassis (Tracks A–C built & CI-enforced, D ~90%), but two data-layer/contract correctness bugs (F-3, F-4) and Track E security defaults (SEC-1/CSRF) gate the jump.** §9 scorecard. |

**One-line position:** Nucleus is a **v0.8.x framework with v1.0-grade
governance machinery, a clean-building/clean-testing core, pre-v1.0 security
defaults, and two real correctness holes (Postgres CRUD portability; a blind
contract-firewall) that are individually small and individually fixable.**

---

## 2. Functional lane — **PASS (root module, SQLite)** `[verified]`

| Step | Result |
|---|---|
| `go build ./...` (root) | **rc=0** — all `pkg/*`, `internal/*`, `cmd/nucleus`, `examples/mvc_api` compile. |
| `go vet ./...` (root) | **rc=0** — clean. |
| `go test ./pkg/...` | **all PASS** (router, auth, authz, app, db, model, storage, mail, observe, observability, tasks {asynq,memory}, outbox, signals, validate, circuit, health, openapi, plugins, errors, nucleus). |
| `go test ./internal/...` | **PASS** except `TestRunGenerateResourceBuilds` (+ the two `New*Builds` smoke tests) — fail only because the offline cache lacks testify's test-only transitive zips (`go-spew`, `go-difflib`) for `go mod tidy`. **Not a code defect:** the generated project compiles (`go build ./...` rc=0 with a local `replace`). |
| `go test ./contracts/...` | **PASS.** |

A skip-list re-run (`-skip 'TestRunGenerateResourceBuilds|TestNewProjectBuilds|
TestRunNewBuilds'`) returns `ok` for `internal/cli`, isolating the failures to
the network artifact.

---

## 3. Contracts + harness lane — **PASS, with a hollow guarantee** `[verified]`

- `go test ./contracts -run '^TestContractFreeze_|^TestFirewall_'` → **ok**
  (API symbols, CLI commands, config keys, type firewall).
- `scripts/ci/run_compatibility_harness.sh --enforce-threshold` → **READY,
  1/1 (100%)** — but the only profile is `core-build` (`go build ./pkg/... ./
  cmd/nucleus ./internal/cli/...`). The fixture apps were purged in ADR-010
  Phase 1; the harness today proves *compilation*, not *behavioural
  compatibility*, until the fixtures return in v0.9.X. The roadmap's ✅ on
  Track B overstates what is currently enforced.

**F-4 · P1 · [verified] — the firewall freeze test is blind to `/vN` imports,
and real leaks live behind that blindness.** `contracts/firewall_test.go`
resolves an import's package name only when an explicit alias is present
(`extractImports`: `name=""` unless `imp.Name != nil`). For a normal versioned
import like `github.com/casbin/casbin/v2`, the name is left empty, so the
name-match (`impName != "" && ident.Name == impName`) is skipped, and the
fallback `strings.HasSuffix(impPath, "/"+ident.Name)` tests `…/v2` against
`/casbin` and fails. Net effect: **every forbidden third-party package imported
without an alias is invisible** — and the forbidden registry is almost entirely
`/vN` paths (casbin/v2, golang-jwt/jwt/v5, validator/v10, koanf/v2,
go-redis/v9, pgx/v5). Two concrete leaks the green test misses, both in
**frozen public** types:

- `pkg/authz/enforcer.go:56-58` — `type Enforcer struct { *casbin.Enforcer; … }`
  embeds the Casbin enforcer, promoting all of Casbin's exported methods onto
  the public `authz.Enforcer` (in the freeze baseline as `type:Enforcer` +
  ~dozen `method:Enforcer.*`).
- `pkg/auth/jwt.go:26-30` — `type Claims struct { …; jwt.RegisteredClaims }`
  embeds `golang-jwt/jwt/v5`'s claims into the public `auth.Claims` (baseline
  `type:Claims`).

*Fix:* in `extractImports`, default the package name to the path's last
non-`vN` segment when no alias is given; re-run the firewall; then decide per
leak whether to wrap (Track C) or formally bless the embed via ADR.

---

## 4. CLI lane — **PASS** `[verified]`

Compiled `cmd/nucleus` (rc=0). Smoke results (all rc=0):
`version` (`nucleus dev`), `help` (all primary commands present), `new demoapp`
(full scaffold incl. `rbac_policy.csv` — confirms ADR-013 R5), `generate
resource article` (feature-folder layout: contracts/models/repositories/
controllers+test/services + up/down migrations), `doctor` (DEGRADED with sane
warnings; `rbac` PASS), `migrate status` (shows the pending generated
migration), `config print --effective --config nucleus.yml` (5-layer effective
config with per-key source attribution `yaml:nucleus.yml:25` and `[REDACTED]`
secrets — confirms the redaction + Phase-3 inspection surfaces), `routes`, and
`openapi` (exports `openapi.json`). The admin bootstrap one-time password is
emitted to stderr exactly as documented (ADR-013 R6).

**CLI-V2-1 · P2 · [verified] — the scaffold pins the advisory-affected
`toolchain go1.26.3`.** The generated `go.mod` is `module …; go 1.26; toolchain
go1.26.3; require github.com/jcsvwinston/nucleus v0.8.0`. The framework version
pin (`v0.8.0`) is correct (the old `latest` concern is resolved). But the
`toolchain go1.26.3` directive (`internal/cli/new.go:140-141`, hand-maintained
constants) is the exact toolchain PR #91 evacuated for GO-2026-5037/5038/5039 —
the framework `go.mod` now declares `go 1.26.4`. Every fresh project is seeded
with the advisory toolchain, contradicting the constants' own "MUST track
go.mod" comment. *Fix:* derive the directive from the embedded framework
`go.mod`, or add a freeze test asserting `scaffoldToolchain` ≥ `go.mod`.

---

## 5. Internal documentation lane — **FAIL (8 of 24 pages)** `[verified]`

`docs-content-verifier` (§9) over guides (14), reference (8), README,
QUICKSTART. **8 Go-symbol violations, ~22 YAML-key violations (13 unique keys),
1 Go-version violation.**

**DOC-1 · P0 — `RATE_LIMITING_GUIDE.md` is built around a key that does not
exist.** `rate_limit: 100` in 10+ blocks (e.g. L43); the real key is
`rate_limit_requests` (`pkg/app/config.go:145`, registry L211). Strict loading
rejects unknown keys (`ErrUnknownConfigKeys`) → a reader's config fails on first
boot. Also `rate_limit_window: 60` (integer) — the field is `time.Duration`,
needs `"60s"`/`"1m"`; and `rate_limit_roles`/`rate_limit_store`/
`rate_limit_redis_url` are unregistered (the last two are "future" but not
marked illustrative).

**DOC-2 · P0 — `MULTISITE_GUIDE.md` teaches a multisite schema that does not
exist.** YAML (L40-50) shows `multisite.sites` as a **list** with
`host`/`name`/`locale`; the shipped schema (`config.go:214-226`) is a **map**
keyed by site name with `hosts []string`, `database`,
`tenant_database_alias_template` — no `name`, no `locale`. Go side: 7 phantom
refs — `app.DatabaseForRequest`/`app.Database`/`app.DB` (methods/fields on
`*app.App`, not package-level), `db.QueryContext`/`db.Alias()` (not on
`*db.DB`; go through `SqlDB()`). `multitenant.resolution`/`header_name` should
be `resolver`/`header`.

**DOC-3 · P1 — `AUTH_GUIDE.md:469-470:** `authz_model_path`/`authz_policy_path`
are not keys (canonical `admin_rbac_policy_file`); CSV samples may predate the
DEP-2026-003 4-column (`allow`/`deny`) form.

**DOC-4 · P1 —** `ERROR_HANDLING.md:434` "Go 1.13+" (floor 1.26);
`MAIL_GUIDE.md:257-258` (`sendgrid_api_key`/`sendgrid_endpoint`) and
`STORAGE_GUIDE.md:601-604` (`s3_bucket`/`s3_region`) show removed legacy keys
without the `# deprecated/# removed` comment §9 requires;
`PLUGIN_SDK.md:138-145` shows five proposed `plugins.*` keys with no
`# illustrative` marker.

**Root cause:** the 2026-05 remediation swept the website + top-level reference
docs but never gave `docs/guides/*` the §9 pass; the pre-flat-config guides
(MULTISITE, RATE_LIMITING, AUTH) still describe the old world.

---

## 6. Website lane — **PASS with findings (2 of 15 pages)** `[verified]`

- `npm ci` + `npm run build`: **clean** (no broken links; `onBrokenLinks:'throw'`).
- `check-coverage.sh --strict`: **0** legacy tokens, **0** dangling `covers:`,
  **0** missing manifests.
- Naming: **0** `goframe`/`GoFrame` on the public site.
- §9 body: **5 Go-symbol violations, 0 YAML, 0 Go-version.**

**WEB-1 · P0 — `features/storage-and-tasks.md:62` does not compile.**
`storage.Metadata{ContentType:"image/png"}` — no such type; `Put()` takes
`storage.PutOptions`. Copy-paste fails `go build`.

**WEB-2 · P1 — public pages document unfrozen packages without a stability
disclaimer.** `routing.md:176-177` (`openapi.DocumentProvider`/`openapi.Document`)
and `storage-and-tasks.md:166-172` (`outbox.ManagedOutbox`/`outbox.Entry`)
reference packages deliberately outside the freeze baseline —
`pkg/openapi`=`experimental`, `pkg/outbox`=`transitional`
(`contracts/packages_test.go:102-103`). The pages present them with frozen-API
authority. *Fix:* per-page lifecycle badge, not freezing immature surfaces.

**WEB-3 · P3 — `check-coverage.sh` prints "OK (warn mode)" even under
`--strict`** (exit codes are correct; only the success string is wrong).

**Systemic gap:** body-content §9 is still manual discipline (the planned
`check-coverage.sh` body extension never landed) — WEB-1 and DOC-1/2 would not
be caught by CI.

---

## 7. Security posture — **WARN** `[reported]` unless noted

Strong fundamentals: bcrypt 12; JWT alg-confusion guard + kid match; JWKS never
exposes HMAC; parameterised CRUD with field-name validation; storage
path-traversal double-guarded; 40+ key redaction denylist (confirmed live via
`config print` `[REDACTED]`); tenant-isolation sentinel on every
`DatabaseForRequest`; plugins behind typed envelopes; constant-time cluster
token compare; session fixation handled; SameSite=None⇒Secure forced.

**SEC-1 · P0-before-v1.0 · [verified] — default CORS reflects any Origin WITH
credentials.** `router.New()` defaults `corsAllowAll:true,
corsAllowCredentials:true` (`pkg/router/router.go:103-106`). FW-6 stopped the
invalid `*`+credentials pair, but the default path now reflects **every**
request Origin with `Access-Control-Allow-Credentials:true`
(`pkg/router/corsmw.go:71-79`). `pkg/app` only restricts when `cors_origins` is
non-empty (`app.go:353-363`) — never in zero-config. The R4 comment in
`app.go` describes intent, not behaviour. Blast radius today is tempered by
SameSite=Lax cookies, but any `session_cookie_samesite: none` deployment
becomes "any origin reads authenticated responses." Violates SPEC §2.4.
*Fix:* default `corsAllowCredentials:false`; require explicit `cors_origins` +
`cors_allow_credentials:true`.

**SEC-2 · P1 · [reported] — admin bootstrap INSERT via `fmt.Sprintf` +
hand-rolled quoting** (`pkg/admin/bootstrap_admin.go:92-102`). Config-sourced
inputs (not HTTP), but the only non-parameterised SQL site; latent escalation
if the table name becomes configurable.

**SEC-3 · P1 · [verified] — CSRF is opt-in (`WithCSRF`); the admin panel mounts
its own mux with NO internal CSRF layer** (zero `csrf` references under
`pkg/admin/`). A default-stack app relies entirely on session SameSite for
state-changing admin routes; composes with SEC-1. Track E work.

**SEC-4 · P2 —** `X-Forwarded-For` trusted unconditionally in `RealIP` +
rate-limiter key (`httputil.go:71-82`, `ratelimit.go:246-263`); spoofable
per-IP limits, no trusted-proxy config.
**SEC-5 · P2 —** admin import upload uses raw `header.Filename` in storage
key/log/response (`management.go:371`); traversal blocked downstream, log/JSON
injection not.
**SEC-6 · P3 —** `sanitizeNext` allows `/admin/../x` same-host escape
(`default_auth.go:249-255`); a `"secret"` literal in an asynq test fixture.

*(Carried from the scheduled draft, not independently re-confirmed this pass —
re-verify before acting: mail header CRLF injection via `Message.Headers`;
`QueryOpts.OrderBy` raw concat; admin tenant chosen by `?tenant=`; unbounded
`EmitAsync` goroutines; a no-op `CookieSessionStore`.)*

---

## 8. Governance lane — **WARN** `[verified]`

Enterprise-grade today: `CI Required Gate` aggregates **7 required jobs** —
test, db-matrix-required (pg+mysql), **live MSSQL**, **live Oracle**,
compatibility-harness, contract-freeze, admin-skeleton; branch protection
(PR-only `main`, `enforce_admins`) matches the handoff. Contract freeze covers
API + CLI + config + a type firewall. 3 DEPs on record, each with MA artefacts
+ CHANGELOG entries.

- **GOV-1 · P1 —** `COMPATIBILITY_SLO.md` still classifies MSSQL/Oracle as
  *exploratory* (promoted to required 2026-05-12) and keeps the pre-v1.0 80%
  target for lanes now gated at 99%.
- **GOV-2 · P2 —** Reference dates stale on 6 governance docs (worst:
  `ENTERPRISE_LONG_TERM_ROADMAP.md`, 54 days).
- **GOV-3 · P2 —** `website-drift` lane + `rehearsal.yml`/`rehearse_rc.sh`
  undocumented in CI_MATRIX / RELEASE_CHECKLIST.
- **GOV-4 · P2 —** Dependency-impact report runs in `release.yml` **without**
  `--enforce-critical-review` (generated, not blocking).
- **GOV-5 · P2 —** No durable per-release store for harness/SLO metrics.

**Structural asymmetry:** every *code* contract is CI-enforced; every
*documentation* contract is manual discipline. All P0-class doc falsehoods live
exactly there — and F-4 shows even a *code* contract (the firewall) can be
green-but-hollow.

---

## 9. Enterprise-readiness scorecard

| Track | Status | Evidence |
|---|---|---|
| A — Contract freeze & inventory | **DONE** | Inventory w/ lifecycle tags; CLI matrix; config registry; freeze tests CI-gated and **passing (verified)**. |
| B — Compatibility harness | **PARTIAL** | Harness + golden tests CI-gated and green, but only the `core-build` profile runs; fixture apps purged until v0.9.X — proves compile, not behaviour. |
| C — Dependency firewall | **AT RISK** | Firewall tests pass but **F-4** shows they are blind to `/vN` imports, and two real leaks (casbin, jwt) live in frozen public types. The track's core guarantee is partially hollow until F-4 is fixed. |
| D — Enterprise data coverage | **~85%** | 5 live DB lanes required in CI (ahead of the roadmap's own 🚧). **But F-3:** the model CRUD layer is `?`-only and breaks on PostgreSQL/Oracle (required engines), masked by SQLite-only CRUD tests. Gates criterion 4 until fixed. |
| E — Security & compliance baseline | **NOT STARTED** | SEC-1 + SEC-3 are precisely Track E deliverables. |
| F — Cloud integration | **PARTIAL** | aws-sm + S3/GCS/Azure shipped; KMS/Lambda/PubSub/ServiceBus not. |
| G — Developer productivity | **PARTIAL (strong)** | Scaffolds + wizard + doctor work end-to-end (verified); CLI-V2-1 is the one freshness gap. |

**Success criteria:** (1) Upgrade safety — *met pre-v1* (freeze+SLO+DEP), with
the F-4 caveat. (2) Operational excellence — *largely met* (observability,
health, admin runtime, doctor), audit sink still in-memory (doctor warns). (3)
Security posture — **not met** (Track E + SEC-1). (4) Multi-env viability —
**blocked by F-3** (CRUD not portable to required engines). (5) Developer
experience — *met with caveats* (guide P0s actively damage onboarding).

**Distance to enterprise-class:** concentrated and tractable — (a) F-3 CRUD
placeholder portability + a Postgres CRUD test, (b) F-4 firewall fix + leak
disposition, (c) SEC-1 CORS default + Track E security baseline, (d) sweep the
guides (DOC-1/2) and add body-content §9 to CI, (e) restore harness fixtures
(v0.9.X), (f) the pre-v1.0 audit/logging plan. None is research-grade; all are
PR-sized.

---

## 10. Prioritized remediation roadmap (PR-sized, via protected-`main`)

**Sprint 1 — correctness & safety (P0):**
1. **F-3** rebind CRUD placeholders per dialect (`pkg/model/crud.go`); add a
   PostgreSQL CRUD test so the SQLite-only blind spot can't recur.
2. **SEC-1** flip the CORS credentials default; fix the misleading R4 comment.
3. **DOC-1/2** rewrite RATE_LIMITING + MULTISITE guides against the shipped
   schema (+ AUTH keys, DOC-3).
4. **WEB-1** `storage.Metadata` → `storage.PutOptions` on the public site.

**Sprint 2 — contract integrity & un-liable docs (P1):**
5. **F-4** fix `firewall_test.go` `/vN` name resolution; re-run; wrap or
   ADR-bless the casbin/jwt embeds.
6. Body-content §9 into `check-coverage.sh` (extend to `docs/guides/*`) + a CI
   lane; lifecycle badges for experimental/transitional packages (WEB-2).
7. **SEC-2** parameterise the bootstrap INSERT; **SEC-3** admin CSRF layer.
8. **CLI-V2-1** derive the scaffold toolchain from `go.mod` + freshness test.
9. **GOV-1** SLO promotion update + reference-date sweep (GOV-2/3).

**Sprint 3 — Track E formally:**
10. Trusted-proxy config (SEC-4); upload filename hardening (SEC-5); deploy
    checks for high-risk misconfig; a hardening profile doc + tests.
11. `--enforce-critical-review` in `release.yml` (GOV-4); durable harness store
    (GOV-5); re-verify & fold the carried security P2s from §7.

---

## 11. What held up (don't spend effort here)

The build is clean; `go vet` is clean; the entire unit/integration suite passes
on SQLite; the freeze/firewall/harness gates are green; the CLI works
end-to-end (scaffold → generate → doctor → migrate → config → openapi); the
public website builds with no broken links and zero legacy naming; secret
redaction, JWT hardening, bcrypt cost, tenant-isolation sentinel, storage
path-traversal guards, and the 5-layer config loader with source attribution
are all real and working. The core is healthy; the gaps are at the edges
(dialect portability, a blind contract test, security defaults, guide drift).

---

## 12. Reconciliation with the scheduled-task draft

The scheduled `auditora` run produced `docs/audits/2026-06-07-exhaustive-audit.md`
(**uncommitted**, static-only, with a "runtime smoke appendix" of commands to
run). This v2 report **executed that appendix** and consolidates both:

- **Confirmed first-hand here:** F-1 CORS (= SEC-1), F-2 guide falsehoods
  (= DOC-1/2/3), **F-3** CRUD `?`-portability (read the CRUD path + proved the
  CRUD tests are SQLite-only), **F-4** firewall `/vN` blindness (read the
  matcher + located two real frozen-type leaks), F-5 admin CSRF (= SEC-3),
  **F-13** `cmd/` is `cmd/nucleus` while `CLAUDE.md:52` says `cmd/goframe/`
  (P3 doc bug).
- **Added by this pass:** the executed build/vet/test/contracts/CLI lanes; the
  website `npm run build`; CLI-V2-1 (scaffold pins the advisory `go1.26.3`
  toolchain); the enterprise scorecard with Track-by-Track grading.
- **Carried, not re-confirmed:** the §7 security P2 tail (mail CRLF,
  `OrderBy` concat, `?tenant=`, unbounded `EmitAsync`, no-op
  `CookieSessionStore`) — re-verify before acting.

The uncommitted draft can be deleted in favour of this file, or kept as the
static-only predecessor; this report is the authoritative, executed version.

---

## 13. Limitations

- `admin/*` sub-modules and the DB-matrix engines (pg/mysql/mssql/oracle) were
  `[ci-delegated]`, not run locally (no Docker; one missing transitive zip in
  the offline cache). F-3's *runtime* PG failure is therefore inferred from the
  driver contract (pgx requires `$N`, not `?`) + the SQLite-only CRUD tests, not
  observed against a live Postgres.
- Benchmarks (`performance-bench`) out of scope.
- Security review is static; no pentest/fuzzing.
- The working tree carried incidental `go.sum`/`admin/*/go.mod` churn from the
  local `go mod download`; it is not part of this audit and was not committed.
