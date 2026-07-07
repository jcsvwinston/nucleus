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
| `pkg/openapi` | experimental, **coupled to the stable builder** (`AppBuilder.WithOpenAPI(pattern, provider openapi.DocumentProvider)`) | The hard one: a stable method referencing an experimental type is not a tenable v1.0 shape. Either promote the minimal `DocumentProvider` contract to stable (and freeze it) or decouple the builder (accept `any` + adapter, or move WithOpenAPI behind an extension). |
| `pkg/outbox` | transitional | Tighten ergonomics now, then promote; or exclude from v1.0 explicitly. |
| `pkg/observability` + `hooks` | experimental | Waiver candidate (§B-1): modules are shielded by the first-party `nucleus.EventBus`; Orbit's only direct use is an optional fallback. Promotion tracked for ~v1.2. |
| `CircuitBreakerSpec/Config` (transitional fields inside stable `pkg/app`, `pkg/mail`, `pkg/storage`) | transitional-in-stable | Decide final field shape now and promote — a stable config struct cannot carry provisional fields into v1.0. |

**Closed when:** the inventory shows no `transitional` tags inside stable
surfaces, and every experimental package is either promoted or listed under
"outside the v1.0 promise" in the release notes.

### A-2 — Deprecation debt paid
Per `docs/governance/DEPRECATION_TEMPLATE.md` discipline, v1.0 must not ship
with one-release aliases still alive:

- `admin_rbac_policy_file` → `rbac_policy_file` (DEP-2026-004 gates removal at
  **v0.12.0** — which sequences the release train: v0.11 → v0.12 → v1.0).
- Legacy flat storage keys `storage_driver`/`storage_path` (superseded by
  nested `storage.*`).
- `tasks.NewJSONTask` (already error-stubbed; delete).

**Closed when:** the three are removed, config registry + migration assistant
updated, freeze baseline rebaselined deliberately.

### A-3 — `auth.CookieSessionStore` (audit N-1, P1, still true today)
Frozen, exported, and not wired into the session lifecycle — opting in
silently degrades to the memory store. Maintainer decision required: **wire it,
deprecate it, or remove it** (removal needs a deprecation entry + migration
assistant per the hard rule). Carried through 3+ audits; a v1.0 freeze would
enshrine a silently non-functional stable symbol.

### A-4 — Documentation residuals on frozen surfaces (verified today)
The big doc-sync (#164–#167) closed the website story; two residuals remain:

- Scaffold `_common/README.md.tmpl` still tells generated projects to "sign in
  at `/admin`" (lines 17/25) and `mvc/rbac_policy.csv` comments reference the
  in-core admin gate (S-1 residual).
- `docs/guides/AUTH_GUIDE.md:531` still references `cfg.AuthzPolicyPath`, a
  field that does not exist (N-4 residual; the phantom keys `auth_engine`/
  `auth_jwt_audience` are already gone).

**Closed when:** both greps return empty and the docs-content-verifier passes.

### A-5 — Security defaults at the major
- **CORS:** ADR-013 R4 deliberately deferred tightening the wildcard default
  "to a major version". v1.0 **is** that major. Decide: flip the default to
  deny (breaking, with migration note) in v1.0, or waive explicitly in §B with
  the next-major commitment restated. Silence is not an option.
- **`mail.Message.Headers`** (audit N-3): sanitize on emit or document the
  trusted-input contract in godoc + guide.

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

The suite CI already **builds** orbit against nucleus main; the gate needs it
to also **run orbit's tests** against the nucleus release candidate before
tagging (a pre-tag lane or a suite-side job). Any break in the Tier-1 list
forces a coordinated orbit release (lockstep).

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
| 1 | Doc/scaffold residuals (A-4) + mail headers doc-or-sanitize (A-5b) | S | quick wins, zero API risk |
| 2 | CookieSessionStore decision + implementation (A-3) | M | removes the worst frozen-surface lie |
| 3 | CircuitBreaker spec finalization + promote (A-1d) | M | cleans stable configs |
| 4 | `pkg/openapi` coupling resolution (A-1a) + outbox disposition (A-1b) | M–L | the structural §A item |
| 5 | v0.11: deprecation WARNs verified; v0.12: removals land (A-2) | M | sequencing per DEP-2026-004 |
| 6 | CORS default decision (A-5a) — in v1.0 or §B waiver | S–M | security posture settled |
| 7 | ✅ Fixture profiles / SLO amendment (A-6) — done 2026-07-07 | M | SLO measurable |
| 8 | Suite-side pre-tag lane running orbit tests vs nucleus RC (A-7) | S | lockstep enforced |
| 9 | `rehearse_rc.sh` full pass + release checklist artifacts → **tag v1.0.0** | — | — |

Nothing here starts implementation by itself: each slice lands as its own PR
train with the iteration loop (architect-reviewer → contract-guardian →
docs-content-verifier where surfaces or docs move).
