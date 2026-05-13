# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

Cerrar las 4 discrepancias de severidad alta entre `website/docs/` y el código
de Nucleus (D1, D3, D5, D8 del informe `docs/audits/2026-05-12-enterprise-readiness.md`)
y blindar el repo contra futuras divergencias añadiendo dos tests de paridad
(CLI ↔ doc y endpoints ↔ doc) que corran en CI.

## Scope

### In

- **D1 — Comando `nucleus i18n …` ficticio.**
  Corregir la doc (`website/docs/cli/overview.md:72-73`) para que cite los
  comandos reales `makemessages` y `compilemessages` registrados en
  `internal/cli/root.go:21,36`. No renombrar el binario.
- **D2 — Comando `nucleus contenttype list` ficticio.**
  Doc reemplazada por el comando real `remove_stale_contenttypes` o eliminada
  de la tabla. Misma edición que D1.
- **D3 — Endpoint `/healthz` documentado pero no expuesto.**
  Implementar handler real en `pkg/app/` que se registre por defecto en
  `App.New`, equivalente al que ya se inyecta en proyectos generados
  (`internal/cli/new.go:344,943`). Debe agregar DB ping + Redis ping (si
  configurado) + storage ping. Mantener `/api/health` del admin tal cual.
- **D5 — "Rate-limit per-tenant" prometido en `README.md:60-63`.**
  Derivar la clave del limiter desde `RequestScopeFromContext` cuando
  multi-tenancy está activado (`pkg/router/ratelimit.go:180-185`). Mantener
  fallback a user_id / IP cuando no haya scope. Sin esto el reclamo se queda
  en el README.
- **D8 — `quickstart.md` usa `.AutoMigrate()` sin advertir SQLite-only.**
  Añadir admonición explícita en `website/docs/getting-started/quickstart.md`
  citando `pkg/db/migrate.go:25-28` y `pkg/app/app.go:107-110`. Apuntar al
  comando `nucleus migrate` como alternativa multi-driver.
- **Paridad CLI ↔ doc.**
  Test en `contracts/` (o `internal/cli/`) que parsee
  `website/docs/cli/overview.md`, extraiga cada token `nucleus <cmd>` y
  verifique que existe en `internal/cli/root.go`. Falla CI si la doc cita un
  comando inexistente. Cubre D1, D2 y D4 (cifra "34 commands") permanentemente.
- **Paridad endpoints ↔ doc.**
  Test que monte el router mínimo en memoria y verifique que las rutas
  citadas en `website/docs/features/observability.md:62` y
  `website/docs/getting-started/quickstart.md:35` responden 200/204. Cubre D3.

### Out

- JWT con rotación de claves, JWKS, RS256/ES256 (iteración separada).
- Default-deny automático en Casbin (iteración separada).
- Endpoint `/metrics` Prometheus en el core.
- Circuit breakers.
- Drift detection de migraciones.
- Adaptadores de dialecto para migraciones (`AutoMigrate` multi-driver real).
- Limpieza de `.DS_Store` versionados, `cmd/goframe/` vacío, Dockerfile
  desalineado con `go.mod` (PR aparte de "hygiene").
- Cualquier cambio en `pkg/admin/ui/`.

## Acceptance criteria

- [x] `website/docs/cli/overview.md` no cita ningún comando que falte en
      `internal/cli/root.go` (verificado por el test de paridad CLI).
- [x] El test de paridad CLI vive en `contracts/` o `internal/cli/` y se
      ejecuta en `.github/workflows/ci.yml` como parte de `go test ./...`.
- [ ] `App.New` registra handler `/healthz` cuando el router está habilitado.
      Responde 200 si DB+dependencias OK, 503 si falla cualquiera.
- [ ] El test de paridad endpoints verifica que `/healthz` responde 200
      contra una app mínima en memoria (driver `sqlite` `:memory:`).
- [x] `pkg/router/ratelimit.go` deriva la clave desde
      `RequestScopeFromContext` cuando hay tenant; sin tenant, fallback a
      user_id/IP. Un test unitario en `pkg/router/ratelimit_test.go`
      demuestra que dos requests del mismo user_id con tenants distintos no
      comparten cubo. *(Implementado en `pkg/router/router_test.go`; el
      plumbing va por `observe.CtxWithTenantID` en lugar de `RequestScopeFromContext`
      directo para evitar ciclo de import `pkg/router → pkg/app`.)*
- [ ] `website/docs/getting-started/quickstart.md` incluye admonición
      `:::warning` explicando que `.AutoMigrate()` solo funciona con SQLite
      hoy.
- [ ] `CHANGELOG.md` tiene entrada bajo `Unreleased / Fixed` referenciando
      D1/D2/D3/D5/D8 y al informe en `docs/audits/`.
- [ ] `go vet ./... && go test ./...` siguen verdes; CI pasa en la PR.

## Status

### Done
- 2026-05-12 — Auditoría completa (`docs/audits/2026-05-12-enterprise-readiness.md`)
  e informe ejecutivo identificando 10 discrepancias doc↔código y 13 brechas
  para "enterprise class".
- 2026-05-12 — **D1 + D2 cerrados** vía [#32](https://github.com/jcsvwinston/nucleus/pull/32):
  - `website/docs/cli/overview.md` ya no cita `nucleus i18n …`, `nucleus contenttype list`
    ni el namespace ficticio `nucleus fixtures …`; añade `nucleus findstatic` que faltaba.
  - `README.md` baja de "34 lifecycle commands" a "37" para coincidir con `commandSpecs`.
  - Guardia permanente: `contracts/cli_doc_parity_test.go` parsea cada `nucleus <token>`
    en el overview y verifica que existe en `internal/cli/root.go` o como alias
    Django-style. `cli.ContractAliasCommandNames()` añadida como accessor exportado.
- 2026-05-13 — **D5 cerrado** vía [#33](https://github.com/jcsvwinston/nucleus/pull/33):
  - `rateLimitKeyFromRequest` ahora antepone `tenant:<id>|` cuando hay tenant resuelto,
    así que dos requests con mismo `user_id` y distintos `tenant_id` van a buckets
    distintos. Plumbing: nueva par `observe.CtxWithTenantID` / `TenantIDFromCtx` para
    cruzar el límite `pkg/app` → `pkg/router` sin ciclo; `requestScopeResolver.Middleware`
    espeja `scope.Tenant` en el contexto de `observe`; `observe.WithContext` añade
    `tenant_id` al logger cuando está presente.
  - Test de regresión: `TestRateLimitMiddleware_SameUserDifferentTenantsHaveSeparateBuckets`
    + dos tests de unidad para las nuevas claves.

### In progress
- (nada — siguiente slice: D3 `/healthz` handler + endpoints-parity test, o D8 `AutoMigrate` admonition.)

### Blocked
- (none)

## Files of interest

- `docs/audits/2026-05-12-enterprise-readiness.md` — informe completo.
- `internal/cli/root.go` — fuente de verdad de comandos CLI registrados.
- `internal/cli/i18ncommands.go`, `internal/cli/contenttypecommands.go` — contexto D1/D2.
- `pkg/app/app.go:107-110, 245-310, 556-564` — wiring de `App.New`, AutoMigrate, RBAC.
- `pkg/db/migrate.go:25-28` — comentario "AutoMigrate intentionally unsupported".
- `pkg/router/ratelimit.go:115-260` — implementación a extender para D5.
- `pkg/app/requestscope.go:77-329` — `RequestScopeFromContext` (origen del tenant).
- `pkg/admin/panel.go:403` — `/api/health` del admin (referencia, no tocar).
- `internal/cli/new.go:338,344,943` — handler `/health` del scaffold a promover al core.
- `website/docs/cli/overview.md` — destino del fix de D1/D2.
- `website/docs/features/observability.md:62`, `website/docs/getting-started/quickstart.md:35` — entradas que prometen `/healthz`.
- `website/docs/getting-started/quickstart.md:45-67` — bloque de `.AutoMigrate()` a anotar.
- `.github/workflows/ci.yml` — donde garantizar que los nuevos tests corren.

## Notes / decisions log

- 2026-05-12 — Decisión: ante cada discrepancia doc↔código, preferir
  **arreglar el código** cuando el reclamo del README es razonable y la
  brecha es de plumbing (D3 `/healthz`, D5 rate-limit per-tenant). Preferir
  **bajar la doc a la realidad** cuando el reclamo es inventado y el código
  no lo motiva (D1 `nucleus i18n …`, D2 `contenttype list`). D8 es solo
  advertencia en doc — implementar `AutoMigrate` multi-driver es trabajo de
  otra iteración.
- 2026-05-12 — La cifra "34 lifecycle commands" en `README.md:31` se
  cambiará a "37" o se reemplazará por "see `nucleus help`" en esta misma
  iteración (no la cubre el test de paridad porque no vive en `website/`,
  pero conviene cerrarla por higiene).
- 2026-05-12 — Track D cerrado por [#30](https://github.com/jcsvwinston/nucleus/pull/30) + [#31](https://github.com/jcsvwinston/nucleus/pull/31); verificado que D1/D2/D3/D5/D8 siguen vigentes contra `main` post-Track-D: `pkg/db/migrate.go:25` mantiene `AutoMigrate intentionally unsupported`, `pkg/router/ratelimit.go` no deriva clave por tenant, `pkg/app/app.go` no registra `/healthz`, `website/docs/cli/overview.md:72-74` sigue citando comandos inexistentes.
