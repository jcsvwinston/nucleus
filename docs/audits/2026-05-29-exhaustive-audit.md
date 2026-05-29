# Nucleus / GoFrame — Exhaustive Audit

> Date: 2026-05-29 · Branch at audit time: `main` @ `1702770` (clean tree).
> Triggered by: maintainer report of anomalies across the CLI ("does not
> generate functional code"), the framework runtime, and a perceived gap
> between the public website and the shipped code.
> Status: **AUDIT COMPLETE — remediation not started.** This document is the
> source of record for the remediation arc that follows it.

---

## 0. Scope, method, and the toolchain limitation

**Scope.** Three fronts, audited in parallel by specialist reviewers
(standing in for the repo's `.claude/agents/*` per CLAUDE.md §10):

1. CLI + scaffolding (`contract-guardian` + `code-reviewer` lens).
2. Framework runtime + public API (`architect-reviewer` + `code-reviewer` +
   `security-auditor` lens).
3. Documentation fidelity — website + internal docs (`docs-content-verifier`
   primary + `website-curator` + `doc-updater` lens), applying the three §9
   checks (Go symbols vs `contracts/baseline/api_exported_symbols.txt`, config
   keys vs `docs/reference/CONFIG_KEY_REGISTRY.md`, Go-version vs `go.mod`).

**Toolchain limitation (important).** The audit environment **cannot compile or
test the module**: the Go toolchain is absent and the egress proxy blocks
`go.dev`, `dl.google.com`, `proxy.golang.org` and `storage.googleapis.com`
(only `github.com`, `pypi.org`, `registry.npmjs.org` are reachable; `apt`
offers only Go 1.18). Every finding below was therefore established by **static
analysis against source + the contract baselines**. Findings whose *severity*
depends on runtime behaviour are tagged `[needs-runtime]` and carry the exact
command the maintainer must run to confirm. This split is deliberate and
honest — see §6.

**Confidence legend.**
- `[verified]` — re-confirmed against source/baselines during this audit.
- `[reported]` — specialist evidence with file:line, internally consistent,
  not independently re-run this pass.
- `[needs-runtime]` — real defect; its blast radius needs a Go build/test on
  the maintainer's machine to grade precisely.

**Severity legend.**
- **P0** — blocks "100% functional with what's implemented" OR an outright
  documentation falsehood a reader copies and it fails.
- **P1** — real defect, contract drift, or security-hardening gap.
- **P2** — hygiene, latent trap, or polish.

---

## 1. Executive verdict (per complaint)

| Complaint | Verdict | Most load-bearing evidence |
|---|---|---|
| **"The CLI does not generate functional code."** | **Substantiated, severity contingent.** Two concrete defects in the generated `go.mod` (wrong Go floor; mutable `latest` pin) and **zero test coverage of the real published build path** (the only build test injects a local `replace` a user never has). Whether a fresh `nucleus new` hard-fails depends on the user's toolchain + proxy indexing — proven only by the smoke test in §8. | `go.mod.tmpl:3,5`; `new_build_test.go:39-61` |
| **"The framework has anomalies."** | **Confirmed.** One P0 startup panic (`Router.Resource("")`), four P1 correctness/hardening gaps, plus a contract-coverage hole that leaves public REST constants unfrozen. Core security primitives (CSRF, bcrypt, secret redaction, JWT default-deny) are **sound**. | `pkg/nucleus/router.go:142`; `pkg/app/app.go:255-271` |
| **"The website is not faithful to the code."** | **Confirmed — this is the worst front.** The site teaches a **nested YAML config schema that does not exist** (the real schema is flat and the loader rejects unknown keys), the **homepage example does not compile**, and ~20 documented Go symbols **do not exist**. The one historically-burned class — Go-version drift — is currently **clean**. | `website/docs/concepts/configuration.md:60-104`; `intro.md:44-45` |

Bottom line: the framework **core** is closer to functional than the headline
fears suggest, but the **front door** — the scaffolder's `go.mod` and the
public docs a new user reads first — is where "it doesn't work" is born.

---

## 2. CLI findings

**CLI-1 · P1 · [verified][needs-runtime] — Generated `go.mod` pins `go 1.25`, framework requires `go 1.26.3`.**
`internal/cli/scaffold/templates/_common/go.mod.tmpl:3` emits `go 1.25`; the
framework `go.mod:3` declares `go 1.26.3` with **no `toolchain` directive**.
A generated module declaring `go 1.25` that requires a dependency demanding
`go 1.26.3` will, under the default `GOTOOLCHAIN=auto`, silently rewrite its own
`go` line to `1.26.3` and attempt to fetch the `go1.26.3` toolchain; under
`GOTOOLCHAIN=local` with a local Go `< 1.26.3` it **hard-fails** with
`requires go >= 1.26.3`. Either way the hand-written floor is wrong on day one.
*Fix:* drive the floor from the framework's `go.mod` (interpolate `{{.GoVersion}}` → `go 1.26`) and add a `toolchain go1.26.3` line so older local toolchains upgrade cleanly. *Confirm:* §8 smoke A.

**CLI-2 · P1 · [needs-runtime] — Generated `require` pins `{{.FrameworkVersion}}` = `latest`; "self-contained" promise is untested.**
`resolveFrameworkVersion()` (`internal/cli/new.go:131-141`) returns `"latest"`
for dev builds (`Version=="dev"`, `internal/cli/root.go:16`). The only build
test (`new_build_test.go:39-61`) **rewrites the require to `v0.0.0` and injects
a local `replace … => <repoRoot>`** "because the published dependency … [is]
neither resolvable offline". So **no test exercises the path a real user takes**.
The module *does* publish VCS tags (`v0.5.0`…`v0.8.0` confirmed via
`git ls-remote`), so `GOPROXY=…,direct` resolution is plausible — but the README's
claim of "self-contained, no `replace` directive needed" is **unverified**.
*Note:* the audit could not query `proxy.golang.org` (egress-blocked), so any
claim that the proxy "returns empty" is unreliable; the real test is §8 smoke B
on a clean machine. *Fix:* pin a concrete published tag (not `latest`) for dev
builds, and add a CI smoke that runs `go mod tidy && go build` on a generated
project **without** the local `replace`.

**CLI-3 · P1 · [verified] — `nucleus generate resource` emits non-compiling Go (`writeError` arity).**
In `internal/cli/generate.go` the generated `writeError` helper is defined as
`func writeError(w http.ResponseWriter, r *http.Request, err error)` (lines
`940` and `1323`) but every call site passes **two** args, e.g.
`writeError(w, gferrors.BadRequest(err.Error()))` (lines `1201, 1207, 1217,
1241, 1247, 1255, 1270, 1277`) → `not enough arguments in call to writeError`.
`nucleus new` is covered by a build test; `generate resource` is **not**.
*Fix:* make the helper 2-arg (`func writeError(w, err)` calling `router.Error(w, nil, err)`) or thread `r` into the calls; drop the dead copy. Add a generate-resource build smoke. *Confirm:* §8 smoke C.

**CLI-4 · P1 · [verified] — CLI freeze baseline is stale; four stable commands are unfrozen.**
`internal/cli/root.go` registers `config`, `doctor`, `openapi`, `wizard`, but
`contracts/baseline/cli_primary_commands.txt` omits all four, and
`docs/reference/CLI_CONTRACT_MATRIX.md` omits `doctor`/`wizard`. The freeze test
is one-directional (baseline ⊆ registered), so an accidental **removal** of any
of these user-facing commands would pass CI silently — the exact regression the
baseline exists to stop. *Fix:* add the four to the baseline (kept sorted) and
add `doctor`/`wizard` rows to the matrix. This is an **additive baseline
refresh driven by the registered set**, not a freeze-masking hand-edit.

**Verified clean (no action):** generated `main.go.tmpl` references only real
symbols (`nucleus.New`, `AppBuilder.{FromConfigFile,WithoutDefaults,Mount,Start}`);
generated `nucleus.yml.tmpl` keys are all registered (flat schema); `mvc/rbac_policy.csv`
matches the authz loader's 4-column deny-override format; `generate`, `openapi`,
`startapp` are fully implemented (not stubs).

---

## 3. Framework findings

**FW-1 · P0 · [verified] — `Router.Resource("")` panics `net/http` at startup.**
`pkg/nucleus/router.go:142` (`joinPath`) returns `""` when prefix+path are both
empty; `Resource("")` then registers the pattern `"GET "`, and
`http.ServeMux.Handle` panics on the empty path. The crash fires for
`Resource("")` in **any** module (a module's `Prefix` is applied at the Mux
level, so the adapter prefix is always `""`). The framework's own reference app
documents the footgun and routes around it
(`examples/mvc_api/internal/notes/module.go:35-39`: "…produces the invalid
pattern \"GET \" … and panics net/http.ServeMux at startup"). *Fix:* floor the
empty result to `"/"` in `joinPath`, and guard `base == ""` in `Resource`.
*Confirm:* §8 cmd 1.

**FW-2 · P1 · [reported] — App-level `Lifecycle.OnShutdown` runs with an unbounded context.**
`pkg/nucleus/nucleus.go:641-647` passes `context.WithCancel(context.Background())`
(no deadline) to `Lifecycle.OnShutdown`. Module `OnShutdown` hooks and DB/telemetry
close *do* get a deadline via `withTimeoutFromConfig` (`pkg/app/app.go:1055-1064`);
only the app-wide hook — the one users put slow I/O in — is unbounded, so it can
hang graceful shutdown past an orchestrator's SIGTERM window. (The backlog's
`wg.Wait()` concern is **refuted**: `cancelServices()` precedes it and each
service selects on `Done()`.) *Fix:* wrap with the same shutdown budget.

**FW-3 · P1 · [verified] — admin-auth DB resolution runs even under `WithoutDefaults()`.**
`pkg/app/app.go:255-271` resolves `admin_auth_database` → `adminAuthSQLDB`
(including a nil-check that errors on a bad alias) **before** the
`if !o.skipDefaults` gate at `:277`. A core-only app that sets a stray
`admin_auth_database` alias — a key meaningless without the admin subsystem —
fails hard at startup, breaking the advertised lightweight-core contract
(`app.go:201-204`). The resolved handle is only consumed by
`attachDefaultSubsystems`, which never runs under `WithoutDefaults`. *Fix:* move
the resolution inside the `!o.skipDefaults` branch (or compute it lazily).

**FW-4 · P1 · [reported] — `SameSite=None` is not coupled to `Secure=true`.**
`pkg/auth/session.go:49-51` sets `Cookie.Secure` and `Cookie.SameSite`
independently; `parseSameSite` maps `"none"` → `SameSiteNoneMode` with no
Secure check and no validation error. `SameSite=None; Secure=false` is dropped
by modern browsers, so a misconfigured deployment gets **no session cookie at
all** (login appears broken) with zero diagnostics. *Fix:* reject (or force
`Secure=true` with a WARN) when `SameSite==none && !Secure`, mirroring the CSRF
module's `validate()` precedent.

**FW-5 · P1 · [verified] — Public REST constants are outside contract protection (freeze-generator bug).**
`pkg/nucleus/router.go:55-68` exports six `ResourceMethod` consts
(`Index, Show, Create, Update, Patch, Destroy`) — every REST controller depends
on them — yet they are **absent** from `contracts/baseline/api_exported_symbols.txt`.
Root cause: `contracts/freeze_test.go` `exportedSymbolsForPackage` captures
package-level untyped consts but its per-type loop never iterates `typ.Consts`,
so **type-associated** consts are dropped. The freeze test is `baseline ⊆
current`, so these constants can be renamed/removed without tripping CI —
contravening CLAUDE.md §7. Same gap hides `auth.{RS256,HS256,ES256}` and
`signals.*` consts. *Fix:* add a `typ.Consts` loop to the generator, then
**regenerate** the baseline (`NUCLEUS_UPDATE_CONTRACT_BASELINE=1 go test
./contracts/...`) — generator-driven, never a hand-edit (§7). *Confirm:* §8 cmd 4.

**FW-6 · P2 · [reported] — CORS can emit `Access-Control-Allow-Origin: *` with `Allow-Credentials: true`.**
`pkg/router/corsmw.go:63-72`: when `allowAll`, `*` is sent and credentials are
added with no guard against the combination; the default posture is permissive
(`router.New` defaults `corsAllowAll=true`). Spec-compliant browsers reject the
pair (not directly exploitable) but it is a silent-misconfig trap. *Fix:* when
credentials are enabled, reflect the specific origin (`Vary: Origin`) or require
an allow-list.

**FW-7 · P2 · [verified-latent] — `joinPath` does not collapse double slashes.**
`pkg/nucleus/router.go:149` does naive `a.prefix + p`. Currently unreachable
(all callers pass `prefix==""`), but the first caller that sets a non-empty
adapter prefix gets `/api/` + `/users` → `/api//users` (a distinct net/http
route). Fix alongside FW-1 with `strings.TrimRight(prefix,"/")+"/"+strings.TrimLeft(p,"/")`.

**Verified sound (good news — no action):** CSRF (`crypto/rand`, AES-256-GCM
with mandatory 32-byte key, constant-time compare, Secure-by-default); bcrypt
cost 12; ADR-007 secret redaction on by default + no secret-valued log keys;
goroutine shutdown paths (health, memory tasks, HTTP server `errCh`) leak-free;
SQL built from model metadata/constants with `?` placeholders (no request-data
concatenation); `html/template` only (no unsafe `text/template`); JWT refuses a
phantom HMAC default. Layering and SPEC §2 non-negotiables: PASS.

---

## 4. Documentation fidelity findings

The single historically-burned class — **Go-version drift — is clean**
(`installation.md:12` correctly says "Go 1.26"; the only other `1.XX` strings
are a legitimate "Go 1.13+ error-wrapping" language reference and a test
fixture). No `goframe`/`GoFrame` brand leakage on the public site. The damage is
in **config schemas** and **non-existent symbols**.

**DOC-1 · P0 · [verified] — The website teaches a nested YAML config schema that does not exist.**
`website/docs/concepts/configuration.md:60-104` (and the config blocks in
`features/auth.md`, `features/observability.md:57-73`, `features/admin.md:30-32`)
present nested blocks — `app:`, `server:`, `session:`, `auth:`,
`observability:`, `admin:`, `multi_tenant:`. The real schema in
`pkg/app/config.go` is **flat**: `host`, `port`, `read_timeout`,
`session_store`, `session_cookie_secure`, `session_cookie_samesite`,
`log_level`, `log_format`, `otlp_endpoint`, `admin_prefix`, `multitenant.*`, …
And `pkg/nucleus/config.go` rejects unknown keys (`ErrUnknownConfigKeys`,
strict in production). A reader who copies the canonical `nucleus.yml`
**fails to boot** (or silently gets all defaults). Keys with **no real
counterpart at all**: `app.name`, `auth.password_hash`, `auth.jwt_secret_env`,
`admin.enabled`, `admin.base_path`, `server.shutdown_timeout`, `session.ttl`,
`observability.otel_enabled`, `observability.otel_service_name`. *Fix:* rewrite
every config block in the flat schema; put any "illustrative, not exhaustive"
marker **inside** the fence.

**DOC-2 · P0 · [verified] — The homepage flagship example does not compile.**
`website/docs/intro.md:44-45` ends the chain with `.Build().Run()`.
`AppBuilder.Build()` returns `(App, error)` (`pkg/nucleus/nucleus.go:437`) and
`nucleus.App` has **no `Run()` method** (the terminal is `AppBuilder.Start()` at
`:453`, or the package-level `nucleus.Run(app)`). The chain is doubly broken
(a `(App, error)` tuple has no methods). *Fix:* terminate with `.Start()` as
`quickstart.md` correctly does.

**DOC-3 · P0 · [verified-by-baseline + reported] — ~20 documented Go symbols do not exist.**
A reader copying any of these gets a compile error:

| Page | Symbol shown | Reality |
|---|---|---|
| `concepts/routing.md:147-150` | `router.HandlerFunc`, `next.ServeHTTP(c)` | type is `router.Handler`; call it directly `next(c)` |
| `concepts/routing.md:176` | `openapi.From(doc)` | use `func() *openapi.Document` provider |
| `concepts/models-and-database.md:205` | `router.TenantFrom(ctx)` | `app.TenantFromContext(ctx)` |
| `features/auth.md:305,308` | `auth.SessionRequired`, `auth.RequireRole`, `a.Sessions` | `a.Session.Middleware()`; no role helper in `pkg/auth` |
| `features/storage-and-tasks.md:122-144` | `tasks.Handler`, `a.Tasks`, `outbox.Publish`, `a.DB.WithTx`, `db.Tx` | `tasks.HandlerFunc`; no `App.Tasks` field; `a.Outbox.EnqueueTx`; `a.DB.Tx(ctx, func(*sql.Tx))` |
| `guides/AUTH_GUIDE.md` (×7) | `auth.JWTClaims`, `GenerateToken`, `router.JWTMiddleware`, `auth.JWTConfig`, `auth.SessionFromContext`, `auth.CheckPasswordHash`/`HashPasswordWithCost`, `authz.NewEnforcer` | `auth.Claims`, `JWTManager.Generate`, `JWTManager.Middleware`, `NewJWTManagerFromKeys`, `auth.ClaimsFromContext`, `auth.CheckPassword`/`HashPassword`, `authz.New` |
| `guides/VALIDATION_GUIDE.md:55,116` | `validate.Struct`, `validate.RegisterValidation` | `validate.Validate`, `validate.RegisterRule` |
| `guides/RATE_LIMITING_GUIDE.md:297` | `router.NewRateLimiter`, `RateLimitConfig` | `router.RateLimitMiddleware` / `WithRateLimit`; `RateLimitOptions` |
| `guides/TESTING_GUIDE.md:435` | `model.ExtractMetadata` | `model.ExtractMeta` |

**DOC-4 · P1 · [verified] — config-key drift in prose/examples.**
`multi_tenant.enabled` (prose + blocks) vs real `multitenant.enabled` (no
underscore); `databases.<alias>.driver`/`.dsn` vs the single real leaf `.url`
(e.g. `url: sqlite://app.db`); `model.ErrNotFound` (`TESTING_GUIDE.md:409`) has
no such sentinel (use `sql.ErrNoRows` / `errors.NotFound`).

**DOC-5 · P2 · [verified] — baseline-coverage gaps (not reader-facing falsehoods).**
`auth.{RS256,HS256,ES256}`, `signals.*` consts, and parts of the
`pkg/outbox` / `pkg/tasks` surface **exist in source** (a reader's copy
compiles) but are **absent from the freeze baseline** — the same generator bug
as FW-5. Route to `contract-guardian`, not to a doc edit.

**Totals:** 31 pages audited (15 website + README + QUICKSTART + 14 guides +
DEVELOPER_MANUAL). ~15 pages carry at least one P0/P1. Worst offenders:
`concepts/configuration.md`, `guides/AUTH_GUIDE.md`, `features/auth.md`,
`features/storage-and-tasks.md`, `intro.md`.

---

## 5. Other / repo-truth findings

**OTH-1 · P2 — Three of four "reference applications" are not real.**
Only `examples/mvc_api` is a tracked Go app (5 `.go` files). `git ls-files`
returns **0 tracked files** for `examples/ecommerce_dashboard` and
`examples/fleetmanager`; on disk they hold thousands of **untracked, gitignored**
files (`frontend/node_modules`, `*.db`, `server.log`) and 0–1 `.go` files;
`showcase_demo` is just stray `app.db`/`server.log`. **No CLAUDE.md §7 breach**
(nothing is committed), but the CLAUDE.md directory map and any docs implying
four working examples overstate reality, and build/test coverage meaningfully
covers only `mvc_api`. *Fix:* either populate or drop the three from the map and
docs; decide their fate when `examples-maintainer` scope (Phase 4) resumes.

**OTH-2 · P2 — Session-state and CHANGELOG drift.**
`.claude/state/HANDOFF.md` and `CURRENT_ITERATION.md` still describe the
admin-bootstrap fix as "PR pending", but it merged as **#81** (`1702770` on
`main`). `CHANGELOG.md` dates `[0.8.0]` as `2026-05-27` while the iteration
archive/release object say `2026-05-28`. Fold both into the first PR that
touches state/CHANGELOG.

---

## 6. Verification responsibility (who runs what)

Per the agreed model: the audit is static; **the maintainer runs the Go/npm
verification and pastes results**. The defects whose grade depends on runtime:

| Finding | Why runtime is needed | Command |
|---|---|---|
| CLI-1, CLI-2 | toolchain auto-upgrade + proxy resolution are environment-dependent | §8 A, B |
| CLI-3 | generated-code compile error surfaces only on build | §8 C |
| FW-1 | confirm the panic + the fix | §8 cmd 1 |
| FW-5 | confirm baseline regen surfaces the consts | §8 cmd 4 |
| All doc fixes | `docs-content-verifier` re-run + `npm run build` (Docusaurus) | §8 D, E |

Everything else (symbol/key existence, schema shape, file:line) is `[verified]`
statically and needs no runtime.

---

## 7. Remediation roadmap (PR-sized blocks)

Each block → its own branch → PR through the protected-`main` flow
(branch → push → `gh pr create` → wait for **CI Required Gate** green →
`gh pr merge --squash --delete-branch`). Specialist subagents own each block's
output contract (CLAUDE.md §10). Suggested order maximises "functional" first.

**Block 1 — P0 runtime panic.** FW-1 (+ FW-7 latent).
Files: `pkg/nucleus/router.go` (+ a `pkg/nucleus` test mounting a module that
calls `Resource("")`). Owners: `code-reviewer` → `contract-guardian` →
`test-runner`. Verify: §8 cmd 1.

**Block 2 — P0 CLI buildability.** CLI-1 + CLI-2.
Files: `internal/cli/scaffold/templates/_common/go.mod.tmpl`,
`internal/cli/new.go`, `internal/cli/new_build_test.go` (add a published-path
smoke without the local `replace`). Owners: `contract-guardian` + `test-runner`.
Verify: §8 A, B.

**Block 3 — P1 framework safety.** FW-3, FW-4, FW-2.
Files: `pkg/app/app.go`, `pkg/auth/session.go`, `pkg/nucleus/nucleus.go`.
Owners: `security-auditor` + `code-reviewer` + `test-runner`. Verify: targeted
`go test ./pkg/app/... ./pkg/auth/... ./pkg/nucleus/...`.

**Block 4 — P1 contract coverage.** FW-5 (+ DOC-5 falls out of it).
Files: `contracts/freeze_test.go`, then regenerate
`contracts/baseline/api_exported_symbols.txt`. Owner: `contract-guardian`.
Verify: §8 cmd 4. **Do not hand-edit the baseline (§7).**

**Block 5 — P1 CLI correctness + contract.** CLI-3 + CLI-4.
Files: `internal/cli/generate.go` (+ a generate-resource build smoke),
`contracts/baseline/cli_primary_commands.txt`,
`docs/reference/CLI_CONTRACT_MATRIX.md`. Owners: `code-reviewer` +
`contract-guardian`. Verify: §8 C + `go test ./contracts/...`.

**Block 6 — P0 docs (website).** DOC-1, DOC-2, DOC-3 (website subset), DOC-4 (website).
Files: `website/docs/**` (`concepts/configuration.md` flat-schema rewrite first,
`intro.md` `.Start()`, `concepts/routing.md`, `concepts/models-and-database.md`,
`features/{auth,observability,storage-and-tasks,admin}.md`). Owners:
`website-curator` → `docs-content-verifier` (must pass before `UPDATED`).
Verify: §8 D, E.

**Block 7 — P0 docs (internal guides).** DOC-3 (guides) + DOC-4 (guides).
Files: `docs/guides/{AUTH_GUIDE,VALIDATION_GUIDE,RATE_LIMITING_GUIDE,TESTING_GUIDE}.md`,
`docs/reference/DEVELOPER_MANUAL.md`. Owners: `doc-updater` →
`docs-content-verifier`. Verify: §8 D.

**Block 8 — P2 hygiene.** FW-6 (CORS), OTH-1 (examples + CLAUDE.md map),
OTH-2 (state + CHANGELOG date), README go-version cross-check. Owners: mixed.

Dependency notes: Blocks 1–5 are independent of 6–7 and of each other (separate
files). Block 4 should land before Block 7 so the `signals.*` / `auth.*` const
references stop needing manual adjudication by `docs-content-verifier`.

---

## 8. Smoke-test appendix (commands for the maintainer)

```bash
# A — generated-project Go floor (CLI-1)
go run ./cmd/nucleus new demo --template api --module example.com/demo
cd demo && go build ./...        # watch for `requires go >= 1.26.3` or a silent go-line bump

# B — published-path buildability, NO local replace (CLI-2)  [run on a clean machine]
go install github.com/jcsvwinston/nucleus/cmd/nucleus@latest
nucleus new demo2 --module example.com/demo2
cd demo2 && GOFLAGS=-mod=mod go mod tidy && go build ./...

# C — generate-resource compiles (CLI-3)  [inside a module dir]
go run ./cmd/nucleus generate resource Book && go build ./...

# 1 — Resource("") panic (FW-1)
go test ./pkg/nucleus/... -run TestResource   # add a case mounting Resource("")

# 4 — contract baseline regen surfaces typed consts (FW-5)
go test ./contracts/...                                   # expect ResourceMethod gap
NUCLEUS_UPDATE_CONTRACT_BASELINE=1 go test ./contracts/ -run TestContractFreeze_APIExportedSymbols
git diff contracts/baseline/api_exported_symbols.txt      # should gain Index/Show/Create/Update/Patch/Destroy

# D — doc fidelity re-check after edits (whole-repo discipline, §9)
#     (re-run the docs-content-verifier pass over the touched pages)

# E — website builds (Docusaurus)
cd website && npm ci && npm run build

# Full lanes (CLAUDE.md §3)
go test ./...
bash scripts/ci/check_contract_freeze.sh
bash scripts/ci/run_compatibility_harness.sh --enforce-threshold
```

---

## 9. Appendix — corrections to prior assumptions

Two specialist claims were **overturned** during cross-check and are recorded so
they are not re-introduced:

1. A claim that `proxy.golang.org` "returns empty" for the module is
   **unreliable** — that host is egress-blocked from the audit environment, so
   resolution can only be judged by §8 smoke B. The module *does* carry tags
   `v0.5.0`…`v0.8.0` on GitHub.
2. A claim that `ecommerce_dashboard`/`fleetmanager`/`showcase_demo` are "empty
   directories" is wrong; they hold thousands of **untracked** files. The
   correct finding is OTH-1 (untracked local scaffolding; not committed).

Also: the design note that the canonical fluent terminal is `Build().Run()` is
**wrong against the shipped API** — `Build()` returns `(App, error)` and the
terminal is `Start()` (or `nucleus.Run(app)`). This is the root of DOC-2.
