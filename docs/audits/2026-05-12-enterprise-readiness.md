# Auditoría de Nucleus — Madurez Enterprise y Paridad Doc ↔ Código

**Fecha:** 2026-05-12
**Rama auditada:** la del HEAD actual en `/Users/jcsv/GolandProjects/GoFrame/GoFrame`
**Método:** Inspección estática de código fuente, documentación, tests y CI. **No** se ha ejecutado `go build` / `go test` (la sandbox no tiene Go); las afirmaciones operativas se marcan como tales. Cada hallazgo cita ruta y línea para que sea reproducible.

---

## 1. Resumen ejecutivo (TL;DR)

| Dimensión | Estado | Comentario corto |
|---|---|---|
| Madurez global del framework | **Pre-1.0 sólido, no enterprise** | Fontanería buena, pero faltan piezas que un comité empresarial pide de oficio |
| Rigor de la documentación (`website/`) | **Aspiracional, no descriptivo** | Bien estructurada y sin stubs, pero contiene afirmaciones que el código no cumple |
| Higiene del repositorio | **Aceptable con manchas** | `.DS_Store` versionados, `cmd/goframe/` vacío residual del rename, Dockerfile desalineado con `go.mod` |
| Cobertura de tests | **Desigual** | 112 archivos `_test.go` vs 260 fuente. Algunos paquetes críticos casi sin tests (storage 0.10, mail 0.17) |

**Veredicto:** Nucleus está más cerca de "**usable but thin**" que de "enterprise-grade". La descripción del README (`README.md:16-66`) y de `website/docs/intro.md` exagera la madurez real. Para honrar la etiqueta "enterprise" hace falta cerrar 6–8 brechas concretas listadas en §4.

---

## 2. Grado de madurez del framework

### 2.1 Lo que **sí está a nivel productivo**

Verificado en código y con tests asociados:

- **Outbox transaccional con leasing real**, no solo `SELECT … FOR UPDATE`. El claim usa actualización condicional optimista (`pkg/outbox/dispatcher.go:331-425`) con `lease_owner`/`lease_until` y TTL configurable. Re-delivery correcto si el nodo cae. Tests: `pkg/outbox/{outbox,bridges,managed}_test.go`.
- **Background jobs sobre Asynq** con propagación de contexto (request_id, user_id, trace_id, traceparent) inyectada en el payload (`pkg/tasks/providers/asynq/tasks.go:60-210`). 8 archivos de test.
- **Sesiones** apoyadas en `alexedwards/scs/v2` con `HttpOnly` fijado, `RenewToken` anti-fixation explícito y stores múltiples (memoria, SQL, Redis, cookie, memcached) (`pkg/auth/session.go:14-247`, `session_store_*.go`). 6 archivos de test.
- **Graceful shutdown** correcto con `signal.Notify(SIGINT, SIGTERM)` y `srv.Shutdown(ctx)` (`pkg/app/app.go:751-847`); hooks `OnShutdown` ejecutados en orden inverso para DB, telemetría, sesiones, admin, outbox.
- **Timeouts HTTP** completos (Read/Write/Idle) en `pkg/app/app.go:762-768`, defaults en `pkg/app/config.go:332-334` (30s/60s/120s).
- **Migraciones SQL nativas** con tabla `nucleus_schema_migrations`, archivos `.up.sql`/`.down.sql`, transacciones por migración y rollback por pasos (`pkg/db/migrate.go:31-303`).
- **Inspectdb multi-driver** con funciones distintas para SQLite/PostgreSQL/MySQL/MSSQL/Oracle (`internal/cli/inspectdb.go:184-228`, 1.250 líneas).
- **Admin panel React 19 + Vite + Tailwind realmente cableado** contra los endpoints CRUD reales — no es maqueta (`pkg/admin/handlers.go:1-1309`, `pkg/admin/ui/src/services/api.ts:1-393`).
- **CI seria**: `.github/workflows/ci.yml` corre `go mod verify`, tests, `-race`, vet, build, smoke de ejemplo, build del admin UI y `govulncheck`. Hay además workflows de release rehearsal.
- **CLI sin TODO basura**: solo 1 `TODO` en código no-test (`internal/cli/wizard.go:178`), 0 `FIXME/XXX/HACK`, 0 `nolint`. Esto es muy buena señal de higiene.

### 2.2 Lo que está "**usable but thin**"

- **Logging estructurado (`pkg/observe/logger_test.go`, 445 líneas)**: wrapper limpio sobre `slog` pero **sin sampling, sin redacción automática de campos sensibles, sin rotación**. El `request_id` solo aparece si el desarrollador monta `RequestLogger`; **`pkg/app/app.go` no lo monta de oficio**.
- **OpenTelemetry**: SDK bien inicializado con OTLP HTTP, propagadores TraceContext+Baggage, spans HTTP/DB/queue (`pkg/observe/otel.go:30-108`). Pero **`pkg/router/otel.go` (TelemetryMiddleware) no está montado por defecto** en `app.New` — hay que activarlo manualmente.
- **AuthN JWT**: solo HS256 (`pkg/auth/jwt.go:33-146`). **No hay RS256/ES256, no hay JWKS, no hay rotación de claves**, no hay refresh tokens. Para banca/salud/regulado esto no pasa.
- **AuthZ Casbin**: integrado correctamente (`pkg/authz/enforcer.go:16-138`), pero **es opt-in**. Si el archivo `admin_rbac.csv` no existe, `rbacEnforcer == nil` (`pkg/app/app.go:559`) y **no hay default-deny** — el middleware no se añade a las rutas a menos que el usuario lo enganche manualmente.
- **CSRF**: implementación double-token completa (`pkg/router/csrf.go:81-225`), pero **la comparación `submitted != token` (línea 184) no es de tiempo constante** y la `EncryptionKey` por defecto se deriva de `SHA-256(nombreDeCookie)` (línea 64-67). Funciona contra ataques básicos, pero un revisor de seguridad lo marcaría.
- **Validación**: helper bien hecho (`pkg/validate/validate.go:36-106`) pero **no se invoca automáticamente** en el router. El desarrollador tiene que llamarlo tras `BindJSON`.
- **Plugin SDK v1**: envelope JSON sólido, descubrimiento por `PATH` con prefijo `nucleus-plugin-` (`pkg/plugins/plugins.go:61-181`), pero la ejecución es `os/exec` por llamada — costo alto en concurrencia, sin sandboxing.
- **Signals**: bus in-process correcto; el "Redis relay" del README es **pub/sub fire-and-forget**, sin persistencia (`pkg/signals/redis.go:32-300+`). Si un suscriptor está caído, pierde eventos.
- **Migraciones multi-driver**: la tabla y la sintaxis son las mismas para todos los motores. **`AutoMigrate` solo soporta SQLite** (`pkg/app/app.go:76-110`, comentario textual: "In a multi-driver environment, we would use a dialector system here"). No hay detección de drift ni checksum de migraciones aplicadas.

### 2.3 Lo que es **stub / demo-only / faltante**

| Carencia | Evidencia |
|---|---|
| **`/healthz` y `/readyz` HTTP en el core** | `grep -rn "healthz" pkg/` no devuelve handler. Solo `pkg/admin/panel.go:403` registra `/api/health` para el admin, y `internal/cli/new.go:344,943` lo escribe en el *scaffold* generado. El comando `nucleus health` (`internal/cli/root.go:32`) está pensado para probar una URL que el propio framework no expone. |
| **Rate-limit per-tenant** (claim del README) | `grep -n "tenant\|Tenant" pkg/router/ratelimit.go` **devuelve cero coincidencias**. La clave es user_id o IP, nunca tenant (`pkg/router/ratelimit.go:180-185`). |
| **Endpoint Prometheus en la app** | `MetricsPath:"/metrics"` está definido (`pkg/app/config.go:387`) pero **no se registra en ningún router**. `MetricsAddr` aplica al agente admin separado (Connect-RPC), no a la app principal. |
| **Circuit breakers** | No hay `gobreaker`, `sony/gobreaker`, ni implementación propia. Solo retries con backoff exponencial en outbox (`pkg/outbox/dispatcher.go:311-321`). |
| **Schema-swap y row-level multi-tenancy** | Solo está implementada la variante **DB swap** (alias de base de datos por tenant) en `pkg/app/requestscope.go:77-329`. |
| **Drift detection de migraciones** | No hay checksum del contenido aplicado vs archivo en disco. |
| **Redacción centralizada de secretos en logs** | El bootstrap del admin escribe la contraseña generada al log (`pkg/app/app.go:225-228`). Solo el live-view del admin redacta (`pkg/admin/live.go:620-740`). |

### 2.4 Higiene del repositorio (signals débiles)

- **`.DS_Store` versionados**: `pkg/.DS_Store`, `pkg/admin/.DS_Store`, 4 bajo `examples/fleetmanager/`. Deberían estar en `.gitignore`.
- **Dockerfile desalineado**: `Dockerfile:1` usa `golang:1.25-alpine`, mientras que `go.mod:3` requiere `go 1.26.3`. **No compilaría con la imagen oficial actual**.
- **Restos del rename GoFrame → Nucleus**: `cmd/goframe/migrations/` y `cmd/goframe/storage/` están vacíos. `NUCLEUS_RENAME_BRIEF.md` sigue en raíz.
- **Directorios UI scaffolded pero vacíos**: 9 carpetas `pkg/admin/ui/src/features/{rbac,infra,auth,health,network,system,audit,overview,data-studio}/pages/` y 6 más (`stores`, `lib`, `services`, `types`, `components/ui`, `components/layout`) vacías. Crea ruido al revisar la UI.
- **Worktrees `.claude/worktrees/`** con copias parciales del repo inflan los conteos brutos (de 998 `.go` a 260 reales tras excluirlos). No es bug, pero conviene tenerlo presente.
- **Convivencia de `pkg/observability` y `pkg/observe`**: dos paquetes con nombres casi idénticos y propósitos distintos (uno es bus de eventos in-process, el otro es slog+OTel). Confunde y la documentación solo describe el segundo.

### 2.5 Veredicto por dimensiones enterprise

| Dimensión | Nivel observado | Justificación corta |
|---|---|---|
| Observabilidad (logs/traces/métricas) | 🟡 Usable but thin | SDK bien wireado, middleware no autoenganchado, sin Prometheus, sin redacción |
| Seguridad (authN/Z/CSRF/sesión) | 🟡 Usable but thin | Falta default-deny, rotación de claves JWT, comparación de tiempo constante en CSRF |
| Resiliencia (timeouts/retries/CB) | 🟡 Usable but thin | Timeouts y graceful OK; sin circuit breakers; outbox correcto |
| Datos (multi-driver, migraciones) | 🟡 Usable but thin | Conexión e inspect OK; AutoMigrate y migraciones sin dialecto |
| Multi-tenancy | 🟡 Parcial | DB-swap funciona y tiene `ErrTenantIsolationViolation`; nada de schema-swap ni row-level |
| Extensibilidad (plugins/signals) | 🟡 Usable but thin | Envelope sólido; pub/sub Redis sin garantías |
| Admin panel | 🟢 Production-ready | UI cableada al CRUD real, 7 tests Go en `pkg/admin` |
| Operaciones (`/healthz`, métricas) | 🔴 Demo / faltante | El gancho operativo más básico no existe en el core |
| CI/CD y release | 🟢 Sólido | 5 workflows, race tests, govulncheck |
| Higiene de repo | 🟡 Aceptable | `.DS_Store`, Dockerfile desalineado, residuos del rename |

---

## 3. Rigor de la documentación (`website/`)

### 3.1 Estado de fondo

- 15 archivos Markdown bajo `website/docs/`, ≈ 6.041 palabras, Docusaurus 3 + TypeScript.
- **Sin marcadores TODO/FIXME/Coming soon/lorem ipsum** en los docs. No hay páginas-stub.
- **100% en inglés** (el repo y el código sí están en inglés también; no hay mezcla con español).
- Sin frontmatter `last_update:` ni timestamps → no se puede saber a qué versión del código corresponde la doc.

### 3.2 Discrepancias materiales doc ↔ código

Estas son afirmaciones documentadas que **no se sostienen** al inspeccionar el código:

**D1 — Comando inexistente `nucleus i18n extract` / `nucleus i18n compile`.**
- *Doc:* `website/docs/cli/overview.md:72-73`.
- *Realidad:* los comandos reales son `makemessages` y `compilemessages` (`internal/cli/root.go:21,36`; `internal/cli/i18ncommands.go:50,131`). No hay subcomando `i18n`. **Un usuario que copie la documentación obtiene "unknown command".**

**D2 — Comando inexistente `nucleus contenttype list`.**
- *Doc:* `website/docs/cli/overview.md:74`.
- *Realidad:* solo existe `remove_stale_contenttypes` (`internal/cli/root.go:40`, `internal/cli/contenttypecommands.go:16`). No hay listado de content types.

**D3 — Endpoint `GET /healthz` documentado pero no expuesto por el core.**
- *Doc:* `website/docs/features/observability.md:62`, `website/docs/cli/overview.md:27`, `website/docs/getting-started/quickstart.md:35`.
- *Realidad:* `grep -rn "healthz" pkg/` no halla handler. El framework solo registra `/api/health` dentro del admin (`pkg/admin/panel.go:403`) y `internal/cli/new.go:338,344,943` inyecta `/health` en el *scaffold de proyectos generados*. El `nucleus health` CLI existe pero apunta a una URL que el propio framework no levanta — y el quickstart le promete al usuario que esa URL responde.

**D4 — "34 lifecycle commands" en el README.**
- *Doc:* `README.md:31`.
- *Realidad:* 37 comandos registrados en `internal/cli/root.go:19-55` (`grep -cE "^\s*\{name:" internal/cli/root.go`). Discrepancia menor pero indica que la cifra no se ha vuelto a contar al añadir comandos.

**D5 — "Rate limiting per tenant".**
- *Doc:* `README.md:60-63` ("per-tenant rate limiting").
- *Realidad:* `pkg/router/ratelimit.go` **no contiene la palabra "tenant" en ninguna línea**. La clave es user_id o IP (`pkg/router/ratelimit.go:180-185`). Es un claim falso.

**D6 — "Tracing automático en HTTP" subentendido en `features/observability.md`.**
- *Doc:* `website/docs/features/observability.md` (líneas alrededor de spans HTTP).
- *Realidad:* el SDK se inicializa, pero `TelemetryMiddleware` (`pkg/router/otel.go:24-65`) **no se monta de oficio** en `app.New`. Sin que el desarrollador lo active, no hay spans HTTP server.

**D7 — Inconsistencia de clave RBAC en el propio sitio.**
- En `concepts/configuration.md` aparece como `admin.rbac_policy_file`.
- En `features/admin.md` aparece como `admin_rbac_policy_file`.
- El código carga el archivo desde 3 rutas distintas (`pkg/app/app.go:556-564, 1293-1311`), una de ellas `admin_rbac.csv`. La doc no aclara cuál se considera canónica.

**D8 — `quickstart.md` usa `.AutoMigrate()` sin advertir que solo funciona en SQLite.**
- *Doc:* `website/docs/getting-started/quickstart.md:45-67` (ejemplo `nucleus.New().Port(...).SQLite(...).Model(...).AutoMigrate()...`).
- *Realidad:* `pkg/db/migrate.go:25-28` indica que `db.AutoMigrate` **no está soportado** ("intentionally unsupported"). El builder `pkg/nucleus/nucleus.go:196-212` sí ofrece `.AutoMigrate()`, pero internamente solo construye scaffold SQLite (`pkg/app/app.go:107-110`, `model.BuildSQLiteMigrationScaffold`). Si el usuario cambia a Postgres tras el quickstart, falla.

**D9 — Paquetes documentados como existentes pero sin página propia.**
- Sin página dedicada: `pkg/errors`, `pkg/validate`, `pkg/signals`, `pkg/plugins`, `pkg/openapi`, `pkg/outbox`, `pkg/mail`, `pkg/observability` (distinto de `pkg/observe`).
- Sin embargo, el intro promete cobertura amplia. Un usuario que llega a `pkg/signals` o `pkg/openapi` no encuentra documentación en el sitio (solo en `docs/` interno y en el README).

**D10 — Binarios mencionados vs presentes.**
- *Doc:* solo documenta `cmd/nucleus`.
- *Realidad:* existen `cmd/goframe/` (residuo del rename) y `cmd/nucleus/`. No se documenta qué hacer con el primero (la respuesta correcta es borrarlo).

### 3.3 Lo que la documentación hace bien

- Estructura limpia (intro → getting-started → concepts → features → architecture → CLI).
- Sin páginas-stub. La más corta es `installation.md` con 214 palabras.
- Los ejemplos de código son sintácticamente válidos y, en su mayoría, sí compilan contra las APIs reales (la inconsistencia es en sub-comandos CLI y en endpoints expuestos, no en imports de paquetes).
- ADRs versionados en `docs/adrs/ADR-00{1,2,3}.md` que el sitio referencia.
- `architecture/compatibility.md` describe los tests de contrato (`contracts/freeze_test.go`, `contracts/firewall_test.go`, `contracts/baseline/*.txt`) que existen y son verificables.

### 3.4 Severidad de las discrepancias

| Discrepancia | Severidad | Riesgo para un usuario |
|---|---|---|
| D1 (`i18n extract/compile`) | **Alta** | Quickstart roto si sigue la doc literal |
| D2 (`contenttype list`) | Media | Comando documentado falla |
| D3 (`/healthz`) | **Alta** | Health checks en orquestador apuntan a URL que no existe |
| D4 (34 vs 37 comandos) | Baja | Solo confusión |
| D5 (rate-limit per-tenant) | **Alta** | Reclamo de seguridad falso para multi-tenant |
| D6 (tracing auto) | Media | Producción "sin traces" sin darse cuenta |
| D7 (inconsistencia RBAC) | Baja | Confusión interna |
| D8 (`AutoMigrate` SQLite-only) | **Alta** | Falla al migrar a Postgres/MySQL siguiendo quickstart |
| D9 (paquetes sin doc) | Media | Cobertura aspiracional vs real |
| D10 (`cmd/goframe`) | Baja | Higiene |

---

## 4. Brechas concretas para alcanzar "Enterprise class"

Lista priorizada, todas con evidencia en código:

1. **Exponer `/healthz` y `/readyz` en el core**, con chequeo agregado de DB + Redis + storage + outbox. Hoy solo existe en el admin y en el scaffold de proyectos nuevos.
2. **Cumplir el reclamo "rate-limit per-tenant"**: derivar la clave del limiter desde `RequestScopeFromContext` o eliminar la afirmación del README.
3. **JWT con rotación de claves**: introducir anillo de claves con `kid`, soportar RS256/ES256 + JWKS. Hoy solo HS256.
4. **Default-deny en AuthZ**: cuando no haya política Casbin cargada, denegar por defecto (o exigir bandera explícita de "open mode") y montar el middleware en `app.New`.
5. **Redacción centralizada de secretos en `slog`**: `slog.HandlerOptions.ReplaceAttr` o un wrapper que filtre `password|secret|token|api_key|jwt`. No volver a loguear la contraseña del bootstrap.
6. **Comparación de CSRF en tiempo constante** (`crypto/subtle.ConstantTimeCompare`) y reemplazar la `EncryptionKey` por defecto por una clave obligatoria de la config.
7. **Endpoint `/metrics` Prometheus** real en el router de la app principal, no solo en el agente admin.
8. **Circuit breakers** en `pkg/mail/{smtp,sendgrid}.go` y en `pkg/storage/{s3,gcs,azure}.go` (donde hay I/O remoto).
9. **Drift detection de migraciones**: checksum del contenido del archivo vs el aplicado.
10. **Adapters de dialecto en migraciones**: hoy todo el SQL usa misma sintaxis para 5 motores.
11. **Documentar `pkg/observability` vs `pkg/observe`** o fusionar/renombrar uno de los dos.
12. **Limpieza**: `.gitignore` para `.DS_Store`, eliminar `cmd/goframe/` vacío, eliminar `pkg/admin/ui/src/features/*/pages/` vacíos o moverlos a una rama de scaffolding.
13. **Sincronizar `Dockerfile`** con `go.mod` (`golang:1.26-alpine`) — sin esto, la imagen no construye.

---

## 5. Recomendaciones para la documentación

Para que la doc deje de ser aspiracional:

- **Test de paridad CLI ↔ doc**: un check de CI que parsee `website/docs/cli/overview.md` y verifique que cada comando citado existe en `internal/cli/root.go`. Cubre D1, D2, D4.
- **Test de paridad endpoints ↔ doc**: similar para `/healthz` y rutas mencionadas. Cubre D3.
- **Frontmatter `last_update:` o `nucleus_version:`** en cada doc, generado automáticamente por una pre-commit hook.
- **Remover los reclamos no implementados del README** hasta que el código los honre: "per-tenant rate limiting", "34 commands" (poner 37 o dejar de contar). Cubre D4 y D5.
- **Pasar el quickstart por CI**: ejecutar literalmente los comandos del `quickstart.md` en un job de prueba. Cubre D8.
- **Página por paquete `pkg/*`** generada con `pkgsite` o equivalente. Cubre D9.

---

## 6. Veredicto final

**Pregunta del usuario:** "¿Es Nucleus enterprise-class hoy?"

**Respuesta basada en evidencia:** **No, aún no.** Es un framework `v0.6.x` pre-1.0 con cimientos serios (outbox, sesiones, tasks, admin UI cableada, CI con govulncheck y race tests) pero con varias afirmaciones del README y del sitio que el código **todavía no cumple**. La etiqueta "enterprise-grade" en `README.md:16` es **aspiracional**; "production-capable for non-regulated SaaS, with caveats" sería una etiqueta honesta.

**Pregunta del usuario:** "¿Es rigurosa la documentación de `website/`?"

**Respuesta:** **Estructuralmente sí, factualmente no del todo.** La doc está limpia, sin stubs ni marcadores, escrita con buen criterio narrativo, pero contiene **10 discrepancias verificables** con el código, de las cuales **4 son de severidad alta** (D1, D3, D5, D8). En su estado actual, un usuario nuevo que siga la doc al pie de la letra se topará con comandos que no existen y endpoints que no responden.

Con un sprint de ~2 semanas dedicado a las 13 brechas de §4 y a los tests de paridad de §5, Nucleus puede pasar legítimamente a "enterprise-ready" `v0.7.0` antes del `v1.0`.
