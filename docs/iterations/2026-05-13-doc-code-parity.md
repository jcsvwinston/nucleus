# Iteration — Doc ↔ Code Parity (audit 2026-05-12)

> Archived: 2026-05-13
> Branches: `fix/doc-code-parity`, `fix/ratelimit-per-tenant`, `docs/enterprise-readiness-audit`, `feat/healthz-core-handler`, `docs/quickstart-automigrate-warning` (all merged + deleted)
> PRs: [#32](https://github.com/jcsvwinston/nucleus/pull/32), [#33](https://github.com/jcsvwinston/nucleus/pull/33), [#34](https://github.com/jcsvwinston/nucleus/pull/34), [#35](https://github.com/jcsvwinston/nucleus/pull/35), [#36](https://github.com/jcsvwinston/nucleus/pull/36)
> Status: COMPLETE (all acceptance criteria met; CI green on every PR)

---

## Goal

Close the four high-severity discrepancies (D1, D2, D3, D5, D8) identified by
the [enterprise-readiness audit](../audits/2026-05-12-enterprise-readiness.md)
between `website/docs/` and the framework code, and add two **permanent
parity guards** (CLI ↔ doc and endpoints ↔ doc) so future drift is caught
in CI rather than discovered by users.

---

## Scope

### In

- **D1** — `nucleus i18n extract|compile` fabricated in `website/docs/cli/overview.md`.
  Replaced with the real `nucleus makemessages` / `nucleus compilemessages`.
- **D2** — `nucleus contenttype list` fabricated. Replaced with the real
  `nucleus remove_stale_contenttypes` (delete-rather-than-list semantics).
- **Adjacent drift** caught while writing the parity guard:
  - `nucleus fixtures dumpdata|loaddata` namespace (does not exist) → `nucleus dumpdata` / `nucleus loaddata`.
  - `nucleus findstatic` (registered but missing from doc) added.
  - `README.md` "34 lifecycle commands" → "37" (matches `commandSpecs`).
- **D3** — `/healthz` endpoint advertised in `README.md` and
  `website/docs/features/observability.md` but never wired in `App.New`. New
  `pkg/app/healthz.go` registers `GET /healthz` by default; probes every
  entry in `a.DBs` via `db.DB.Health` with a 2 s per-DB timeout. 200 healthy
  / 503 unhealthy with a deterministic JSON body.
- **D5** — README promises rate-limit per-tenant but
  `pkg/router/ratelimit.go` keyed every bucket as `user:<id>`. Now prefixes
  `tenant:<id>|` when a tenant is resolved. Plumbing crosses the
  `pkg/app` → `pkg/router` boundary via a new `observe.CtxWithTenantID` /
  `observe.TenantIDFromCtx` pair.
- **D8** — `quickstart.md` example calls `.AutoMigrate()` against a config
  that could point anywhere; the implementation is SQLite-only. Added an
  explicit `:::warning` admonition citing the source locations and pointing
  at `nucleus migrate` as the multi-driver path.
- **CLI ↔ doc parity guard** (`contracts/cli_doc_parity_test.go`): parses
  every `` `nucleus <token>` `` reference in
  `website/docs/cli/overview.md` and verifies the leading token resolves to
  either a primary command (`cli.ContractPrimaryCommandNames`) or a
  Django-style alias (new `cli.ContractAliasCommandNames` accessor).
  `nucleus help` exempt as a builtin dispatch.
- **Endpoints ↔ doc parity guard**
  (`contracts/endpoints_doc_parity_test.go`): mounts a minimal in-memory app
  via `app.New(cfg, app.WithoutDefaults())` against SQLite `:memory:` and
  verifies every endpoint documented in `observability.md` /
  `quickstart.md` responds with the expected status. Currently covers
  `/healthz` end-to-end through the real router.

### Out

- JWT rotation, JWKS, RS256/ES256.
- Default-deny in Casbin.
- `/metrics` Prometheus endpoint.
- Circuit breakers.
- Drift detection of migrations.
- Adapter-based multi-driver `AutoMigrate`.
- Redis / mail / object-storage probes in `/healthz`. Tracked as follow-up;
  the firewall rules in `contracts/firewall_test.go` flag `redis/go-redis/v9`
  as needing a wrapper, so the right path is a `pkg/health` (or similar)
  package that exposes a `Probe(ctx) error` interface for storage / mail /
  cache backends to implement.
- Hygiene PR (versioned `.DS_Store`, empty `cmd/goframe/`, Dockerfile vs
  `go.mod` alignment).

---

## Acceptance criteria

- [x] `website/docs/cli/overview.md` no cita ningún comando que falte en `internal/cli/root.go` (verificado por el test de paridad CLI).
- [x] El test de paridad CLI vive en `contracts/` y se ejecuta en `.github/workflows/ci.yml` como parte de `go test ./...`.
- [x] `App.New` registra handler `/healthz` cuando el router está habilitado. 200 si DB+dependencias OK, 503 si falla cualquiera.
- [x] El test de paridad endpoints verifica que `/healthz` responde 200 contra una app mínima en memoria (driver `sqlite` `:memory:`).
- [x] `pkg/router/ratelimit.go` deriva la clave desde el contexto cuando hay tenant; sin tenant, fallback a user_id/IP. Test unitario demuestra que dos requests del mismo user_id con tenants distintos no comparten bucket.
- [x] `website/docs/getting-started/quickstart.md` incluye admonición `:::warning` explicando que `.AutoMigrate()` solo funciona con SQLite hoy.
- [x] `CHANGELOG.md` tiene entrada bajo `Unreleased / Fixed` referenciando D1/D2/D3/D5/D8 y al informe en `docs/audits/`.
- [x] `go vet ./... && go test ./...` siguen verdes; CI pasa en cada PR (incluida la gate MSSQL/Oracle promovida en [#31](https://github.com/jcsvwinston/nucleus/pull/31)).

---

## Files of interest

- `docs/audits/2026-05-12-enterprise-readiness.md` — informe que dispara la iteración.
- `internal/cli/root.go` — fuente de verdad para nombres de comandos CLI.
- `internal/cli/aliases.go` — `ContractAliasCommandNames` exportada para el test de paridad CLI.
- `pkg/app/app.go` — wiring de `/healthz` en `App.New`.
- `pkg/app/healthz.go` + `healthz_test.go` — handler y probes.
- `pkg/router/ratelimit.go` + `pkg/router/router_test.go` — clave por tenant.
- `pkg/app/requestscope.go` — espeja `scope.Tenant` en el contexto de `pkg/observe`.
- `pkg/observe/logger.go` — par `CtxWithTenantID` / `TenantIDFromCtx`.
- `contracts/cli_doc_parity_test.go` — guard CLI.
- `contracts/endpoints_doc_parity_test.go` — guard endpoints.
- `website/docs/cli/overview.md`, `website/docs/features/observability.md`, `website/docs/getting-started/quickstart.md` — docs corregidas.
- `README.md`, `CHANGELOG.md` — cifras y release notes sincronizadas.

---

## Notes / decisions log

- 2026-05-12 — Política: ante cada discrepancia, **arreglar el código** cuando el reclamo del README es razonable y la brecha es de plumbing (D3 `/healthz`, D5 rate-limit per-tenant). **Bajar la doc a la realidad** cuando el reclamo es inventado (D1 `nucleus i18n …`, D2 `contenttype list`). D8 es solo advertencia en doc — implementar `AutoMigrate` multi-driver es trabajo de otra iteración.
- 2026-05-12 — Sub-decisión D5: el plumbing del tenant cruza `pkg/app → pkg/router` por `pkg/observe` para evitar el ciclo `pkg/router → pkg/app`. `pkg/observe` ya era el canal de `UserID`, así que el tenant lo monta encima sin nueva infraestructura.
- 2026-05-13 — Sub-decisión D3: el handler probe solo DBs en este PR. Wiring de Redis / mail / object-storage probes requiere un `pkg/health` (o equivalente) que exponga `Probe(ctx) error` por dependencia, para que `pkg/app` no importe directamente `redis/go-redis/v9` (firewall rule en `contracts/firewall_test.go`). Deferred.
- 2026-05-13 — Mantenimiento del repo: borradas 21 ramas remotas stale (`codex-*`, `codex/*`, `claude/priceless-turing-dac190`, `fix/website-trailing-slash`) y 23 ramas locales sin worktree; eliminado el worktree `confident-keller-35e74b` con 28 k líneas de deletes staged WIP; main checkout movido de `fix/website-trailing-slash @ 1cad95f` a `main @ b82bf84`. `origin` ahora solo expone `main`.

---

## Follow-ups carried into future iterations

- `remove_stale_contenttypes` aún no tiene cobertura específica en MSSQL/Oracle exploratory tests (audit D-row tracked in the report, not in this iteration's scope).
- Redis / mail / object-storage probes para `/healthz` — pendiente de `pkg/health` (ver "Sub-decisión D3").
- Cosmetic rename de los jobs `db-matrix-exploratory-{mssql,oracle}` en `ci.yml` (perdieron `continue-on-error: true` en [#31](https://github.com/jcsvwinston/nucleus/pull/31) pero conservan el label "Exploratory").
- Las 13 brechas "enterprise class" del informe (JWT rotation con JWKS, default-deny Casbin, `/metrics` Prometheus, circuit breakers, drift detection de migraciones, hygiene del repo, etc.) siguen abiertas en su totalidad.
