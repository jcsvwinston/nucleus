# v1.0 Gate — what an honest tag still requires

> **Date:** 2026-07-06 · **Current version:** v0.10.0
> **Origin:** Quantum suite Fase 5 ([QADR-0005](https://github.com/jcsvwinston/quantum/blob/main/docs/adr/QADR-0005-secuenciacion-convergencia.md)):
> Nucleus reaches v1.0 first, with Orbit in lockstep as the dogfooding harness.
> **Precedent:** Quark's `docs/V1_GATE.md` — a qualitative, verifiable checklist;
> v1.0 is NOT tagged until every §A item is closed or explicitly waived in §B
> with a commit that documents the decision.
> **Inputs:** full sweep of `API_CONTRACT_INVENTORY.md`, the contract baseline,
> ADR-001..020 follow-ups, `docs/governance/*`, the 2026-06-21 exhaustive audit
> (re-verified against today's tree — several findings are already closed), and
> the exact Nucleus surface Orbit consumes (14 packages, inventoried below).

## Why this document exists

The freeze machinery already works: 17 stable packages (1,492 exported symbols)
under contract-freeze tests, a firewall against third-party type leaks, a
compatibility harness, and per-surface lifecycle tags. What v1.0 adds is a
**promise**: those tags become binding. This gate lists everything that today
would make that promise dishonest — surfaces still marked provisional, debt the
deprecation policy says must be paid first, and known defects on frozen
surfaces. Each item is checkable; none closes by "I thought about it".

## Current standing (verified 2026-07-06)

| Check | Status |
|---|---|
| Contract freeze (17 pkgs, 1,492 symbols) | ✅ green, rebaselined post-ADR-019/020 |
| Firewall (no third-party types on stable surfaces) | ✅ green |
| DB matrix: sqlite/postgres/mysql + mssql/oracle required lanes | ✅ green |
| Runtime/module surface (ADR-010 Phase 4) | ✅ complete |
| Admin extraction (ADR-019) + public SQL ingest (ADR-020) | ✅ shipped; orbit is public and tagged (v0.3.0) |
| Website/scaffold admin story (audit D-WEB) | ✅ closed by #164–#167 (residuals in §A-4) |

---

## §A · Blocking items (close before v1.0)

### A-1 — Disposition of the four non-stable packages
Every package must end v1.0 either **stable (in the baseline)** or **explicitly
outside the v1.0 promise** (documented in the inventory and release notes):

| Package | Today | Decision needed |
|---|---|---|
| `pkg/openapi` | ✅ CLOSED 2026-07-09: re-signed to stdlib + outside the v1.0 promise | Promoting `DocumentProvider` (= `func() *Document`) would have frozen the entire ~40-symbol experimental document model. Instead, v0.11 ships stdlib members — `AppBuilder.WithOpenAPIHandler(pattern, http.Handler)`, `OpenAPISpec.Handler`, `app.App.MountOpenAPIHandler` (the adapter `openapi.Handler(provider)` already existed, so DX cost is one call) — and deprecates the three provider-typed members (DEP-2026-008 + MA-2026-008; removal at v0.12 with deliberate rebaseline). `pkg/openapi` stays experimental, documented outside the v1.0 promise (inventory + release notes). Removal landed in v0.12.0: the three provider-typed members are gone, pkg/app no longer imports pkg/openapi, baseline rebaselined deliberately (-17 symbols across the four removals). |
| `pkg/outbox` | ✅ DECIDED 2026-07-08: **outside the v1.0 promise** (documented) | Nobody has inventoried which "non-essential ergonomics" still need tightening; promoting without that list is freezing blind. Stays `transitional` through v1.0, promotion tracked for v1.x once the inventory exists. Nuance recorded in the inventory: stable `pkg/app.Config` carries `OutboxConfig`, so the config *shape* freezes with pkg/app while the Go surface stays outside the promise — contained (config keys are additive-friendly), unlike the openapi type coupling. |
| `pkg/observability` + `hooks` | experimental | Waiver candidate (§B-1): modules are shielded by the first-party `nucleus.EventBus`; Orbit's only direct use is an optional fallback. Promotion tracked for ~v1.2. |
| `CircuitBreakerSpec/Config` (was transitional-in-stable across `pkg/app`, `pkg/mail`, `pkg/storage`) | ✅ CLOSED 2026-07-07 (slice 3) | Shape declared final and promoted: the 4-field spec (`Enabled`, `FailureThreshold`, `Cooldown`, `HalfOpenMaxConcurrent`) is identical across the koanf spec (`app.CircuitBreakerSpec`) and the per-package plumbing configs (`mail`/`storage.CircuitBreakerConfig`) — the layering is deliberate (config surface decoupled from `circuit.Config` and its test-only `Now` field). Inventory markers removed; the 8 `*_circuit_breaker.*` registry keys promoted to `stable`. Symbols were already in the freeze baseline. |

**Closed when:** the inventory shows no `transitional` tags inside stable
surfaces, and every experimental package is either promoted or listed under
"outside the v1.0 promise" in the release notes.

### A-2 — Deprecation debt paid ✅ CLOSED 2026-07-09 (removals landed in v0.12.0)
Per `docs/governance/DEPRECATION_TEMPLATE.md` discipline, v1.0 must not ship
with one-release aliases still alive. All three are now removed (v0.12.0),
registry + migration assistants updated, and the freeze baseline rebaselined
deliberately (-17 symbols, -2 key patterns across the train's removals):

- `admin_rbac_policy_file` → `rbac_policy_file` (DEP-2026-004 gates removal at
  **v0.12.0** — which sequences the release train: v0.11 → v0.12 → v1.0).
  *WARN verified 2026-07-07: one-time startup WARN in `resolveRBACPolicyFile`.*
- Legacy flat storage keys `storage_driver`/`storage_path` (superseded by
  nested `storage.*`). *WARN added 2026-07-07 (slice 5 prep): the fallback
  consumed them silently; `warnLegacyStorageKeys` now emits the one-time WARN
  on deviation from the DefaultConfig values, and DEP-2026-005 + MA-2026-005
  formalize the notice. Removal at v0.12 must also migrate the
  `toStorageConfig` fallback, `DefaultConfig` seeding, and the
  `doctor`/`health` CLI reads.*
- `tasks.NewJSONTask` (already error-stubbed; delete). *Verified 2026-07-07:
  returns a deprecation error unconditionally.*

**Closed when:** the three are removed, config registry + migration assistant
updated, freeze baseline rebaselined deliberately.

### A-3 — `auth.CookieSessionStore` ✅ CLOSED 2026-07-09: removed in v0.12.0
Frozen, exported, and never functional — `CommitCtx` encrypts and discards
the payload because the `SessionStore` contract cannot see the HTTP response
(architectural, not a bug); `session_store=cookie` was never a config value;
enumeration returns empty (`ErrSessionStoreNotIterable` exists because of
it). **Maintainer decision (recorded here per the hard rule): remove.**
v0.11 shipped the `Deprecated:` godoc markers + DEP-2026-006 + MA-2026-006;
v0.12.0 removed type + constructor with the deliberate rebaseline. A
response-aware cookie-session feature may return post-v1.0 under a contract
designed for it.

### A-4 — Documentation residuals on frozen surfaces ✅ CLOSED 2026-07-07
The big doc-sync (#164–#167) closed the website story; the two residuals are
now fixed (gate slice 1):

- Scaffold `_common/README.md.tmpl` no longer claims an in-core `/admin` or
  the removed `admin_bootstrap_*` keys — it points to the Orbit module and
  `modules.orbit.*`; `mvc/rbac_policy.csv` comments no longer reference an
  in-core admin gate (S-1 residual gone).
- `docs/guides/AUTH_GUIDE.md:531` now uses the real `cfg.RBACPolicyFile`
  field (N-4 residual gone; the phantom keys `auth_engine`/
  `auth_jwt_audience` were already gone).

Both greps return empty.

### A-5 — Security defaults at the major
- **CORS:** ✅ DECIDED 2026-07-08: **flip at v1.0.0** (recorded here; no §B
  waiver). ADR-013 R4 scheduled the tightening "for a major version" and
  v1.0 is that major — skipping the first major since the promise would turn
  it into an indefinite deferral. v0.11 ships the one-time startup WARN when
  `cors_origins` is empty (DEP-2026-007 + MA-2026-007); the v1.0.0 release
  branch flips empty→deny with the migration note (explicit
  `cors_origins: ["*"]` keeps allow-all — tested). The credentials half was
  already closed by ADR-014/SEC-1. Closes fully when the flip lands in the
  v1.0.0 branch.
- **`mail.Message.Headers`** (audit N-3): ✅ CLOSED 2026-07-07 (gate slice 1)
  — `Send` now rejects CR/LF in custom header keys/values and blank keys
  (same discipline as `From`/`Subject`); contract documented in godoc and
  `MAIL_GUIDE.md`.

### A-6 — Compatibility SLO measurable again ✅ CLOSED 2026-07-07 (slice 7)
`COMPATIBILITY_SLO.md` requires **fixture-app pass rate ≥95%**; fixture
profiles were removed 2026-05-16 and never returned, leaving the SLO
unmeasurable. Restored: the harness now runs three profiles — `core-build`
(stable-surface compilation, kept from the interim harness), `mvc-api`
(build + tests of `examples/mvc_api` against the current tree, `GOWORK=off`
for determinism), and `showcase-suite` (`examples/showcase_demo` compiled
against the current tree via an ephemeral `go.work`, quark/orbit at released
tags). Of the historical trio, `admin-heavy` is obsolete (ADR-019) and
`plugin-heavy` returns with the plugin examples (ADR-010 Phase 4).
`RELEASE_CHECKLIST.md` §2 updated. Verified: 3/3 profiles pass (100%).

### A-7 — Orbit lockstep harness (QADR-0005)
Orbit consumes 14 Nucleus packages; the Tier-1 surface that must not move:

> `nucleus.Runtime` (Logger, DB/DatabaseHandle(s), Session, Authorizer,
> Storage, Models, Observability, JWT) · `nucleus.EventBus` +
> `nucleus.SQLEvent`/`HTTPEvent` · `app.Extension` + `App` wiring fields ·
> `auth.SessionManager`/`User` · `authz.Enforcer` · `db.DB`
> (Engine/System/SqlDB) · `model.Registry`/CRUD contract ·
> `router.Mux`/`Context` · `storage.Store` · `tasks.Inspector` ·
> `signals.Bus` · `errors` payloads · `observe` ctx helpers.

✅ CLOSED 2026-07-08: the suite CI now also **runs orbit's tests** against
the nucleus the workspace pins — job `orbit-lockstep` in the umbrella's
`integration.yml` (quantum#34, merged). RC procedure documented in the
workflow: a quantum PR bumps the nucleus submodule to the release candidate
and the lane runs orbit's six modules against it before tagging. Any break
in the Tier-1 list forces a coordinated orbit release (lockstep).

---

## §B · Waiver candidates (explicit, or they don't count)

Each requires a documented decision (commit in this file + release notes):

1. **`pkg/observability` stays experimental through v1.0** — shielded by the
   stable `EventBus` facade; promotion tracked ~v1.2 (roadmap Track G).
2. **Driver-level SQL instrumentation** (ADR-018 follow-up): direct
   `db.QueryContext` traffic remains invisible to the live feed until v1.1.
3. **ADR-010 Phase 2+ reserved fields** (`Module.Jobs`, `Webhooks`,
   `Migrations`): ship v1.0 as reserved-shape + boot WARN (decisions R1/R2) —
   the fields are part of the frozen shape, execution arrives later without
   breaking it.
4. **Generator layout unification** (ADR-013 R7): two scaffolding layouts
   coexist; DX work, not surface work.
5. **Oracle reserved-word quoting + dotted-identifier split** (ADR-011
   follow-up in `pkg/model/meta.go`): correctness edge on one engine; document
   as known limitation if not fixed.
6. `inspectdb` wizard table-list placeholder (`internal/cli/wizard.go`).

---

## §C · Suggested slice plan (order matters)

| # | Slice | Size | Unblocks |
|---|---|---|---|
| 1 | ✅ Doc/scaffold residuals (A-4) + mail headers doc-or-sanitize (A-5b) — done 2026-07-07 | S | quick wins, zero API risk |
| 2 | ✅ CookieSessionStore decision + implementation (A-3) — removed in v0.12.0 (2026-07-09) | M | removes the worst frozen-surface lie |
| 3 | ✅ CircuitBreaker spec finalization + promote (A-1d) — done 2026-07-07 | M | cleans stable configs |
| 4 | ✅ `pkg/openapi` coupling resolution (A-1a) + outbox disposition (A-1b) — provider members removed in v0.12.0 (2026-07-09) | M–L | the structural §A item |
| 5 | ✅ v0.11: deprecation WARNs verified; v0.12: removals landed (A-2) — done 2026-07-09 | M | sequencing per DEP-2026-004 |
| 6 | 🚂 CORS default decision (A-5a) — decided `flip at v1.0.0` 2026-07-08; v0.11 WARN shipped, flip rides the v1.0.0 branch | S–M | security posture settled |
| 7 | ✅ Fixture profiles / SLO amendment (A-6) — done 2026-07-07 | M | SLO measurable |
| 8 | ✅ Suite-side pre-tag lane running orbit tests vs nucleus RC (A-7) — done 2026-07-08 (quantum#34) | S | lockstep enforced |
| 9 | `rehearse_rc.sh` full pass + release checklist artifacts → **tag v1.0.0** | — | — |

Nothing here starts implementation by itself: each slice lands as its own PR
train with the iteration loop (architect-reviewer → contract-guardian →
docs-content-verifier where surfaces or docs move).
