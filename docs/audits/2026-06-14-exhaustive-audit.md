# Nucleus / GoFrame — Exhaustive Audit (executed lanes)

> Date: 2026-06-14 · Branch at audit time: `main` @ `64d28dd` (working tree
> **clean**).
> Triggered by: the scheduled `auditora` task — exhaustively re-verify the
> framework's correctness, hunt for bugs / vulnerabilities / falsehoods in
> **code and docs**, and position Nucleus against the "best web framework on
> the market" bar. Explicit standing instruction: verify against the **code**,
> not against what the project documents about itself.
> Status: **AUDIT COMPLETE. Remediation not started** (report-only run; per the
> protected-`main` workflow, fixes land via branch → PR by the maintainer).
> Predecessor: `docs/audits/2026-06-07-exhaustive-audit-v2.md` (pre-v0.9.0).
> This pass re-grades that report's findings against the shipped v0.9.0 +
> the post-v0.9.0 `Unreleased` batch, and adds first-hand findings.

---

## 0. Scope, method, environment

Lanes executed this pass:

1. **Functional** — `go build` / `go vet` / `go test` on the root module.
2. **Contracts** — freeze + firewall tests; compatibility harness.
3. **Concurrency** — `go test -race` on the goroutine-bearing packages and the
   request path.
4. **CLI** — compiled `nucleus` binary smoke-tested end to end + a scaffold →
   build → generate proof.
5. **Security** — static review of CORS, mail, sessions, auth/admin login,
   CRUD injection guard, multi-tenant DB resolution; one runtime CORS harness.
6. **Docs faithfulness** — §9 body-content verification (Go symbols vs the
   freeze baseline, YAML keys vs `CONFIG_KEY_REGISTRY.md`, Go-version pins vs
   `go.mod`) across 40 internal + public pages.

**Environment — the Go lane actually executed.** Go 1.26.4 (linux/arm64),
the maintainer's exported module cache served offline (`GOMODCACHE` +
`GOPROXY=off`), build cache on local disk, `GOWORK=off`, `GOTOOLCHAIN=local`,
`GOSUMDB=off`. Root module (`pkg/*`, `internal/*`, `cmd/nucleus`, `contracts`,
`examples/mvc_api`) was built, tested, and race-tested. The `admin/*`
sub-modules and the live DB-matrix engines (PostgreSQL/MySQL/MSSQL/Oracle)
were **not** run locally (no Docker) — they remain `[ci-delegated]` to the
green `CI Required Gate`.

**Confidence:** `[verified]` executed/inspected first-hand this pass ·
`[reported]` agent evidence w/ file:line · `[ci-delegated]` covered by the
required CI gate, not re-run here.

**Severity:** P0 functional break / doc falsehood a reader copies and it fails ·
P1 real defect / contract drift / security gap · P2 hygiene/latent · P3 cosmetic.

---

## 1. Executive verdict

| Question | Verdict |
|---|---|
| **Does the framework build, vet, test, and race-test?** | **YES.** `go build ./...` rc=0, `go vet ./...` rc=0, full `go test ./...` green and `go test -race` green on every goroutine-bearing package and the request path. The only test failure is the documented offline artifact (`TestRunGenerateResourceBuilds` can't `go mod tidy` testify's test-only deps under `GOPROXY=off`); the generated code itself compiles (proved via `replace`). |
| **Are the v2 audit's headline findings fixed?** | **MOSTLY YES, and well.** F-3 (CRUD dialect portability), F-4 (firewall `/vN` blindness), SEC-1 (CORS credentials default), the `OrderBy` SQL-injection vector, the login-timing oracle, `Router.Resource("")`, the `WithoutDefaults` admin bootstrap, `doctor`'s RBAC probe, and CLI-V2-1 (advisory toolchain pin) are all **fixed and verified first-hand**. §2. |
| **Any NEW correctness/security holes?** | **YES — one P1 and two P2.** A **frozen, exported session store that silently does nothing** (`CookieSessionStore`), a **residual CORS wildcard-plus-credentials path** the SEC-1 fix doesn't guard, and **unsanitised custom SMTP headers**. §4. |
| **Are the docs faithful?** | **Website: faithful.** **Internal guides: one P1 falsehood remains** (`AUTH_GUIDE.md` teaches two non-existent config keys + a phantom Go field) plus minor §9 drift. The structural gap persists: body-content §9 is still **not** enforced in CI, so these don't get caught. §5. |
| **Enterprise-class position?** | **Closer than at v2.** The two correctness holes that gated the jump (F-3, F-4) are closed; the security defaults (SEC-1) are flipped. What remains is smaller: one broken opt-in store, two latent footguns, one guide falsehood, and the still-unlanded body-content CI lane. §6. |

**One-line position:** Nucleus is now a **clean-building, clean-testing,
race-clean v0.9.x framework whose v2-era correctness and default-security holes
are genuinely closed** — held back from "best-in-market" by one silently-broken
frozen API, two explicit-misconfiguration footguns the framework could refuse,
and the perennial gap that documentation correctness still isn't machine-enforced.

---

## 2. What the v0.9.0 + Unreleased arc actually fixed — `[verified]`

Credit where due. Each of these was a prior-audit finding; each is confirmed
fixed against the **code** (not the CHANGELOG) this pass:

| Prior finding | Status | First-hand evidence |
|---|---|---|
| **F-3** CRUD `?`-only, breaks on PG/Oracle | **FIXED, comprehensively** | `pkg/model/crud.go` — `execContext`:875 and `queryContext`:883 both call `c.rebind(query)`; the count-estimate `QueryRowContext` paths rebind explicitly (238-252); `FindByID`/`Create`/`Update`/`Delete`/`FindAll` all route through those two helpers. `rebind` maps `?`→`$N`/`@pN`/`:N` per dialect (835-849). |
| **F-4** firewall blind to `/vN` imports | **FIXED** | `go test ./contracts/...` green; `extractImports` now derives the local name from the last non-`vN` path segment; the accidental `*casbin.Enforcer` embed was removed (ADR-015). |
| **SEC-1** CORS credentials default `true` | **FIXED** | `pkg/router/router.go:111` `corsAllowCredentials: false` default; credentials emitted only with an explicit allow-list (ADR-014). *(But see N-2 for the residual.)* |
| **`OrderBy`** raw concat → SQL injection | **FIXED, robust** | `pkg/model/crud.go` `SanitizeOrderBy`:701-739 — comma-split, ≤2 tokens/clause, column resolved against known columns only, direction ∈ {asc,desc}, clause rebuilt from allow-listed tokens. `"id; DROP …"` → rejected. |
| **Login timing oracle** (fleetdesk #17) | **FIXED** | `pkg/admin/default_auth.go:166-168` — unknown-user branch runs `auth.CheckPassword(password, dummyPasswordHash)` (cost-12 dummy) and discards the result, equalising wall-clock with the found branch. |
| `Router.Resource("")` startup panic | **FIXED** | `joinPath` floors empty joins to `/`; regression test present. |
| `WithoutDefaults` bootstrapped admin | **FIXED** | bootstrap guarded by `!skipDefaults`; verified live on the `api` scaffold (zero admin output). |
| `doctor` couldn't find `rbac_policy.csv` | **FIXED** | verified live — `doctor` → `✓ rbac  RBAC policy file found at rbac_policy.csv`. |
| **CLI-V2-1** scaffold pinned advisory `toolchain go1.26.3` | **FIXED** | `internal/cli/new.go:144-145` `scaffoldGoVersion="1.26.4"`, `scaffoldToolchain=""`; scaffolding a project emits `go 1.26.4`, no `toolchain` line. A CI freshness test asserts it tracks `go.mod`. *(But see N-6 — the v0.9.0 CHANGELOG entry still describes the old behaviour.)* |
| `generate resource` non-compiling / multi-entity unsafe | **FIXED** | scaffolded a project, generated two resources, `go build ./...` rc=0. |
| BindForm was a stub (no typed binding / validation) | **FIXED + hardened** | `pkg/router/bind_form.go` — reflection-based typed binding + `validate` parity + a **mass-assignment guard** (`isServerOwnedField` skips `db:"pk"`/`readonly`/`autoCreateTime`/`autoUpdateTime`, recursing into the embedded `BaseModel`). |

This is a strong remediation arc: every individually-small-but-real correctness
hole the prior audit named has been closed at the source, not papered over.

---

## 3. Functional, contracts, concurrency, CLI lanes — **PASS** `[verified]`

| Step | Result |
|---|---|
| `go build ./...` | **rc=0** |
| `go vet ./...` | **rc=0** |
| `go test ./pkg/...` | **all PASS** (25 packages) |
| `go test ./contracts/...` | **PASS** (freeze + firewall + harness) |
| `go test ./internal/... ./examples/...` | **PASS** except `TestRunGenerateResourceBuilds` — fails only because the test harness forces `GOPROXY=off` and the offline cache lacks testify's test-only transitive zips (`go-spew`, `go-difflib`). **Not a code defect:** a skip-list run is `ok`, and the generated project compiles end-to-end via a local `replace` (rc=0). |
| `go test -race ./pkg/{signals,circuit,outbox,tasks/...,observability/...,health}` | **all PASS, race-clean** |
| `go test -race ./pkg/{router,auth}` | **PASS, race-clean** |
| CLI smoke (`version`, `new mvc`, `generate resource ×2`, `doctor`, `migrate status`, `openapi`) | **all rc=0**; doctor DEGRADED with sane warnings + `✓ rbac`; openapi exports a 28 KB document; generated `go.mod` pins `v0.9.0` / `go 1.26.4` / no toolchain. |

The build is healthy. No vet diagnostics, no data races on any path exercised.

---

## 4. NEW findings — code

### N-1 · P1 · `[verified]` — `auth.CookieSessionStore` is a frozen, exported, silently non-functional session store

`pkg/auth/session_store_cookie.go`. `CommitCtx` (99-128) marshals the session,
encrypts it (AES-GCM), base64-encodes it — **and then throws it away**:

```go
// Store the encrypted data in the session using the token as key
// This allows the middleware to read it and set it as a cookie
_ = encoded // In a real implementation, this would be set as a cookie   // line 126
return nil                                                                // reports success
```

Nothing is persisted. `FindCtx` (66-95) then tries to base64-decode and
AES-GCM-decrypt the **scs session token itself** (a random 32-byte token, not a
stored payload), which cannot decrypt → every lookup returns not-found/error.
Net effect: **any data committed to this store is lost, and every read misses.**
An application that wires `sessionManager.SetStore(auth.NewCookieSessionStore(key))`
gets sessions that never persist — login silently never sticks, on every request.

Why this is P1, not P3:

- It is **stable, frozen public API** — `contracts/baseline/api_exported_symbols.txt`
  lines 279 (`func:NewCookieSessionStore`), 294-301 (eight `method:CookieSessionStore.*`),
  375 (`type:CookieSessionStore`).
- Its godoc **actively misrepresents it**: "*CookieSessionStore persists
  sessions in encrypted cookies*" (line 15). It persists nothing. This is a
  falsehood living in shipped code — exactly the audit's mandate.
- There is **no round-trip test** anywhere in `pkg/auth` (grep-confirmed); the
  encrypt/decrypt helpers are unit-correct, which is how a non-functional store
  ships green.

Mitigating factor (caps blast radius, doesn't change the grade): it is **not
config-selectable** — `buildSessionManager` (`pkg/app/app.go:1474-1514`) accepts
only `memory`/`sql`/`redis` and errors on anything else, so a user cannot reach
it through `session_store: cookie`. It is reachable only by calling `SetStore`
directly.

**Fix options (pick one, route through `contract-guardian` since it's frozen):**
(a) implement it for real — but note scs's store contract is server-side
keyed-by-token, so a *true* client-side cookie store needs middleware
cooperation, not just a `Store`; this is a design change, not a one-liner;
(b) make `CommitCtx`/`FindCtx` fail loudly (return an explicit
`"not implemented"` error) so misuse is impossible to miss; or
(c) deprecate + remove the type via the deprecation policy. The status quo —
a frozen API that returns success while doing nothing — is the worst of the three.

### N-2 · P2 · `[verified, runtime-confirmed]` — `cors_origins: ["*"]` + `cors_allow_credentials: true` reflects *any* Origin *with* credentials

SEC-1 (v0.9.0) flipped the credentials **default** to `false` and warns when
credentials are set with an **empty** origins list. It does **not** guard the
**explicit-wildcard** case. Runtime harness (calling the public
`router.CORSMiddleware` with an `Origin: https://evil.example` request):

```
[wildcard + credentials  cors_origins:["*"], creds:true]
  Access-Control-Allow-Origin = "https://evil.example"
  Access-Control-Allow-Credentials = "true"
```

Mechanism: `pkg/router/corsmw.go:33` recomputes `allowAll` from the literal
`"*"` string regardless of the app-level flag; with `AllowCredentials` true the
`if allowAll && !opts.AllowCredentials` branch (71) is false, so it falls to the
reflect-Origin branch (74-75) and then sets `Access-Control-Allow-Credentials:
true` (78-80). Because `allowAll` short-circuits the allow-list loop, **every**
origin "passes". The app layer (`pkg/app/app.go:361-369`) only warns for the
empty-origins case; `cors_origins: ["*"]` with credentials passes through
unguarded.

The irony: app.go's own comment (353-360) gives "*reflecting every Origin with
credentials would let any site read authenticated cross-origin responses*" as
the rationale for the SEC-1 design — yet that exact outcome is reachable via the
most common copy-paste CORS config (`["*"]`).

Severity is P2 (it requires the operator to opt into both `["*"]` *and*
credentials — but that is a very common misconfiguration, and the framework
positions itself as security-by-default). **Fix:** reject `["*"]` (or any
allow-all) combined with credentials at the `pkg/nucleus` load-path validation
(loud boot error), and/or have `CORSMiddleware` drop credentials with a WARN
when `allowAll` is true. A best-in-market framework refuses the footgun rather
than emitting the unsafe headers.

### N-3 · P2 · `[verified]` — SMTP header injection via unsanitised `mail.Message.Headers`

`pkg/mail/mail.go` `validateMessage` (208-245) correctly rejects CR/LF in
`From`/`Subject` and `ParseAddress`-validates every recipient — but **never
validates the custom `Headers` map** (neither keys nor values). The RFC-822
builder then trusts them: `pkg/mail/message.go:23-28` only `strings.TrimSpace`s
the value (which strips *leading/trailing* whitespace but leaves **interior**
CR/LF intact) and never inspects the key:

```go
value := strings.TrimSpace(msg.Headers[key])
...
headers = append(headers, fmt.Sprintf("%s: %s", key, value))   // key + value both unchecked
```

A header value `"x\r\nBcc: attacker@evil.com"` (or a key
`"X\r\nBcc: attacker@evil.com"`) injects an arbitrary SMTP header — silent BCC
exfiltration, recipient spoofing, body smuggling. The interior `\r\n` survives
`TrimSpace` and lands verbatim in the wire message that `smtpSender.Send`
streams to `DATA` (`smtp.go:93-94`).

Mitigating factor: **no framework code routes untrusted input into
`Message.Headers` by default** (the only `mail.Send` call sites are the
operator-run CLI `sendtestemail` and config-sourced webhook headers), so this is
**latent** — it bites an application that sets, say, a user-influenced `Reply-To`
or `List-Unsubscribe`. That keeps it P2, but the framework is the component
assembling the SMTP message, so it owns the sanitisation. **Fix:** in
`validateMessage`, reject any header key that isn't a valid RFC-822 field-name
token and any value containing `\r` or `\n` (mirror the From/Subject guard).

---

## 5. NEW findings — documentation (falsehoods)

A parallel `docs-content-verifier` (§9) pass over **40 pages** (14 guides, 8
reference, README/QUICKSTART/CONTRIBUTING, 15 website). The website is clean;
the internal guides carry the residue.

### N-4 · P1 · `[reported]` — `docs/guides/AUTH_GUIDE.md` teaches two non-existent config keys and a phantom Go field

- **L469** `authz_model_path:` and **L470** `authz_policy_path:` are **not**
  registry keys. Strict loading rejects unknown keys (`ErrUnknownConfigKeys`),
  so a reader who copies this `# nucleus.yml` block **fails to boot**. The
  canonical key is `admin_rbac_policy_file` (`CONFIG_KEY_REGISTRY.md:183`); there
  is no model-path key at all (the Casbin model is built in).
- **L532** `enforcer, err := authz.New(logger, cfg.AuthzPolicyPath)` — the call
  shape is fine (`authz.New(logger, policyPath ...string)` exists) but
  `cfg.AuthzPolicyPath` is **not a field** on `app.Config`; the snippet won't
  compile. Canonical: `cfg.AdminRBACPolicyFile`.

The public-website twin (`website/docs/features/auth.md`) is already correct —
only the internal guide lags. This is the same falsehood class the project has
been fighting (DOC-1/2/3 were fixed; this one slipped through because the §9
sweep is still manual). **Fix:** route through `doc-updater` →
`docs-content-verifier`, replacing the keys with `admin_rbac_policy_file` and
the field with `AdminRBACPolicyFile`.

### N-5 · P3 · `[reported]` — minor §9 drift (would trip a body-content CI check)

- `docs/guides/ERROR_HANDLING.md:434` — prose "Go 1.13+" (floor is 1.26). Fix
  to "Go 1.26+". *(The wrapping it describes works on 1.13+, so the claim is
  harmless in spirit, but it violates the §9 version rule.)*
- `CONTRIBUTING.md:10` — "matches the `go 1.26.3` directive in `go.mod`";
  `go.mod` actually declares **`go 1.26.4`**. The floor (1.26+) is right, the
  cited directive string is wrong. (README/QUICKSTART/DEVELOPER_MANUAL/
  installation.md all correctly say 1.26.4.)
- `docs/guides/MAIL_GUIDE.md:258-259` (`sendgrid_api_key`/`sendgrid_endpoint`),
  `docs/guides/STORAGE_GUIDE.md:605-606` (`s3_bucket`/`s3_region`),
  `docs/reference/PLUGIN_SDK.md:138-147` (`plugins.*`) — show unregistered keys
  in clearly historical ("Before:" / "OLD") or "Proposed" blocks that **lack the
  exact `# deprecated, use …` / `# illustrative` marker** §9 requires. Today
  harmless (the prose frames them), but they will trip the body-content guard
  the moment it lands. Add the markers.

### N-6 · P3 · `[verified]` — the v0.9.0 CHANGELOG misdescribes the shipped scaffold

The v0.9.0 `Changed` entry "*Generated projects pin… declares both a `go 1.26`
directive and a `toolchain go1.26.3` directive*" no longer matches reality:
CLI-V2-1 was fixed (`scaffoldToolchain=""`), and scaffolding now emits
`go 1.26.4` with **no** toolchain line (verified). The code is right; the
historical changelog entry was never reconciled, so a reader diffing the
changelog against actual output sees a contradiction. Add a one-line
`Unreleased` correction (or a note on the v0.9.0 entry).

---

## 6. Still-open carried items (re-confirmed or worth re-checking)

- **C-1 · P3 · `[verified]` — `sanitizeNext` allows same-origin `/admin/../x`
  traversal** (`pkg/admin/default_auth.go:262-275`). Cross-origin redirects are
  correctly blocked (`://` check + `HasPrefix(prefix)`), so this is *not* a
  cross-origin open redirect; it only permits a post-login redirect to an
  arbitrary same-host path via `..`. Low risk, but `path.Clean` + a re-check of
  the prefix would close it. (= v2 SEC-6, still open.)
- **Carried v2 P2s not independently re-confirmed this pass** (re-verify before
  acting): SEC-4 (`X-Forwarded-For` trusted unconditionally in `RealIP` + the
  rate-limiter key → spoofable per-IP limits; needs a trusted-proxy config),
  SEC-5 (admin import uses raw `header.Filename` in storage key/log/JSON →
  log/JSON injection), SEC-2 (admin bootstrap INSERT via `fmt.Sprintf`, config-
  sourced only). None are believed regressed; they were simply out of this
  pass's deep focus.

---

## 7. Enterprise-readiness scorecard (updated from v2)

| Track | v2 (2026-06-07) | Now (2026-06-14) | Note |
|---|---|---|---|
| A — Contract freeze & inventory | DONE | **DONE** | freeze + firewall green; firewall no longer hollow (F-4 fixed). |
| B — Compatibility harness | PARTIAL | **PARTIAL** | still `core-build`-only; fixtures return in v0.9.X. Unchanged. |
| C — Dependency firewall | AT RISK | **DONE** | F-4 fixed; casbin embed removed; leaks adjudicated (ADR-015). |
| D — Enterprise data coverage | ~85% (F-3 gate) | **~95%** | F-3 fixed across all CRUD paths; live-PG CRUD test gated in CI. |
| E — Security & compliance baseline | NOT STARTED | **STARTED** | SEC-1 default flipped, login oracle closed, admin authn at router edge (ADR-016), mass-assignment guard. Residuals: N-2, N-3, CSRF still opt-in, audit sink still in-memory. |
| F — Cloud integration | PARTIAL | **PARTIAL** | unchanged. |
| G — Developer productivity | PARTIAL (strong) | **STRONG** | CLI-V2-1 fixed; scaffold/codegen multi-entity safe; the one remaining DX wart is N-4 (a guide that won't boot/compile). |

**Distance to "best-in-market":** materially shorter than at v2. The remaining
list is (a) N-1 fix/deprecate the broken cookie store, (b) N-2 refuse
wildcard-plus-credentials, (c) N-3 sanitise mail headers, (d) N-4 fix the auth
guide, (e) **finally land the body-content §9 CI lane** so N-4/N-5-class
falsehoods can't recur, (f) the standing Track-E items (CSRF-by-default
posture, persistent audit sink, trusted-proxy config). All PR-sized.

---

## 8. Prioritized remediation roadmap (PR-sized, via protected-`main`)

**Sprint 1 — correctness & safety:**
1. **N-1** — make `CookieSessionStore` either real or loudly unimplemented (do
   not leave a frozen API that returns success while persisting nothing); add a
   round-trip test through `SessionManager`. Route through `contract-guardian`.
2. **N-2** — reject allow-all + credentials at the `pkg/nucleus` load path and
   drop credentials (with WARN) in `CORSMiddleware` when `allowAll`.
3. **N-4** — fix `AUTH_GUIDE.md` keys (`admin_rbac_policy_file`) + field
   (`AdminRBACPolicyFile`) via `doc-updater` → `docs-content-verifier`.

**Sprint 2 — hardening & doc integrity:**
4. **N-3** — validate `mail.Message.Headers` keys/values for CR/LF + token shape.
5. **Land the body-content §9 extension to `scripts/website/check-coverage.sh`**
   and wire a CI lane over `docs/guides/*` + `website/docs/*` — this is the
   single highest-leverage structural fix; it ends the recurring guide-falsehood
   cycle (N-4/N-5).
6. **N-5 / N-6** — Go-version + marker sweep across the three guides and
   CONTRIBUTING; reconcile the v0.9.0 CHANGELOG scaffold entry.

**Sprint 3 — Track E tail:**
7. C-1 (`sanitizeNext` `path.Clean`), SEC-4 (trusted-proxy config), SEC-5
   (upload filename hardening), CSRF-by-default posture decision, persistent
   audit sink.

---

## 9. What held up (don't spend effort here)

Build, vet, the full unit/integration suite, and the race detector are all
green; the freeze/firewall/harness gates pass; the CLI works end to end
(scaffold → generate → doctor → migrate → openapi); the v2 correctness holes
(F-3 dialect portability, F-4 firewall, OrderBy injection) and default-security
holes (SEC-1, login timing) are genuinely closed at the source; the
mass-assignment guard, JWT/bcrypt hardening, tenant-isolation sentinel on
`DBForRequest`, and the OrderBy allow-list are real and working. The core is
healthy and the remediation discipline is visibly effective — the gaps are at
the edges (one broken opt-in store, two misconfiguration footguns, one guide,
and a not-yet-automated doc check).

---

## 10. Limitations

- `admin/*` sub-modules and the live DB-matrix engines (pg/mysql/mssql/oracle)
  were `[ci-delegated]`, not run locally (no Docker). N-2's runtime evidence is
  against the in-process `CORSMiddleware`, which is the exact code the app wires.
- The `-race` sweep covered every goroutine-bearing package and the
  router/auth request path; `pkg/app`/`pkg/admin`/`pkg/model` race runs exceeded
  the sandbox time budget and were left to CI (their non-race suites pass).
- Security review is static (plus one CORS runtime harness); no pentest/fuzzing.
- Benchmarks (`performance-bench`) out of scope.
- The offline build cache lacked testify's test-only transitive zips, so the
  three network-backed scaffold *build-smoke* tests can't run here; the
  generated code's compilation was proved via a local `replace` instead.
