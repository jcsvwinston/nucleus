# Nucleus / GoFrame — Exhaustive Audit (executed lanes)

> Date: 2026-06-21 · Branch at audit time: `main` @ `d85cb7c` (working tree
> clean except the untracked, intentionally-uncommitted `2026-06-14` audit).
> Triggered by: the scheduled `auditora` task — exhaustively re-verify the
> framework's correctness and hunt for bugs / vulnerabilities / falsehoods in
> **code and docs**, measured against the "best web framework on the market"
> bar. Standing instruction: verify against the **code**, not against what the
> project documents about itself.
> Status: **AUDIT COMPLETE. Remediation not started** (report-only run; per the
> protected-`main` workflow, fixes land via branch → PR by the maintainer).
> Predecessor: `docs/audits/2026-06-14-exhaustive-audit.md`. This pass re-grades
> that report's open findings against the post-`v0.9.0` arc — most importantly
> the **admin→orbit extraction (ADR-019, PRs #148–155)**, which removed
> `pkg/admin` from the core — and adds first-hand findings on the new surface.

---

## 0. Scope, method, environment

Lanes executed this pass:

1. **Functional** — `go build` / `go vet` / `go test` on the root module.
2. **Contracts** — freeze + firewall tests; compatibility harness.
3. **Concurrency** — `go test -race` across every goroutine-bearing package and
   the request path (incl. the new `pkg/nucleus` EventBus).
4. **CLI** — compiled `nucleus` binary smoke-tested end to end + a scaffold →
   generate → **build → run → probe-endpoints** proof.
5. **Security / code** — static review of CORS, mail, sessions, the new
   ADR-019 surface (`Router.Mount`, `Runtime.DatabaseHandle(s)`, first-party
   `EventBus`, `SessionManager.ActiveSessions`); one runtime CORS harness; one
   runtime scaffold-boot harness.
6. **Docs faithfulness** — §9 body-content verification (Go symbols vs the
   freeze baseline, YAML keys vs `CONFIG_KEY_REGISTRY.md`, Go-version pins vs
   `go.mod`) across the guides + the public website, **plus** a first-hand run
   of the repo's own `scripts/website/bodycheck` tool over both trees.

**Environment — the Go lane actually executed.** Go 1.26.4 (linux/arm64), the
maintainer's exported module cache served offline (`GOMODCACHE` +
`GOPROXY=off`), build cache on local disk, `GOWORK=off`, `GOTOOLCHAIN=local`,
`GOSUMDB=off`. Root module (`pkg/*`, `internal/*`, `cmd/nucleus`, `contracts`,
`examples/mvc_api`) was built, tested, and race-tested. The live DB-matrix
engines (PostgreSQL/MySQL/MSSQL/Oracle) were **not** run locally (no Docker) —
they remain `[ci-delegated]`. The **`orbit`** repo (the new home of the admin
panel) is a separate module and was **not** mounted this session — see §7.

**Confidence:** `[verified]` executed/inspected first-hand this pass ·
`[reported]` evidence w/ file:line · `[ci-delegated]` covered by the required
CI gate, not re-run here.

**Severity:** P0 functional break / doc falsehood a reader copies and it fails
on a default path · P1 real defect / contract drift / security gap · P2
hygiene/latent · P3 cosmetic.

---

## 1. Executive verdict

| Question | Verdict |
|---|---|
| **Does the framework build, vet, test, and race-test?** | **YES.** `go build ./...` rc=0, `go vet ./...` rc=0, `go test ./pkg/... ./contracts/...` green, `go test -race` green on every goroutine-bearing package + the request path. The only failures are the long-documented offline artifact: five scaffold *build-smoke* tests that run `go mod tidy` and can't resolve testify's exact-version test-only deps under `GOPROXY=off`. No assertion failures. §3. |
| **Is the admin→orbit extraction clean at the code level?** | **YES.** `pkg/admin` is gone, the freeze baseline was rebaselined (the removed symbols are absent), the generated `nucleus.yml` template points to orbit, and a generated app **builds and boots** with no admin. The core code did its half of the migration well. §2. |
| **Did the migration leave falsehoods behind?** | **YES — and on the most-trafficked path.** The **scaffold templates still advertise the removed in-core admin** (the `nucleus new` banner, `main.go` comments, and the generated `README.md`), and the README instructs a config edit that now **breaks boot**. NEW P1 (**S-1**). The public website still ships a whole `/admin` story (quickstart "sign in at /admin", a `features/admin.md` page, boot-breaking `admin_prefix:` YAML). §4–§5. |
| **Are the prior code findings fixed?** | **NO — all three carried.** `CookieSessionStore` is still silently non-functional (**N-1, P1**); `cors_origins:["*"]` + credentials still reflects any origin with credentials (**N-2, P2**) — now *enshrined in a passing regression test*; `mail.Message.Headers` is still unsanitised (**N-3, P2**). §4. |
| **Are the docs faithful?** | **Internal guide falsehood still live** (`AUTH_GUIDE.md` teaches 2 non-existent keys + a phantom field — **N-4, P1**, unchanged since 2026-06-14). The §9 body-content checker the last three audits asked for **was built** (`scripts/website/bodycheck`) — but it is **not wired into any CI workflow**, only points at `website/docs`, has a precision bug, and misses prose. §6. |
| **Enterprise-class position?** | **The core is in good shape; the edges and the docs are the drag.** Build/test/race/contracts are green, the new ADR-019 surface is well-built and race-clean, but the same small holes recur audit-over-audit because doc-correctness still isn't enforced and the carried code findings haven't been picked up. §8. |

**One-line position:** Nucleus is a **clean-building, race-clean, contract-stable
framework that executed a hard refactor (admin→orbit) well at the code level** —
but it shipped that refactor with its **flagship first-run experience (the
scaffold) still describing a feature that no longer exists**, and it is still
carrying every code/doc finding from the previous audit. None are deep; all are
PR-sized; the blocker to "best-in-market" is follow-through, not architecture.

---

## 2. What the post-v0.9.0 arc did well — `[verified]`

Credit where due, confirmed against the **code** this pass:

- **The admin→orbit extraction (ADR-019) is clean at the source.** `pkg/admin`
  is removed; `MountAdmin`, `AdminAgentConfig`, `App.Admin` are absent from
  `contracts/baseline/api_exported_symbols.txt` (rebaselined, not faked); the
  config registry marks `admin_prefix`/`admin_title`/`admin_bootstrap_*` as
  `removed` with a pointer to `modules.orbit.*`, and `admin_rbac_policy_file`
  as a `deprecated` alias for `rbac_policy_file`. A generated app builds and
  boots with no admin and no dangling references in its `nucleus.yml`.
- **The new ADR-019 public surface is genuinely well-engineered.**
  `SessionManager.ActiveSessions`/`SessionInfo` (`pkg/auth/session_enumerate.go`)
  carries an exemplary `SECURITY:` docblock (Token is a bearer credential,
  Values may hold secrets, in-process-operator-only), handles nil manager,
  non-iterable stores, and undecodable payloads. The first-party `EventBus`
  (`pkg/nucleus/eventbus.go`) is careful concurrent code: Release discipline,
  `sync.Once` cancel, drain-on-cancel, and a slice copy to avoid backing-array
  aliasing. `Router.Mount`'s canonical redirect targets the static dev-supplied
  pattern (no open-redirect from request input). All race-clean (§3).
- **The prior-arc fixes still hold** (re-spot-checked): F-3 CRUD dialect rebind,
  F-4 firewall `/vN`, SEC-1 CORS credentials default, `OrderBy` allow-list,
  `Router.Resource("")`, CLI `scaffoldGoVersion` freshness. None regressed.

---

## 3. Functional, contracts, concurrency, CLI lanes — **PASS** `[verified]`

| Step | Result |
|---|---|
| `go build ./...` | **rc=0** |
| `go vet ./...` | **rc=0** |
| `go test ./pkg/...` | **all PASS** (25 packages) |
| `go test ./contracts/...` | **PASS** (freeze + firewall + harness) |
| `go test ./internal/... ./cmd/... ./examples/...` | **PASS except the 5 documented offline-tidy smoke tests** — `TestRunGenerateResourceBuilds`, `TestRun_GenerateResource`, `TestRun_GenerateModelAndHandler`, `TestRun_StartAppScaffold`. Every failure is the *same* class: the test runs `go mod tidy` inside a scaffolded project and the offline cache holds `go-spew`/`go-difflib` only at pseudo-versions, not testify's pinned `v1.1.1`/`v1.0.0`. **Not a code defect** — see proof below. |
| `go test -race ./pkg/{nucleus,signals,outbox,circuit,health}` | **PASS, race-clean** (incl. new EventBus/runtime) |
| `go test -race ./pkg/{router,auth,tasks,observability,db}` | **PASS, race-clean** |
| CLI smoke (`version`, `new`, `generate resource`, run, probe) | **all rc=0** |

**Generated-code build proof `[verified]`.** Scaffolded `nucleus new myapp`,
`generate resource Widget name:string price:int`, pointed the module at the
local nucleus via `replace`, and ran `go build ./...` → **rc=0**. Then built and
**ran** the app and probed endpoints: `/healthz` → **200**, `/admin` → **403**,
`/_/config` → **403**. The generated code is correct and boots; the `/admin`
result is the evidence for S-1 below.

---

## 4. Findings — code

### S-1 · P1 · `[verified, runtime-confirmed]` — the scaffold still advertises the removed in-core admin, and its README instructs a boot-breaking edit

This is **new this pass** and it is on the single highest-traffic path in the
whole project: what a new user gets from `nucleus new`. The admin→orbit
migration updated the `nucleus.yml` template (it correctly says *"To add an
admin UI, mount the orbit module"*) but left **three sibling surfaces stale**:

1. **`internal/cli/new.go:114`** — for the default (`mvc`) template the CLI
   prints `Running endpoints: http://localhost:8080/admin  (and /healthz)`.
   There is no `/admin` — runtime-confirmed **403**.
2. **`internal/cli/scaffold/templates/mvc/main.go.tmpl:21-25`** — the generated
   `main.go` comments claim *"the admin panel is mounted at /admin"*, that
   `admin_rbac_policy_file` *"grants anonymous access"*, and that *"the app
   serves /admin and the built-in endpoints."* All false (and the generated
   `nucleus.yml` actually uses `rbac_policy_file`, not `admin_rbac_policy_file`,
   so the comment is even internally inconsistent with the file beside it).
3. **`internal/cli/scaffold/templates/_common/README.md.tmpl:16-26`** — the
   generated README has a whole *"First boot: admin account & password"*
   section: it tells the user the first boot creates a bootstrap admin from
   `admin_bootstrap_email`, to *"sign in at /admin"*, and — the sharp edge — to
   *"set `admin_bootstrap_password` in `nucleus.yml` and reboot."*

**Why P1, not cosmetic — runtime proof of harm.** `admin_bootstrap_email` and
`admin_bootstrap_password` are registry status **`removed`**. The generated app
uses *strict* config loading, so following the README's own instruction bricks
the app:

```
$ printf '\nadmin_bootstrap_password: hunter2\n' >> nucleus.yml && ./myapp
myapp: nucleus: unknown configuration key(s) in nucleus.yml:
  - admin_bootstrap_password
```

A new user's literal first-run experience is a README that describes a
non-existent panel and hands them a remediation step that refuses to boot.

**Fix:** update the three surfaces to match the already-correct `nucleus.yml`
template — drop the `/admin` banner line (or point it at orbit), rewrite the
`main.go.tmpl` comment block, and replace the README's admin section with the
orbit-module pointer. Owner: `examples-maintainer` is the wrong agent (these are
CLI templates); route through the CLI/`doc-updater` path. Add a scaffold smoke
assertion that the generated README/banner contains no `/admin`.

### N-1 · P1 · `[verified]` — `auth.CookieSessionStore` is still a frozen, exported, silently non-functional store *(carried, unchanged)*

`pkg/auth/session_store_cookie.go`. `CommitCtx` (99-128) still marshals,
encrypts, base64-encodes the payload — **and discards it**:

```go
encoded := base64.URLEncoding.EncodeToString(ciphertext)
// Store the encrypted data in the session using the token as key
_ = encoded // In a real implementation, this would be set as a cookie   // line 126
return nil  // reports success
```

Nothing is persisted; every read misses. The godoc (line 15) still asserts
*"CookieSessionStore persists sessions in encrypted cookies"* — a falsehood
living in shipped code. It is still **frozen public API** (baseline lines 251,
266-273, 348) and still has **no round-trip test** anywhere in `pkg/auth`
(grep-confirmed). Mitigant unchanged: not config-selectable (`session_store`
accepts only memory/sql/redis), reachable only via `SetStore`. Flagged in the
2026-06-14 audit; **not picked up.** Fix options unchanged: implement for real
(needs middleware cooperation — a design change), make it fail loudly
(`"not implemented"`), or deprecate-and-remove. Route through `contract-guardian`.

> Corollary (P3): `SessionManager.ActiveSessions`' docstring claims the cookie
> store yields `ErrSessionStoreNotIterable`, but `CookieSessionStore.AllCtx`
> returns an empty map, so `ActiveSessions` on a cookie store silently returns
> an **empty slice**, not the sentinel error. Another small lie that exists only
> because N-1 was never resolved.

### N-2 · P2 · `[verified, runtime-confirmed]` — `cors_origins:["*"]` + `cors_allow_credentials:true` still reflects any Origin with credentials *(carried; now locked in by a test)*

Re-confirmed at runtime against the **current** code (the FW-6 refactor changed
the shape but not the outcome). Harness via the public `router.CORSMiddleware`:

```
config: cors_origins=["*"], cors_allow_credentials=true
request Origin: https://evil.attacker.example
-> Access-Control-Allow-Origin     = https://evil.attacker.example
-> Access-Control-Allow-Credentials = true
```

Any site can read authenticated cross-origin responses. Mechanism unchanged:
`pkg/app/app.go:268` gates the credentials pass-through on `len(CORSOrigins) > 0`
— and `["*"]` has length 1, so it takes that branch; only the *empty* list is
guarded (`else if`, 273). `pkg/router/corsmw.go` then reflects the origin
because the `allowAll && !AllowCredentials` short-circuit (line ~71) is false
when credentials are on.

**New wrinkle:** `pkg/router/corsmw_fw6_test.go:15`
(`TestCORSMiddleware_WildcardWithCredentialsReflectsOrigin`) now **asserts this
behaviour as correct**. The FW-6 work fixed the *invalid-header* failure mode
(`*` + credentials, which browsers reject) but encoded the *security* failure
mode (reflect-any-origin + credentials) as the intended result. The app's own
comment (app.go:264-265) names this exact outcome as the thing to prevent.
**Fix:** reject allow-all + credentials at the `pkg/nucleus` load path (loud boot
error) and drop credentials with a WARN in `CORSMiddleware` when `allowAll`;
re-point the FW-6 test at that refusal. A best-in-market framework refuses the
footgun rather than shipping a test that blesses it.

### N-3 · P2 · `[verified]` — SMTP header injection via unsanitised `mail.Message.Headers` *(carried, unchanged)*

`pkg/mail/mail.go` `validateMessage` (208-245) guards `From`/`Subject` for
`\r\n` and `ParseAddress`-checks recipients, but **never inspects the `Headers`
map** (the function returns at 245 with no reference to `msg.Headers`).
`pkg/mail/message.go:23-28` still only `TrimSpace`s each value (interior CRLF
survives) and never validates the key, then writes `key: value` into the
`\r\n`-joined wire message. A value `"x\r\nBcc: attacker@evil.com"` injects an
arbitrary header. Latent (no framework path routes untrusted input into
`Headers` by default → keeps it P2), but the framework owns SMTP assembly, so it
owns the sanitisation. **Fix:** in `validateMessage`, reject header keys that
aren't valid RFC-822 field-name tokens and any value containing `\r`/`\n`.

### New-surface review (ADR-019) — `[verified]`, no defects

`Router.Mount`, `Runtime.DatabaseHandle(s)`, `EventBus`, and
`SessionManager.ActiveSessions` were reviewed line-by-line and are sound (§2).
Two low-severity notes, both P3:

- **`EventBus.HTTPEvent.PayloadPreview`** masks query keys containing
  `KEY/SECRET/PASSWORD/TOKEN` but, per its own doc, **not** OAuth `code`/`state`.
  An OAuth authorization `code` is a short-lived bearer secret; on an
  operator-only feed this is documented and low-risk, but masking `code`/`state`
  too would close it.
- **`Runtime.DatabaseHandle(s)`** hand a module the raw framework `*db.DB`, which
  **bypasses** the per-request tenant scoping of `DBForRequest`. By design and
  documented ("framework-owned; do not Close"), but worth a one-line caveat in
  the godoc that tenant-aware queries must use the request-scoped path.

---

## 5. Findings — documentation (falsehoods)

### N-4 · P1 · `[verified]` — `docs/guides/AUTH_GUIDE.md` still teaches two non-existent config keys and a phantom Go field *(carried, unchanged)*

- **L468-469** `authz_model_path:` / `authz_policy_path:` — **absent** from
  `CONFIG_KEY_REGISTRY.md`. Strict loading rejects unknown keys, so a reader who
  copies this `# nucleus.yml` block **fails to boot**. Canonical key:
  `rbac_policy_file` (the Casbin model is built-in; there is no model-path key).
- **L531** `enforcer, err := authz.New(logger, cfg.AuthzPolicyPath)` —
  `cfg.AuthzPolicyPath` is **absent** from the freeze baseline *and* from
  `pkg/app` source; the snippet **won't compile**. Canonical field:
  `cfg.RBACPolicyFile`.

The public-website twin was fixed long ago; only the internal guide lags —
because the body-content checker isn't pointed at `docs/guides` (§6). Route
through `doc-updater` → `docs-content-verifier`.

### D-WEB · P1 · `[verified]` — the public website still ships an in-core `/admin` story that no longer exists

The admin→orbit break left the website (`website/docs/**`) carrying the old
admin surface across ~11 pages. The most damaging, reader-copies-and-it-fails
cases:

- **`website/docs/getting-started/quickstart.md`** (the highest-traffic
  onboarding page) — L42/L47 advertise `/admin` for the `mvc` skeleton, and
  L169-177 has a whole *"5 — Create an admin user … sign in to the admin panel
  at /admin"* section. `/admin` is a 403; there is no admin user flow.
- **`website/docs/features/admin.md`** — an entire page for the removed feature;
  its frontmatter `covers:` lists `pkg/app.App.MountAdmin` /
  `App.RegisterAdminModels` (absent from the baseline).
- **Boot-breaking YAML** in copy-paste blocks: `features/admin.md:30` and
  `concepts/configuration.md:99` both show `admin_prefix: /admin` — a `removed`
  key that fails strict load.

This cluster is the **failing "Website Docs Drift (advisory)" CI job**
(`.github/workflows/ci.yml:373`, which runs `check-coverage.sh --strict`) and is
already captured by the open follow-up chip **`task_b8cbc177`**. It is tracked —
but it is still live, so it belongs in this audit. Note the CI job is marked
**advisory** (non-blocking), which is why main is "green" while shipping the
drift; consider making it blocking once cleared. Route through `website-curator`.

### N-5 · P3 · `[verified]` — residual §9 drift *(carried)*

- `docs/guides/ERROR_HANDLING.md:434` — prose "Go 1.13" (floor is 1.26). The
  repo's own `bodycheck` flags this as a hard violation when pointed at the
  guides (§6).
- `CONTRIBUTING.md:10` — "matches the `go 1.26.3` directive in `go.mod`";
  `go.mod` declares **`go 1.26.4`**. (README/QUICKSTART/installation are correct.)
- `docs/guides/{MAIL,STORAGE}_GUIDE.md` historical/illustrative blocks
  (`sendgrid_api_key`, `s3_bucket`, `s3_region`) still lack the exact
  `# illustrative` / `# deprecated, use …` marker §9 requires.

---

## 6. Structural — the §9 body-content guard was built but is dormant `[verified]`

This is the headline structural update, and it is a *mixed* result.

**Good news:** the body-content checker that the last three audits kept asking
for **now exists** — `scripts/website/bodycheck/main.go`. It checks (1) Go-version
claims vs `go.mod` [hard], (2) `pkg.Symbol` references in fenced ```go blocks vs
the freeze baseline [hard], and (3) YAML keys vs the config registry [advisory].

**Bad news — it cannot catch today's falsehoods, for four independent reasons:**

1. **It is wired into no CI workflow.** `grep -rl bodycheck .github/` →
   *nothing*. The only doc gate in CI is `check-coverage.sh` (frontmatter
   dangling-`covers:` only). The tool is built and dormant.
2. **It only points at `website/docs`.** The default `-docs website/docs` means
   `docs/guides/**` — where **N-4** and **N-5** live — is never scanned.
   *Demonstration:* `go run ./scripts/website/bodycheck -docs docs/guides
   -strict` exits **1** and flags `ERROR_HANDLING.md:434` (N-5) immediately. One
   flag would surface it; it isn't run.
3. **It misses prose.** The `/admin` falsehoods in the quickstart and the
   `admin_prefix` boot-breaker are prose / plain YAML, not package-qualified
   symbols in ```go blocks — so even on `website/docs` it reports **0 hard
   violations**. The drift is caught only by the *frontmatter* check.
4. **It has a precision bug that would generate false positives on the guides.**
   When run over `docs/guides` it flags `app.DatabaseForRequest` and
   `app.Database` (MULTISITE_GUIDE) as "not in baseline" — but those methods
   **do exist** (`App.DatabaseForRequest`/`App.Database`, baseline 155-156,
   `pkg/app/app.go:993`). The tool mis-parses the receiver `h.app.Method` as a
   package qualifier `app.` and can't match `App`-methods invoked on a value.
   This false-positive risk is plausibly *why* it hasn't been pointed at the
   guides — and it must be fixed before it can be.

**Recommendation (unchanged in spirit, sharper in detail):** (a) fix the
`h.app.`/`App.method` parse so the tool is trustworthy on real code; (b) extend
its scope to `docs/guides/**` and `docs/reference/**`; (c) wire it into CI as a
**blocking** lane. Until (a)–(c) land, N-4/N-5-class guide falsehoods will keep
recurring — this is the fourth consecutive audit to say so. The
`docs-content-verifier` subagent discipline remains necessary even after the
tool is wired, because the tool deliberately skips block-local-qualified symbols
like `cfg.AuthzPolicyPath` (exactly N-4's phantom field).

---

## 7. Scope moved to `orbit` — re-home the carried admin findings `[reported]`

`pkg/admin` no longer exists in this repo, so the prior audits' admin-area
findings — **C-1** (`sanitizeNext` same-origin `/admin/../x`), **SEC-2** (admin
bootstrap `fmt.Sprintf` INSERT), **SEC-4** (`X-Forwarded-For` trust in
`RealIP`/rate-limiter), **SEC-5** (admin upload uses raw `header.Filename`), and
the **login-timing oracle** fix — now live in the separate `orbit` module
(`github.com/jcsvwinston/orbit`). They were **not auditable here** this pass.

**Action:** the `orbit` repo has never had a dedicated security pass of its own
and now owns the most security-sensitive surface in the stack (admin authn,
session/RBAC management, file upload). Schedule an `auditora`-equivalent run
against `orbit` (mount the repo, re-verify C-1/SEC-2/4/5 + login timing against
the moved code). Until then, treat those five as *carried, unverified, in orbit*.

---

## 8. Enterprise-readiness scorecard (updated from 2026-06-14)

| Track | 2026-06-14 | Now (2026-06-21) | Note |
|---|---|---|---|
| A — Contract freeze & inventory | DONE | **DONE** | rebaselined cleanly through the admin removal; freeze/firewall green. |
| B — Compatibility harness | PARTIAL | **PARTIAL** | still `core-build`-only. Unchanged. |
| C — Dependency firewall | DONE | **DONE** | green. |
| D — Enterprise data coverage | ~95% | **~95%** | CRUD dialect portability holds; live-PG gated in CI. |
| E — Security & compliance baseline | STARTED | **STARTED** | no movement on N-1/N-2/N-3; the sensitive admin code moved to orbit (now unaudited there). CSRF still opt-in; audit sink still in-memory. |
| F — Cloud integration | PARTIAL | **PARTIAL** | unchanged. |
| G — Developer productivity | STRONG | **REGRESSED to GOOD** | codegen/CLI remain strong, but S-1 means the *first-run experience itself* now ships a falsehood that bricks boot if followed. This is the most user-visible wart in the project. |

**Distance to "best-in-market":** architecturally short, executionally stalled.
The core refactor landed well; the gap is that **every finding from the previous
audit is still open** and the migration's doc/scaffold tail wasn't swept. All
remediation is PR-sized.

---

## 9. Prioritized remediation roadmap (PR-sized, via protected-`main`)

**Sprint 1 — stop shipping the falsehood on the default path:**
1. **S-1** — sweep the scaffold's admin residue (`new.go:114`,
   `main.go.tmpl:21-25`, `README.md.tmpl:16-26`) to match the already-correct
   `nucleus.yml` template; add a scaffold smoke assertion that the generated
   README/banner contains no `/admin`.
2. **D-WEB** — clear `task_b8cbc177` (website-curator): retire/rewrite
   `features/admin.md`, the quickstart admin section, and the `admin_prefix:`
   YAML; add the orbit pointer. Consider flipping the drift job to blocking.
3. **N-4** — fix `AUTH_GUIDE.md` keys (`rbac_policy_file`) + field
   (`cfg.RBACPolicyFile`) via `doc-updater` → `docs-content-verifier`.

**Sprint 2 — the carried code findings + make the guard real:**
4. **N-1** — make `CookieSessionStore` real or loudly unimplemented; add a
   round-trip test through `SessionManager`. (`contract-guardian`.)
5. **N-2** — refuse allow-all + credentials at load; drop credentials with WARN
   in `CORSMiddleware` when `allowAll`; re-point the FW-6 test at the refusal.
6. **bodycheck hardening + wiring** (§6): fix the `h.app.`/`App.method` parse,
   extend scope to `docs/guides` + `docs/reference`, wire it into CI as a
   blocking lane. This is the single highest-leverage structural fix.

**Sprint 3 — hardening tail:**
7. **N-3** — validate `mail.Message.Headers` keys/values for CR/LF + token shape.
8. **N-5** — Go-version + illustrative-marker sweep (ERROR_HANDLING, CONTRIBUTING,
   MAIL/STORAGE guides).
9. **orbit security pass** (§7) — re-home and re-verify C-1/SEC-2/4/5 + login
   timing against the moved code.

---

## 10. What held up (don't spend effort here)

Build, vet, the full unit/integration suite, the freeze/firewall/harness gates,
and the race detector are all green; the CLI scaffolds, generates, builds, and
boots; the admin→orbit extraction is clean at the code level and the new ADR-019
surface (Mount, EventBus, ActiveSessions, DatabaseHandle) is well-built,
well-documented, and race-clean. The core is healthy. The work is at the edges —
a scaffold/website that didn't finish the migration's doc tail, three carried
code findings, one carried guide falsehood, and a §9 guard that exists but isn't
switched on.

---

## 11. Limitations

- The live DB-matrix engines (pg/mysql/mssql/oracle) were `[ci-delegated]`, not
  run locally (no Docker). N-2's runtime evidence is against the in-process
  `CORSMiddleware`, which is the exact code the app wires.
- The `orbit` repo was not mounted; §7's carried findings are unverified there.
- The five scaffold *build-smoke* tests can't run offline (testify test-only
  deps absent at pinned versions); the generated code's compilation **and boot**
  were proved first-hand via `replace` instead.
- Security review is static plus two runtime harnesses (CORS, scaffold boot); no
  pentest/fuzzing. Benchmarks out of scope.
