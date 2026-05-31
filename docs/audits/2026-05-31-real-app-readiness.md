# Nucleus / GoFrame — Real-App Readiness Review

> Date: 2026-05-31 · Branch: `main` @ `6bf4f0a` (clean) · all 12 CI checks green.
> Context: the 2026-05-29 exhaustive audit is fully remediated and merged
> (#82), followed by ADR-010 layer 5 (#84), examples/CLAUDE.md reconciliation
> (#86) and Go-version doc alignment (#88). This review answers a different
> question: **is the framework ready for people to build real applications on
> it, and what hidden errors will that surface?**
> Method: strictly read-only static analysis (no Go toolchain in the audit
> sandbox); findings verified against source `file:line` + the freeze baseline.
> Runtime confirmations are delegated to the shakedown runbook in §4.

---

## 1. Verdict

**The happy path for a real REST/CRUD app works.** Composition
(`nucleus.New().FromConfigFile(...).Mount(mod).Start()`), the module lifecycle,
controller routing (including the merged `Resource("")` and Patch fixes),
DB/Runtime access, model registration, and the `examples/mvc_api` reference app
all hold together and would compile/boot against `main`.

**The risks are operational surprises and declared-but-inert API surface — not
crashes.** Nothing on the happy path silently fails; the traps are fields the
contract advertises that the runtime doesn't honor yet, plus two run/serve and
security-default mismatches. They won't stop a correctly-built app from serving,
but they *will* cost a real app-builder hours of confusion. Fix them before the
"build many real apps" phase.

---

## 2. Happy path — WORKS (verified statically)

- **Composition & lifecycle** (`pkg/nucleus/nucleus.go` `Run`): normalize →
  validate (semantic/referential/`Requires`) → bind module config (ADR-010 L5)
  → `app.New` → middleware → per-module `Runtime` → register models →
  `OnStart` (sorted) → `mountModule` → config/OpenAPI endpoints → services →
  `core.Run` (real `http.Server`, graceful SIGINT/SIGTERM) → bounded
  `OnShutdown`. Sound.
- **Module contract consumed:** Models (`nucleus.go:744`), Routes/Middleware/
  Prefix (`mountModule`), Requires (`validateModuleRequires`, `nucleus.go:534`),
  DefaultDB, OnStart/OnShutdown. ✓
- **Router / controllers:** `Resource(path, controller, methods)` type-asserts
  the controller per verb and **panics loudly at registration** if a verb's
  interface is missing (not a silent 404); Index/Show/Create/Update/Patch/
  Destroy all wired; `Resource("")` floors to `/`. ✓
- **DB/Runtime:** `rt.DB()` resolves default/named alias, returns `nil` (clean
  `OnStart` error) on misconfig rather than panicking; `model.BaseModel` +
  `AutoMigrate` available (dev convenience; prod = SQL migrations). ✓
- **`examples/mvc_api`:** every framework symbol it uses exists in source +
  baseline; demonstrates model + migration + Resource controller + CRUD +
  config + hermetic test. A faithful, compilable specimen. ✓
- **Generated projects build:** `go.mod` floor `go 1.26` + `toolchain
  go1.26.3`; framework dep pinned to `v0.8.0` (not `latest`); `generate
  resource` codegen compiles; api skeleton boots core-only and serves
  `/healthz`; mvc skeleton boots `/admin` + `/healthz`. ✓

---

## 3. Hidden errors to fix before scaling up

Severity: **P1** = will bite a real app/deployer; **P2** = friction/polish.
Class: **[safe]** = additive/doc fix, no design call; **[decision]** = needs a
design/ADR call (remove-vs-wire, behavior change).

### R1 · P1 · [decision] — `Module.Migrations` is a silent no-op
`pkg/nucleus/module.go:52` (interface) + `:91` (field `Migrations fs.FS`) +
`:139` (accessor) — but **nothing in `Run`/`mountModule` ever reads it** (grep:
zero call sites beyond the accessor). A module that embeds its SQL migrations
expecting boot-time application gets **no tables and no error**; the first query
fails with a confusing "no such table". Consistent with the SQL-first / `nucleus
migrate` stance, but the field's presence is a trap.
*Fix options:* (a) wire it (auto-apply at boot — conflicts with the "no hidden
auto-migrate" principle → ADR); (b) remove `Migrations` from the contract until
wired (deprecation via `migration-assistant`); (c) **cheapest/safe-now:** log a
boot-time `WARN` when a module supplies `Migrations` ("not auto-applied; run
`nucleus migrate up`") and document it.

### R2 · P1 · [decision] — `Module.Jobs` / `Module.Webhooks` run once against a `nil` registry
`pkg/nucleus/nucleus.go:708-733` invokes `spec.Jobs(nil)` / `spec.Webhooks(nil)`
with empty-interface registries (Phase 2+ stubs). A module that writes a
background-job closure sees it "called" but **no scheduler ever runs** — a silent
no-op that looks wired.
*Fix:* same shape as R1 — WARN + doc now; remove-or-implement is the design call.

### R3 · P1 · [decision] — `nucleus serve` ≠ `go run .` for api projects
`internal/cli/serve.go:42` calls `app.New(cfg)` (full defaults), but the **api**
template's `main.go` runs `WithoutDefaults()`. So the same project, run two
"official" ways, behaves differently: `go run .` = core-only, open `/healthz`;
`nucleus serve` = mounts `/admin`, activates default-deny authz (→ surprise
**403** on app routes with no policy), and creates a bootstrap admin user
(one-time password to stderr). 
*Fix:* give `serve` a `--without-defaults`/core-only mode (or detect project
intent), and document that `serve` is full-stack. Small but a behavior decision.

### R4 · P1 · [safe] — CORS is allow-all by default with no config knob
`pkg/router/router.go:90` defaults `corsAllowAll: true`; `app.New` never sets
origins; **there is no `cors_*` key** in `app.Config` or the registry. A real
deployment serves `Access-Control-Allow-Origin: *` on every endpoint and
**cannot restrict it via `nucleus.yml`**. (The FW-6 fix prevents the
`*`+credentials CVE, so it's not exploitable that way — but any origin can read
unauthenticated responses, with no first-class lock-down.)
*Fix (additive):* add `cors_origins []string` (+ `cors_allow_credentials bool`)
to `app.Config`, thread into `router.WithCORSOrigins`, register in
`CONFIG_KEY_REGISTRY.md`, ship a commented line in the templates.

### R5 · P2 · [safe] — `doctor` looks for the wrong RBAC filename
`internal/cli/doctor.go:279` probes `admin_rbac.csv` (and `config/`, `rbac/`
variants) but the mvc scaffold ships `rbac_policy.csv`. It works when the
`admin_rbac_policy_file` config key is present (read first), but remove that key
and both `doctor` and framework auto-discovery (`app.go` `rbacPolicyPath`) miss
the scaffolded file → spurious "no RBAC policy" warning.
*Fix:* add `rbac_policy.csv` to the discovery lists (or rename the scaffold
file to match).

### R6 · P2 · [safe] — admin bootstrap password is under-documented in the scaffold
The mvc template auto-creates an admin user on first boot and writes a one-time
password **to stderr** (`pkg/admin/bootstrap_admin.go`). Correct secure default,
but under systemd/Docker (stderr → journald) it's easily missed, and the
generated `nucleus.yml`/`README` never mention it.
*Fix:* README note + a comment on `admin_bootstrap_password` in the mvc
`nucleus.yml`.

### R7 · P2 · [decision] — `generate resource` layout ≠ `mvc_api` layout
`nucleus generate resource` writes `internal/{models,controllers,services,
repositories,contracts}/` (layered), but the only worked example uses
`internal/<feature>/` (feature-folder: `internal/notes/`). A real app following
the example then running the generator gets two competing conventions.
*Fix:* pick the canonical layout and align the generator (or document both).

### R8 · P2 · [safe] — `mvc_api` config uses CWD-relative paths
`examples/mvc_api/config/nucleus.yaml` uses `sqlite://examples_mvc_api.db` and
`main.go` assumes the binary runs from the repo root. Copy the pattern and run
from a Docker `WORKDIR`/`systemd` unit and it breaks (wrong DB dir or
"unsupported scheme").
*Fix:* note the path assumption in the example/docs.

---

## 4. Real-app shakedown runbook (run on a Go 1.26 + network machine)

Run from the repo root. This deliberately exercises the paths most likely to
surface runtime/hidden errors. Capture anything that deviates from "expect".

```bash
# 0. Baseline: framework itself is green
go vet ./... && go test ./...

# 1. api skeleton boots core-only and serves /healthz
nucleus new probe --template api --module example.com/probe
( cd probe && go mod tidy && go run . & )           # note the PID
curl -fsS localhost:8080/healthz                     # expect 200 {"status":"healthy",...}

# 2. mvc skeleton: admin gated, healthz open, default-deny on app routes
nucleus new site --template mvc --module example.com/site
( cd site && go mod tidy && go run . 2>boot.log & )
curl -i localhost:8080/healthz                       # expect 200
curl -i localhost:8080/admin/                        # expect admin login (not 403)
curl -i localhost:8080/                              # expect 403 (default-deny) — know this
grep -i password site/boot.log                       # R6: one-time admin password on stderr

# 3. R3 — serve vs go run divergence (api project)
( cd probe && nucleus serve --config nucleus.yml & )
curl -i localhost:8080/healthz                       # 200
curl -i localhost:8080/anything                      # if 403 + admin user appears in app.db -> R3 confirmed

# 4. migrate on a fresh project
( cd site && nucleus migrate --config nucleus.yml --migrations migrations up )   # "Migrations applied"
( cd site && nucleus generate migration add_widgets ) # writes *.up.sql/*.down.sql; edit then `migrate up`

# 5. generate resource -> build -> live endpoint
( cd site && nucleus generate resource Widget --out . && go build ./... )
#   then mount the generated handler (see internal/controllers/widget_handler.go) and:
curl -s localhost:8080/widgets/                      # expect {"data":[],"count":0}

# 6. R1 — module migrations no-op: put a .sql in a module's Migrations fs.FS,
#    boot, then query its table -> expect "no such table" (confirms silent no-op)

# 7. R4 — CORS allow-all default
curl -i -H 'Origin: https://evil.com' localhost:8080/healthz   # expect Access-Control-Allow-Origin: *

# 8. ops introspection
nucleus routes --config nucleus.yml                  # expect /healthz, /admin/*, your routes
nucleus doctor --config nucleus.yml                  # R5: RBAC check; remove admin_rbac_policy_file to see the miss
```

A real first app to build as the true shakedown: a 2–3 resource API (e.g.
`authors` + `books` with a relation) using the `mvc_api` shape — it exercises
multi-model migration, relations, validation, and `Resource` dispatch in a way
the single-resource example does not.

---

## 5. Recommended remediation

**Safe batch (do now, additive/doc — one branch/PR each or grouped):**
R4 (CORS config key), R5 (doctor filename), R6 (admin-bootstrap doc), R8
(example path note), and the WARN guards for R1/R2/R3 so inert/divergent
surface fails loud instead of silent.

**Design batch (needs your call, likely an ADR each):**
R1/R2 — remove the inert `Migrations`/`Jobs`/`Webhooks` fields until wired, vs
implement them; R3 — how `serve` should treat core-only projects; R7 — the
canonical generated-project layout.

Each remediation goes through the specialized subagents (CLAUDE.md §10) and the
protected-`main` PR flow, with the §4 runbook as the acceptance check.
