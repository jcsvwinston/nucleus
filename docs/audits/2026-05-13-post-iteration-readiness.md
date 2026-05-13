# Re-auditoría de Nucleus — Estado post-iteración doc↔code parity

**Fecha:** 2026-05-13
**Base previa:** `4745158` (Track D close) — cubierta por `AUDITORIA_NUCLEUS_2026-05-12.md`.
**HEAD actual:** `b1e497e` en `main`.
**Ámbito del trabajo de Code:** 16 commits (`#32`–`#47`) que cubren la iteración prometida (D1/D2/D3/D5/D8 + tests de paridad) **y además** 7 brechas enterprise del backlog (JWT rotation, default-deny Casbin, `/metrics`, drift, AutoMigrate multi-driver, circuit breaker, `pkg/health`).
**Método:** tres agentes independientes verificando código real, sin fiar de commit messages ni de `HANDOFF.md`. Cada afirmación lleva `archivo:línea`.

---

## 1. Resumen ejecutivo

| Dimensión | Antes (`4745158`) | Ahora (`b1e497e`) | Cambio |
|---|---|---|---|
| Discrepancias doc↔código severidad alta | 4 abiertas | **0** | ✓ cerradas las 5 (D1–D8) |
| `/healthz` y `/metrics` cableados por defecto | 0 | 2 | ✓ ambos en `App.New` |
| Rate-limit per-tenant | claim falso | **real, con test de regresión** | ✓ |
| Tests de paridad doc↔código | 0 | 2 (uno robusto, uno parcial) | ✓ |
| Primitivas enterprise nuevas en árbol | n/a | JWT rotation, JWKS, Casbin deny, circuit breaker, drift, `pkg/health` | ✓ código |
| Primitivas enterprise **realmente integradas** en `App.New` | n/a | **3 de 6** | ⚠ las otras 3 son librerías huérfanas |
| Higiene básica (`.DS_Store`, Dockerfile) | con manchas | **mejorada** | ✓ parcial |
| Contratos congelados (`contracts/baseline/`, `firewall_test.go`) | intactos | **intactos** | ✓ disciplina respetada |

**Veredicto global:** Nucleus **ha mejorado de forma material**, pero la mejora es asimétrica. Las capas **operacionales** (health, métricas, rate-limit por tenant, multi-driver migrations) están ahora razonablemente en su sitio y enganchadas. Las capas de **seguridad** (rotación de claves JWT, default-deny RBAC) y de **resiliencia** (circuit breaker) tienen primitivas correctas en el árbol pero **el framework no las consume**: el operador que use `app.New(cfg)` no obtiene rotación de claves, ni RBAC por defecto, ni circuit-breakers protegiendo I/O remoto.

Calificación: pasamos de **"usable but thin"** a **"production-capable en operacional, todavía thin en seguridad/resiliencia"**. Sigue siendo pre-`v1.0` justificado.

---

## 2. Iteración pedida — cierre de D1/D2/D3/D5/D8 + tests de paridad

Verificación item por item contra el código en `b1e497e`:

### D1 — `nucleus i18n extract|compile` ficticio
- `website/docs/cli/overview.md:74-75` cita ahora `nucleus makemessages` y `nucleus compilemessages`, ambos registrados en `internal/cli/root.go:21, 36`. **Cerrada.**

### D2 — `nucleus contenttype list` ficticio
- `website/docs/cli/overview.md:76` cita `nucleus remove_stale_contenttypes`, registrado en `internal/cli/root.go:40`. **Cerrada.**

### D3 — Endpoint `/healthz` no expuesto
- `pkg/app/app.go:376` monta `a.Router.Get("/healthz", a.handleHealthz)` en `App.New`. Funciona incluso bajo `WithoutDefaults()` (handler montado **antes** de `attachDefaultSubsystems`).
- `pkg/app/healthz.go:35,41-62` define `healthzPingTimeout=2s` y compone respuesta 200/503 con JSON `{status, checked_at, checks[]}`.
- `pkg/app/healthz.go:91-118` arma el set de probes desde el estado actual: DBs por alias, Redis si `RedisURL`, Storage si attached, Mail solo si `mail.HealthChecker` implementado.
- **Test E2E real** en `contracts/endpoints_doc_parity_test.go:28-92`: arranca `httptest.NewServer` y verifica 200.
- **Cerrada.** *Caveat:* no hay test que fuerce la rama 503 — sería deseable.

### D5 — Rate-limit per-tenant
- `pkg/router/ratelimit.go:180-190`: la clave se prefija con `tenant:<id>` cuando hay tenant en el contexto (vía `observe.TenantIDFromCtx`, declarado en `pkg/observe/logger.go:94`). Mantiene fallback a `user:` y luego `ip:`.
- Regression test en `pkg/router/router_test.go:340-372` (`TestRateLimitMiddleware_SameUserDifferentTenantsHaveSeparateBuckets`): demuestra que dos requests con mismo `user-id` pero distinto tenant tienen cubos independientes. **Cerrada, con test específico.**

### D8 — `quickstart.md` y `AutoMigrate`
- `website/docs/getting-started/quickstart.md:75-95` incluye admonición `:::warning AutoMigrate is dev-mode only` explicitando los tres dialectos soportados (SQLite, PostgreSQL, MySQL), que MSSQL/Oracle devuelven `db.ErrAutoMigrate`, y recomendando migraciones SQL para producción.
- `pkg/app/app.go:84-121` implementa `App.AutoMigrate` dispatching por dialecto a `BuildSQLiteMigrationScaffold`, `BuildPostgresMigrationScaffold`, `BuildMySQLMigrationScaffold` (`pkg/model/migration_scaffold*.go`).
- **Cerrada.** *Caveat:* el comentario en `pkg/db/migrate.go:25` (`"AutoMigrate is intentionally unsupported in the SQL-native runtime"`) quedó obsoleto — ahora coexisten dos puntos de entrada con la misma firma pero distinta semántica (`db.AutoMigrate` siempre falla, `app.AutoMigrate` sí funciona). Deuda interna, no afecta a la doc.

### Tests de paridad — CLI y endpoints
- `contracts/cli_doc_parity_test.go:33-79`: parsea `website/docs/cli/overview.md` extrayendo cada `nucleus <cmd>` en spans de inline-code y exige que el comando exista en `cli.ContractPrimaryCommandNames()` ∪ `ContractAliasCommandNames()` ∪ `{help}`. **Robusto:** compara contra el registro real. Cualquier futura discrepancia D1/D2 dispara CI.
- `contracts/endpoints_doc_parity_test.go:28-92`: monta `httptest.NewServer` con `app.New(cfg, app.WithoutDefaults())` + SQLite `:memory:`, verifica `/healthz` (200, JSON parseable) y `/metrics` (200). **Parcial:** la lista de endpoints documentados está **hardcodeada en el test** — no se descubre desde la doc. Si mañana se documenta `/readyz` o `/livez`, el test no fallaría aunque no exista.
- Ambos viven en `go test ./...` sin build tags ni `-short`, dentro del job `test` requerido por `ci-required-gate` (`.github/workflows/ci.yml:44, 357-411`).
- **Veredicto compuesto:** CLI parity = robusto. Endpoints parity = guard útil pero asimétrico (la simetría doc→código no es real).

**Conclusión §2:** las 5 brechas de severidad alta están **realmente cerradas** y dos quedan respaldadas por tests de regresión. La parte que reclama matiz es el test de endpoints, que protege pero no escala.

---

## 3. Brechas enterprise extra cerradas — verificación de profundidad

Code hizo más de lo pedido. Para cada bloque, profundidad real, cableado en `App.New` y calidad de tests:

### A. `pkg/health` (PR #39)
- Interfaz `Prober{Name(), Probe(ctx) error}` (`pkg/health/health.go:21-30`). `Run` concurrente con per-probe timeout y cancellation por `context.WithTimeout` (`:47-63, :70-72`).
- Probes: DB (interface `DBHealther`, sin acoplar a `*sql.DB`), Redis (URL + `redis.ParseURL`, cliente *lazy* por cada probe — `redis.go:25-52`), Storage (vía `storage.Store.List(prefix:"_nucleus_healthz/", limit:1)`), Mail (type-assert a `mail.HealthChecker`).
- **Cableado:** sí, vía `App.buildHealthProbes` (`pkg/app/healthz.go:91-118`).
- **Tests:** 2 archivos, 11 tests. **No hay test de timeout/cancellation** (el caso donde una dep responde lento y el handler debe devolver 503 sin colgarse). `pkg/health/{db,redis,storage}.go` **no tienen `_test.go` propios** — solo se ejercitan vía el agregador.
- **Verdict:** `usable but thin`. Diseño correcto, lagunas en tests.

### B. `/healthz` core (PR #35 + #42)
- Cableado siempre (`pkg/app/app.go:376`).
- Per-probe timeout 2 s, no se cuelga (`pkg/health/health.go:54-62`).
- **Tests:** solo shape JSON y composición del set de probes (`pkg/app/healthz_test.go`). No hay test que fuerce el código 503 a nivel handler. El único E2E real es el `endpoints_doc_parity_test.go` que solo prueba el happy path.
- **Verdict:** `usable but thin`. Cableado bien, tests decorativos.

### C. JWT key rotation + JWKS (PR #40)
- `pkg/auth/jwt.go`: anillo de claves real (`JWTManager.keys map[string]*SigningKey` — `:122`). Soporta HS256 y RS256; **ES256 no implementado** (switch cae a nil en `:72-80`).
- `RotateKey`/`RemoveKey` con mutex (`:186-217`). `Validate` rutea por `kid` del header (`:286-323`).
- JWKS handler (`:392-399, :438-455`) RFC 7517 compliant, filtra HMAC keys para no leakear secretos.
- Tests sólidos: 17 tests en `pkg/auth/jwt_rotation_test.go` incluyendo el caso pedido "tokens viejos válidos durante grace period" (`:176-208`).
- **Cableado:** **cero**. Búsqueda exhaustiva: `NewJWTManager`/`NewJWTManagerFromKeys` **no se invoca** desde `pkg/app`. El handler `/.well-known/jwks.json` solo aparece en tests (`:359`).
- **Verdict:** **`demo-only`**. Librería bien hecha y bien probada, **pero el framework no la usa**. Un usuario que llame `app.New(cfg)` no obtiene rotación de claves ni endpoint JWKS — tiene que armarlo todo a mano. La promesa "enterprise-grade auth" sigue sin cumplirse desde `App.New`.

### D. Default-deny en Casbin (PR #41)
- Modelo Casbin con deny-override (`pkg/authz/enforcer.go:32-47`): `e = some(where (p.eft == allow)) && !some(where (p.eft == deny))`. `AddPolicy` auto-estampa `allow`, `Deny` auto-estampa `deny`.
- **Breaking change implícito:** políticas CSV antiguas con 3 columnas (sub,obj,act) quedan inertes — se requieren 4 (sub,obj,act,eft). El comentario lo admite en `:26-31`.
- **Cableado:** **sospechoso/intacto.** Si el archivo `admin_rbac.csv` no existe, `rbacEnforcer` sigue siendo `nil` (`pkg/app/app.go:611-619`) y el middleware `Enforcer.Middleware()` (`pkg/authz/middleware.go:31-56`) **nunca se monta en el router** — confirmado: ninguna llamada `a.Router.Use(rbacEnforcer.Middleware())` en `pkg/app`. Solo el admin panel chequea via enforcer (`pkg/admin/panel.go:787`).
- **Tests:** 5 unitarios cubren `deny`/`allow`/`RemovePolicy`. **No hay test E2E que demuestre que sin política una ruta protegida devuelve 403** — porque el middleware no se monta.
- **Verdict:** `usable but thin`. La primitiva de "deny-override en el modelo" es real. La promesa "default-deny en la aplicación" **sigue sin cumplirse**: rutas no-admin sin política siguen pasando sin chequeo. Code mejoró la primitiva, no el wiring.

### E. `/metrics` Prometheus vía OTel exporter (PR #43)
- `pkg/observe/otel.go:110-121`: crea `prometheus.NewRegistry()`, registra `otelprom.New(WithRegisterer(registry))` como reader del MeterProvider, devuelve `promhttp.HandlerFor(registry, EnableOpenMetrics:true)`. **Formato Prometheus/OpenMetrics auténtico**, no nombre.
- Coexiste con OTLP push (líneas 103-108).
- **Cableado:** sí, por defecto. `MetricsPath="/metrics"` en `pkg/app/config.go:387`; `PrometheusEnabled := MetricsPath != ""` en `pkg/app/app.go:210`; ruta montada en `:383-388`.
- **Métricas expuestas:** solo lo que instrumentos OTel del proceso produzcan vía el MeterProvider global. Hay instrumentación HTTP (`pkg/observability/hooks`) y SQL observer (`app.go:330-342`). **No hay** runtime Go metrics (no veo `otelruntime.Start()`) ni Asynq meter dedicado.
- **Tests:** `pkg/observe/otel_test.go` cubre setup/shutdown, **no hace GET /metrics** ni parsea OpenMetrics.
- **Verdict:** `usable but thin`. Cableado y exporter auténticos. Faltan runtime/DB-pool metrics por defecto y test E2E que valide formato.

### F. Drift detection en migraciones (PR #44)
- `pkg/db/migrate.go:211-221`: detecta migraciones aplicadas cuya `.up.sql` ya no existe. Una sola constante `DriftKindMissingUpFile` (`:170-176`).
- El propio comentario admite que **schema-level drift y checksums no están** (`:182-185`): "Today this detects file-level drift only… requires per-dialect schema introspection — tracked as a follow-up".
- **Cableado:** solo via CLI `nucleus migrate drift` (`internal/cli/migrate.go:141-157`). `App.New` **no** chequea drift al arrancar.
- Tests: 3 (`pkg/db/drift_test.go`).
- **Verdict:** `demo-only`. Lo que un operador entiende por "drift detection" (checksum de archivos aplicados vs vivos, o introspección de schema vs migraciones) **sigue sin existir**. Lo único cubierto es el caso burdo "alguien borró un archivo".

### G. AutoMigrate multi-driver (PR #45)
- Scaffolds reales: `pkg/model/migration_scaffold_postgres.go:22-160` genera SQL con `BIGSERIAL`, `TIMESTAMPTZ`, `BYTEA`, `DOUBLE PRECISION`, `CREATE INDEX IF NOT EXISTS`, FKs con `CONSTRAINT … REFERENCES`. MySQL análogo. **Generación dialect-aware sí.**
- Dispatcher en `pkg/app/app.go:129-153` usa `dbConn.System()` y devuelve `ErrAutoMigrate` para mssql/oracle/desconocido.
- **Tests:** `pkg/model/migration_scaffold_dialects_test.go` tiene 6 tests Postgres/MySQL pero son **puramente string-matching**: verifican que el SQL generado contiene `"id" BIGSERIAL PRIMARY KEY`, etc. **No ejecutan el SQL contra un Postgres/MySQL real**. Solo SQLite tiene test de ejecución.
- **Verdict:** `usable but thin`. La generación es plausible, pero "funciona realmente para Postgres y MySQL" se sostiene solo a nivel string. Sin un integration test contra DB real, no hay garantía de que no falle por sintaxis sutil, collation o charset.

### H. Circuit breaker (PR #46)
- `pkg/circuit/circuit.go`: implementación propia (no `sony/gobreaker`). Estados closed/open/half-open completos (`:155-208`), `FailureThreshold`/`Cooldown`/`HalfOpenMaxConcurrent` configurables, defaults 1/30s/1 (`:93-117`), `ErrOpen`, mutex correcto.
- Diseño minimal por elección explícita (`:9-13`): "no event bus, no metrics surface, no per-call timeout".
- Tests: 9 en `pkg/circuit/circuit_test.go`, cubren todas las transiciones.
- **Integración:** **cero**. Búsqueda confirmada: `pkg/circuit` se importa **solamente en documentación** (`docs/guides/OBSERVABILITY_BASELINE.md`, `website/docs/features/observability.md`). **Ningún archivo `.go` en `pkg/mail`, `pkg/storage`, `pkg/plugins`, `pkg/outbox`, `pkg/tasks` lo usa.**
- **Verdict:** **`stub` a nivel integración**. La primitiva es correcta y bien testeada como librería independiente — sería production-ready si alguien la usara. Pero la pregunta "¿está integrado en mail/storage/plugin runner?" se responde con un no rotundo. Es decoración: existe en el árbol y en los docs, no en el data path.

### I. Hygiene sweep (PR #38)
- `Dockerfile:1`: `FROM golang:1.26-alpine AS builder` — coincide con `go.mod:3` (`go 1.26.3`). **Bump real, ahora la imagen sí construye.**
- `.DS_Store`: 5 archivos eliminados del index. Pero `find` confirma que **siguen en working tree** 3 archivos (`.claude/.DS_Store` y dos más). Los nuevos `.gitignore` deberían bloquear su reaparición, pero el barrido se vendió como "completo" cuando es parcial en disco.
- `cmd/goframe/`: el commit afirma que "ya no existe en main". **Falso a medias:** el paquete Go fue removido pero los directorios `migrations/` y `storage/` (subdirs de datos) **siguen existiendo** como residuo.
- CI: dos jobs renombrados (`db-matrix-exploratory-{mssql,oracle}` → `db-matrix-live-{mssql,oracle}`). `scripts/ci/run_exploratory_stability.sh` actualizado en el mismo commit. **Cualquier branch protection o badge externo que referencie los nombres antiguos quedó roto silenciosamente** (no es regresión per se, pero es un riesgo declarado).
- **Verdict:** `usable but thin`. El Dockerfile bump es correcto y necesario. El resto del commit body sobre-vende.

---

## 4. Métricas globales — antes vs ahora

Tabla verificada vía `git ls-tree` y `wc`:

| Métrica | `4745158` | `b1e497e` | Δ |
|---|---:|---:|---:|
| Archivos `.go` no-test | 259 | 268 | **+9** |
| Archivos `_test.go` | 112 | 122 | **+10** |
| LOC no-test | 57.285 | 58.911 | **+1.626** |
| LOC test | 27.429 | 29.023 | **+1.594** |
| Ratio test/code | 47,9 % | **49,3 %** | mejora |
| Deps directas `go.mod` | 35 | 35 | 0 (Prometheus/OTel entran como **indirect**) |
| TODO/FIXME/XXX/HACK (no-test) | 1 | 1 | 0 |
| `panic(` (no-test) | 4 | 4 | 0 |
| `// nolint` directivas | 0 | 0 | 0 |
| `.DS_Store` versionados | 5 | 0 | −5 |
| Cambios en `contracts/baseline/*` | — | **0** | disciplina respetada |
| Cambios en `contracts/firewall_test.go` | — | **0** | firewall intacto |
| Tests eliminados | — | **0** | sin regresión declarada |
| `t.Skip` añadidos | — | **0** | sin skips ocultos |

**Lectura:** disciplina alta. El proyecto **no introdujo deuda visible** mientras añadía 9 archivos productivos y 10 de test. Los contratos congelados ni se tocaron.

---

## 5. Regresiones y riesgos verificados

1. **Rename de jobs CI con efecto cruzado fuera del repo.**
   `db-matrix-exploratory-{mssql,oracle}` → `db-matrix-live-{mssql,oracle}`. El selector en `scripts/ci/run_exploratory_stability.sh` se actualizó en el mismo commit, pero **branch-protection rules, badges externos y dashboards en GitHub viven fuera del repo** y pueden referenciar los nombres antiguos. Verificación interna: `grep -r "DB Matrix Exploratory"` da 0. Verificación externa: no posible desde el repo.

2. **Archivos sin test individual en `pkg/health`.**
   `pkg/health/{db,redis,storage}.go` se ejercitan solo vía agregador. El resto de paquetes nuevos en el rango (`circuit`, `app/healthz`, `auth/jwt_rotation`, `authz/deny`, `db/drift`, `model/scaffold_dialects`) sí tienen `_test.go` por archivo. **Inconsistencia con la convención del repo**, no regresión.

3. **Divergencia Dockerfile vs `go.mod` no resuelta del todo.**
   `Dockerfile` ahora pide `golang:1.26-alpine`. `go.mod` declara `go 1.26.3`. **Pero**: si `setup-go` en CI usa la versión minor de `go.mod` (típicamente la última 1.26.x), los runners de CI y las imágenes Docker pueden estar sobre patch-levels distintos. No es ruptura, es divergencia silenciosa.

4. **Tres primitivas enterprise nuevas son librerías huérfanas.**
   JWT rotation (#40), Casbin deny-override (#41), Circuit breaker (#46) están bien implementadas y testeadas como librerías independientes, **pero ningún consumidor las invoca**. El framework las contiene pero no las usa. Riesgo: el `CHANGELOG` y los docs pueden vendar como features lo que en realidad es código no cableado.

5. **Tests E2E ausentes.**
   Ningún test del rango arranca la app y hace `curl` real contra `/healthz` o `/metrics` con dependencias reales. El único test "E2E-ish" es `contracts/endpoints_doc_parity_test.go`, que valida solo el happy path 200. No hay garantía de que el código 503 funcione bajo carga de probes lentas.

---

## 6. Madurez por dimensión — comparativa

Repito la tabla de §2.5 de la auditoría anterior con la columna actualizada:

| Dimensión | 2026-05-12 | 2026-05-13 | Justificación |
|---|---|---|---|
| Observabilidad (logs/traces/métricas) | 🟡 usable but thin | 🟢 **production-ready** | `/metrics` Prometheus cableado, OTel exporter real, ratelimit observable; falta runtime/DB-pool metrics y test E2E |
| Seguridad (authN/Z/CSRF/sesión) | 🟡 usable but thin | 🟡 **usable but thin (sin cambio neto)** | JWT rotation y default-deny **existen pero no se cablean** — el operador no los obtiene de oficio |
| Resiliencia (timeouts/retries/CB) | 🟡 usable but thin | 🟡 **usable but thin (sin cambio neto)** | Circuit breaker existe como librería pero **no protege a ningún cliente** (mail/storage/plugins) |
| Datos (multi-driver, migraciones) | 🟡 usable but thin | 🟡 **usable but thin (mejora parcial)** | `AutoMigrate` multi-driver real a nivel SQL, pero sin integration tests contra Postgres/MySQL; drift solo file-level |
| Multi-tenancy | 🟡 parcial | 🟡 **parcial (rate-limit cerrada)** | DB-swap funciona; ratelimit per-tenant ✓; schema-swap y row-level siguen ausentes |
| Extensibilidad (plugins/signals) | 🟡 usable but thin | 🟡 **igual** | No tocado en este rango |
| Admin panel | 🟢 production-ready | 🟢 **igual** | No tocado |
| Operaciones (`/healthz`, métricas) | 🔴 demo / faltante | 🟢 **production-ready** | `/healthz` y `/metrics` cableados por defecto en `App.New`, contract guards en CI |
| CI/CD y release | 🟢 sólido | 🟢 **sólido +** | Dos guards nuevos en `contracts/`, MSSQL/Oracle promovidos a required, Dockerfile alineado |
| Higiene de repo | 🟡 aceptable | 🟢 **mejorada** | `.DS_Store` versionados eliminados, contracts/baseline intacto, Dockerfile bump |
| Documentación (rigor doc↔código) | 🔴 con discrepancias altas | 🟢 **alineada + guarded** | 5 discrepancias cerradas, dos parity tests añadidos |

**Saldo:** 3 dimensiones suben (Observabilidad, Operaciones, Higiene/Doc); 4 se mantienen igual o con mejora parcial; ninguna baja.

---

## 7. Brechas que siguen pendientes (de las 13 originales de §4 del informe previo)

De las 13 brechas enterprise originales:

| # | Brecha original | Estado actual |
|---:|---|---|
| 1 | `/healthz` y `/readyz` core | **cerrada `/healthz`** (no hay `/readyz` separado) |
| 2 | Rate-limit per-tenant | **cerrada** |
| 3 | JWT rotation con JWKS | **librería ✓, integración ✗** — el operador no la obtiene |
| 4 | Default-deny AuthZ | **primitiva ✓, wiring ✗** — sin política, rutas no-admin siguen abiertas |
| 5 | Redacción de secretos en logs | **abierta** |
| 6 | CSRF tiempo constante + key obligatoria | **abierta** |
| 7 | `/metrics` Prometheus | **cerrada** |
| 8 | Circuit breakers en mail/storage | **primitiva ✓, integración ✗** |
| 9 | Drift detection migraciones | **parcial** (file-level sí, checksum/schema no) |
| 10 | Adapters dialecto migraciones | **cerrada** (con caveat: sin integration tests reales) |
| 11 | Documentar `pkg/observability` vs `pkg/observe` | **abierta** |
| 12 | Limpieza `.DS_Store`, `cmd/goframe/`, dirs vacíos UI | **parcial** (`.DS_Store` del index sí; working tree parcial; `cmd/goframe` aún residuo) |
| 13 | Dockerfile sync con `go.mod` | **cerrada** |

**Cerradas en este rango: 4.** **Parcial/mejora: 4.** **Abiertas: 5.**

---

## 8. Recomendaciones para la próxima iteración

Priorizadas por **leverage real** (qué obtiene el operador que llama `app.New(cfg)`):

**Alta prioridad — convertir librerías huérfanas en features integradas:**

1. **Cablear default-deny en `App.New`.** Hoy si no hay política Casbin, las rutas no-admin pasan sin chequeo. Decisión arquitectónica: ¿el comportamiento por defecto en `v1.0` debe ser deny? Si sí, hace falta (a) crear un enforcer-default con política vacía, (b) montar `enforcer.Middleware()` en el router, (c) exponer `app.WithOpenAuthz()` para opt-out explícito. Sin esto, el commit #41 es decorativo.
2. **Cablear JWT rotation en `App.New`.** Construir un `JWTManager` desde config (`auth.jwt_keys[]` con `kid`, `alg`, `secret`/`pem`), montar `/.well-known/jwks.json` por defecto, y hacer que el middleware de auth lo consuma. Sin esto, el operador sigue con HS256 single-secret.
3. **Integrar circuit breaker en al menos `pkg/mail` y `pkg/storage`.** Envolver SMTP/SendGrid y los clientes S3/GCS/Azure con `circuit.Breaker`. Sin esto, el commit #46 es decoración.

**Media prioridad — robustecer lo ya cableado:**

4. **Test E2E `/healthz` rama 503**: un probe que falle deliberadamente y verificar que la respuesta es 503 con `checks[].status="unhealthy"`.
5. **Test E2E `/metrics`**: GET y parseo OpenMetrics. Añadir runtime Go metrics (`otelruntime.Start()`) y DB-pool métricas explícitas.
6. **Integration tests reales** para `AutoMigrate` Postgres/MySQL: el job `db-matrix-required` ya existe; basta añadir un test que llame `app.AutoMigrate(...)` contra un Postgres real y verifique que `\d table` devuelve los tipos correctos.
7. **Drift detection schema-level**: checksum del `.up.sql` aplicado en `nucleus_schema_migrations` y comparación con el archivo actual. La estructura `Drift` ya existe; basta extender.

**Baja prioridad — higiene:**

8. Redactor de secretos en `slog` (gap #5 original).
9. CSRF tiempo constante con `crypto/subtle.ConstantTimeCompare` y `EncryptionKey` obligatoria (gap #6).
10. Endpoints parity test debe **parsear** la doc, no hardcodear la lista.
11. Borrar `cmd/goframe/{migrations,storage}/` y los `.DS_Store` que quedan en working tree.
12. Resolver la ambigüedad `pkg/db.AutoMigrate` (siempre falla) vs `pkg/app.AutoMigrate` (sí funciona). Lo mínimo: actualizar el comentario en `pkg/db/migrate.go:25`.
13. Documentar la dualidad `pkg/observability` (event bus) vs `pkg/observe` (slog/OTel) o renombrar uno.

---

## 9. Veredicto final

**Pregunta:** ¿En qué punto estamos?

**Respuesta:** Nucleus pasó de una v0.6.x **"usable but thin con doc aspiracional"** a una v0.6.x+ **"production-capable en operacional, thin todavía en seguridad/resiliencia"**.

Las **operaciones** del framework (health, métricas, rate-limit, doc parity, migraciones multi-driver) están en su sitio y el operador las obtiene por defecto. La **seguridad** y la **resiliencia** tienen primitivas correctas en el árbol pero no las consume nadie — el operador no recibe rotación de claves, ni RBAC por defecto, ni circuit-breakers. Esto es importante porque el commit log y el HANDOFF pueden dar la impresión de que esos bloques están "cerrados", y técnicamente lo están a nivel de **librería**, pero no a nivel de **producto**.

**El trabajo de Code merece un veredicto matizado:**
- ✅ Disciplina: contratos congelados intactos, firewall intacto, sin tests eliminados, ratio test/code mejora, sin deuda nueva.
- ✅ Wiring operacional sólido: `/healthz`, `/metrics`, rate-limit per-tenant, parity guards.
- ⚠ Wiring de seguridad/resiliencia incompleto: primitivas excelentes, integración inexistente.
- ⚠ Sobre-venta en commit messages: el "hygiene sweep" dejó `.DS_Store` en working tree; el "drift detection" solo detecta el caso más burdo; el "circuit breaker" no protege nada todavía.

Si el objetivo es **etiquetar `v1.0` como "enterprise-class"**, **falta una iteración** que cierre el wiring de las tres librerías huérfanas (JWT, default-deny, circuit breaker), añada tests E2E auténticos y resuelva la dualidad `AutoMigrate`. Con eso, Nucleus llega a un `v0.7.0` defendible y `v1.0` es realista.
