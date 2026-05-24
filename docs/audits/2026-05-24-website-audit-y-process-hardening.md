# Auditoría website ↔ código + veredicto competitivo

**Repo:** `github.com/jcsvwinston/nucleus` (alias local: GoFrame/Nucleus)
**Branch / HEAD:** `main` @ `aad8bf8`
**Fecha:** 2026-05-24
**Alcance:** los 15 archivos de `website/docs/**` contrastados con `pkg/*`, `internal/cli/*`, `docs/reference/*`, `docs/adrs/*`, `docs/guides/*`, `SPEC.md` y los últimos 60 días de `git log` (304 commits, ADR-010 fluent API v2 completo, Oracle ADR-011, secure-by-default, `/_/config`, etc.).
**Método:** 4 auditores en paralelo (getting-started, architecture+concepts, features, CLI+coverage) + 1 investigación competitiva externa. Cada hallazgo P0/P1 fue re-verificado a mano contra el código antes de incluirse aquí. Un hallazgo de los subagentes se descartó por error (ver §Verificaciones cruzadas).

---

## 0. TL;DR

- **El website es honesto pero pobre en cobertura.** No miente sistemáticamente, pero tiene **3 falsedades P0 verificadas** (un ejemplo que no compila, una versión de Go incorrecta, una key de config inexistente en un ejemplo YAML) y deja **gran parte del framework sin documentar**: ~11 de 21 paquetes `pkg/*` no tienen página propia, y las guías internas más densas (`DEPLOYMENT_GUIDE`, `CSRF_GUIDE`, `RATE_LIMITING_GUIDE`, `MODELING_MULTI_DATABASE`, `DETAILED_TUTORIAL`, `TESTING_GUIDE`) no se reflejan en absoluto en el sitio público.
- **El gap más grave NO es de falsedad, es de cobertura.** El ADR-010 (fluent API v2, multi-file merge, env layer + file:line, `/_/config`, effective-config inspection) está parcialmente reflejado; SchemaDrift (ADR-009) y soporte MSSQL/Oracle (ADR-011 + multi-block AutoMigrate) están casi invisibles. Un evaluador externo creería que Nucleus es un framework más pequeño y menos serio de lo que realmente es.
- **Posición competitiva: ~6/10 hoy, con potencial real de 9/10.** Buffalo (el único "Django de Go") está archivado desde feb 2024. GoFrame (`gogf`, chino) es el rival más directo en ambición pero pierde por idioma, falta de stdlib-first y ausencia de contratos congelados. Hay un **cuadrante vacío** en el ecosistema — framework Go enterprise full-stack, stdlib-first, occidental, con SLO de compatibilidad verificable y multi-DB serio — y Nucleus está a 6-12 meses de ocuparlo si ejecuta bien.
- **Bloqueador #1 para mover la aguja: el website.** El código está muy por delante de su documentación pública. Hasta que el sitio refleje lo que ya se ha enviado, ningún CTO va a apostar por él.

---

## 1. Falsedades verificadas (corregir antes de cualquier campaña)

Cada uno re-verificado a mano contra el código por encima del informe del subagente.

### P0-1 — `website/docs/getting-started/installation.md:12` — versión de Go incorrecta

> Doc actual: *"Go **1.25** or newer"*

`go.mod` declara `go 1.26.3` **sin `toolchain` directive**, así que `go install …@latest` con Go 1.25 fallará. La página instruye una versión que rompe el comando que la propia página recomienda.

**Fix:** cambiar a `Go 1.26 or newer` (o añadir un `toolchain go1.26.3` en `go.mod` si quieres conservar el floor de 1.25 — decisión de governance, no de docs).

### P0-2 — `website/docs/features/auth.md:86` — el ejemplo no compila

> Doc actual:
> ```go
> hash, err := auth.HashPassword("hunter2")
> ok,   err := auth.VerifyPassword(hash, "hunter2")
> ```

`pkg/auth/password.go` solo exporta:
- `HashPassword(password string) (string, error)` (línea 10)
- `CheckPassword(password, hash string) bool` (línea 20)

No existe `VerifyPassword`, y la firma esperada `(bool, error)` está mal — la función real retorna solo `bool`. Para más vergüenza, el propio **frontmatter de la misma página** (línea 6 `covers:`) ya lista `pkg/auth.CheckPassword`. Es una inconsistencia interna del archivo: el frontmatter dice una cosa, el cuerpo otra.

**Fix:**
```go
hash, err := auth.HashPassword("hunter2")
if err != nil { /* ... */ }
ok := auth.CheckPassword("hunter2", hash)  // nota: (password, hash), no (hash, password)
```

### P0-3 — `website/docs/features/storage-and-tasks.md:72-74` — key YAML inexistente

> Doc actual:
> ```yaml
> storage:
>   driver: s3              # local | s3 | gcs | azure
> ```

`storage.driver` **no existe** como key. Las opciones reales son:
- **Stable (recomendada):** `storage.provider` (anidado), tag koanf `provider` en `pkg/app/config.go:222`.
- **Legacy deprecated:** `storage_driver` (flat, no anidado), tag koanf `storage_driver` en `pkg/app/config.go:160`, marcado deprecated en `CONFIG_KEY_REGISTRY.md:225`.

El ejemplo enseña una key que es ni la nueva ni la vieja — está mezclando ambas formas de la peor manera. Usuario que copia/pega obtiene unknown-fields error si `WithUnknownFields(Strict)` está activo.

**Fix:** usar la canónica `storage.provider: s3`. La misma página ya lo lista correctamente en `config_keys:` (línea 29), confirmando que el ejemplo del cuerpo simplemente está mal.

---

## 2. Omisiones críticas (no son mentiras pero hieren igual)

### 2.1. Paquetes `pkg/*` SIN página propia en el website

De los 21 paquetes en `pkg/`, el website solo da página dedicada a `auth`, `admin`, `observability`/`observe`, `storage` (+`tasks` dentro). El resto está invisible para un visitante:

| Paquete | Prioridad | Por qué duele |
|---|---|---|
| `pkg/plugins` | **P0** | Sistema de plugins completo con 3 comandos CLI estables (`plugin list`, `plugin doctor`, `plugin test`). Es feature diferenciadora y vendible. El website cero. |
| `pkg/validate` | **P0** | Validación es horizontal en cualquier framework serio. Guide interna existe (`VALIDATION_GUIDE.md`); el website no la refleja. |
| `pkg/mail` | **P1** | 2 drivers built-in + plugin ABI para SendGrid/Mailgun/SES/Postmark/Resend. CLI `mailproviders` + `sendtestemail`. Solo `MAIL_GUIDE.md` interna. |
| `pkg/openapi` | **P1** | Generación OpenAPI desde código; comando `openapi` existe. Killer feature para vender ante equipos API-first. |
| `pkg/signals` / `pkg/outbox` | **P1** | Patrones enterprise (signals = pub/sub interno; outbox = transactional outbox). Diferenciadores claros vs Gin/Echo. |
| `pkg/router` | **P1** | Página propia documentando middlewares, sub-routers, modos fluent vs direct. Hoy solo `concepts/routing.md` lo toca por encima. |
| `pkg/db` | **P1** | Multi-DB, `ExecScript`, `WithTx`, healthchecks. Sin página. |
| `pkg/errors`, `pkg/circuit`, `pkg/health`, `pkg/authz` | **P2** | Páginas cortas para search-engine + completitud arquitectónica. |

### 2.2. Guías internas (`docs/guides/*`) que no llegan al website

| Guide interna | Tema | Prioridad para el website |
|---|---|---|
| `DEPLOYMENT_GUIDE.md` | Reverse-proxy, env layers, drift checks, /\_/config en prod | **P0** — sin esto un CTO no lleva esto a prod |
| `CSRF_GUIDE.md` | CSRF (ADR-006/008) | **P0** — feature de seguridad clave |
| `RATE_LIMITING_GUIDE.md` | Rate limit window/burst | **P1** |
| `MODELING_MULTI_DATABASE.md` | Multi-DB serio, alias, módulos | **P1** — junto a `models-and-database.md` |
| `MULTISITE_GUIDE.md` | Multi-tenancy nativa | **P0** — uno de los killer features (ver §5) |
| `TESTING_GUIDE.md` | Testear apps Nucleus | **P1** |
| `SIGNALS_GUIDE.md` | Patrones de signals | **P2** |
| `DETAILED_TUTORIAL.md` | Tutorial end-to-end | **P0** — el `quickstart.md` actual termina demasiado pronto |

### 2.3. Conceptos del código que el website omite

| Concepto | Dónde está | Por qué importa |
|---|---|---|
| ADR-010 §2 — **5 capas de validación de config** (syntactic / schema / field-semantic / referential / module-specific) | ADR-010 + `pkg/nucleus/*` | `concepts/configuration.md` dice "layered precedence chain" sin enumerar las 5 capas. Es la promesa fail-fast del framework — vendible. |
| `_append` / `_remove` + null-revert + **non-nullable security keys** (`cors.origins`, `auth.providers`, `authz.policy_path`, `session.secret`) | ADR-010 Phase 2b, `pkg/nucleus/merge.go` | Mencionados en `configuration.md` pero sin la lista explícita ni la garantía de fail-loud. |
| **Effective-config inspection** (ADR-010 Phase 3a) y `/_/config` auth-gated (Phase 3b) con file:line provenance (Phase 3.1) | `pkg/nucleus/effective.go`, `internal/cli/configcommands.go` | Diferenciador real vs Gin/Echo/Fiber. Apenas se menciona y nada del file:line. |
| **SchemaDrift** (ADR-009) — 4 clases de drift detection, soportado en SQLite/PG/MySQL/MSSQL/Oracle | `pkg/db/schema_drift.go` | `concepts/models-and-database.md` ni lo nombra. Es uno de los features más vendibles para "production safety". |
| **Multi-engine real** — MSSQL y Oracle con AutoMigrate dialect-aware y multi-block PL/SQL (ADR-011 + `db.ExecScript`) | `pkg/db/exec_script.go`, `pkg/model/meta.go` | `models-and-database.md` dice "Postgres, MySQL, SQLite" y se calla. Es el feature que abre puertas a banca/seguros/admin pública. |
| **Module-scoped migrations** con namespacing `<module>/<filename>` y ownership awareness | ADR-010 Phase 2d, `pkg/db/migrate.go` | Apenas un párrafo; debería ser una sección completa con ejemplo. |
| **`Options` field en `nucleus.App` direct-struct** y equivalencia de las 3 surfaces (fluent / direct-struct / bootstrap) — verificado por `equivalence_test.go` | ADR-010 §1 | `concepts/application.md` no explica que son intercambiables. |
| **`session_cookie_secure: true` por default** (2026-05-23) y SameSite handling | `pkg/app/config.go:443`, ADR-010 Phase 2b MED-1 | Reciente; las páginas de auth pueden estar desfasadas. |

### 2.4. CLI overview incompleto

`cli/overview.md` es **demasiado alto nivel** y reconoce abiertamente que `CLI_CONTRACT_MATRIX.md` (interna) es el "canonical reference". Eso significa que el binario es el único source of truth para CLI — el website no sirve como referencia ejecutable. Comandos del contrato estable verificados como AUSENTES en overview:

- `migrate create` (alias `makemigrations`) — confirmado en `internal/cli/migrate.go:40`
- `migrate refresh` — confirmado en `migrate.go:183`
- `migrate reset` — confirmado en `migrate.go:161`
- `wizard --type ...` — mencionado de pasada sin documentar los `--type` válidos
- Flags detallados de `plugin list/doctor/test`, `sql migrate`, `inspectdb`

**Recomendación:** auto-generar una página `cli/reference.md` desde `commandSpecs` en `internal/cli/root.go` (es trivial — todas las commands están registradas declarativamente). Mantener `overview.md` como onboarding narrativo, separar la referencia exhaustiva.

---

## 3. Stale / a corregir (pero menores)

- `configuration.md` líneas 160-161: parcialmente cubre el comportamiento de `null → default`, pero no enumera las keys donde `null` se rechaza con `ErrSecurityKeyNotNullable`. Citar `cors.origins`, `auth.providers`, `authz.policy_path`, `session.secret`, `jwt_secret`.
- `concepts/application.md`: documentar que `App.Options []Option` permite pasar `WithoutDefaults()`, `WithUnknownFields(Strict)`, etc., también desde la surface direct-struct.
- `concepts/models-and-database.md` línea 96: actualizar a *"PostgreSQL, MySQL, SQLite, MSSQL (build tag `mssql`) y Oracle (build tag `oracle`)"*.
- `principles.md`: añadir mención explícita a MSSQL/Oracle como exploratory build-tag-gated (alineado con `SPEC.md §4`).
- `intro.md`: el patrón canónico `nucleus.New().FromConfigFile().Use().Mount().Build().Run()` no aparece como una sola pieza visible — se ve fragmentado entre páginas.

---

## 4. Verificaciones cruzadas (qué descarté de los subagentes)

Para honestidad metodológica: el subagente de architecture+concepts reportó como **P0 crítico** que `pkg/nucleus.Context` no existe y que `routing.md:113-114` ("`*router.Context` (or, in fluent mode, a `*nucleus.Context` that wraps it)") era falso. **Falsa alarma:** `pkg/nucleus/context.go:12` define `type Context struct { *routerpkg.Context }` con métodos `BindJSON`, `BindXML`, `BindForm`, `Query`, `Param`, `JSON`, `XML`, `HTML`, `String`, `Status`, `NoContent`, `Redirect`, `Set`, `Get`, `RequestID`, `SessionGetString`, `SessionPutString`. La doc es correcta. (Observación lateral: ese `Context` es muy thin y poco idiomático — `interface{}` en lugar de `any`, `mapFormToStruct` con TODO de "production implementation would use reflection" — es candidato a refactor, pero eso es deuda técnica, no falsedad de docs.)

Todo lo demás de los reportes que cité arriba fue re-verificado a mano: `auth.CheckPassword` vs `VerifyPassword`, `storage.provider` vs `driver`, `go 1.26.3` en `go.mod`, existencia de `migrate create/refresh/reset`, ausencia de página de plugins.

---

## 5. Veredicto franco: ¿a qué nivel estamos para ser "el mejor framework Go enterprise"?

### 5.1. Posicionamiento honesto: **~6/10 hoy, techo realista 9/10**

Esto NO es un autoengaño optimista ni un palo gratuito — es una lectura calibrada:

**Lo que ya está al nivel top-tier (8-9/10):**

1. **Multi-DB serio** — SQLite, PostgreSQL, MySQL, MSSQL, Oracle con AutoMigrate dialect-aware. Gin/Echo/Fiber **ni siquiera juegan en esta liga** (no tienen ORM/migrator); GoFrame (`gogf`) lo soporta pero sin lane CI verificable. Nucleus tiene la matriz CI con build tags + ADR-011 para identifier-casing + `db.ExecScript` para multi-block PL/SQL. **Esto es mejor que el 90% del ecosistema.**
2. **Compatibility SLO + freeze tests en CI (`contracts/`)** — promesa contractual estilo Go stdlib. **Nadie en Go la formaliza así**. Es el moat más defendible. ADR-001 + COMPATIBILITY_SLO + contract-guardian subagent + scanner para nested packages.
3. **Config layering completo** (ADR-010): fluent + direct-struct + bootstrap equivalentes, 5 capas de validación, multi-file merge con `_append`/`_remove`, env layer, file:line provenance, effective-config inspection, `/_/config` auth-gated. **Esto no existe en ningún otro framework Go.** Es Encore-grade DX sin lock-in cloud.
4. **Stdlib-first** — ADR-001 disciplinado, `net/http`, `database/sql`, `log/slog`. Es vendible a equipos que han sufrido Fiber/fasthttp incompat.
5. **Operational features built-in** — circuit breaker, outbox, slog redaction (ADR-007), Casbin RBAC default-deny mount (ADR-004), JWT ES256 + AWS Secrets (ADR-005), CSRF (ADR-006/008), schema drift detection (ADR-009). Cada uno suelto en Gin requiere ensamblar 8-10 libs.

**Lo que está en el medio (5-6/10):**

6. **Admin panel + CLI Django-like** — `createuser`, `changepassword`, `migrate`, `inspectdb`, `routes`, `shell`, `doctor`. Buen comienzo, pero un repaso UX/DX del admin (especialmente con los hallazgos de validación e i18n) lo subiría a 8/10.
7. **Router** — Funciona, soporta resources, middlewares, pero `pkg/nucleus.Context` se ve **subdesarrollado** (`mapFormToStruct` con TODO, métodos delgados). Necesita pulido.
8. **Plugins** — Sistema existe + CLI + ABI documentado en `PLUGIN_SDK.md`, pero el ecosistema es 0. Sin plugins reales (al margen de los de mail) el feature es papel.

**Lo que cojea claramente (3-5/10):**

9. **Website / DX externa** — Lo más urgente. Cubierto en §1-§4. Hoy un evaluador externo de 30 minutos saldría con la impresión de que Nucleus es un framework de juguete. La realidad del repo está a generaciones por delante de su escaparate.
10. **Multi-tenancy** — Existe `MULTISITE_GUIDE.md` interna pero no se muestra como feature de primera clase. Es un killer feature potencial que está enterrado.
11. **Observabilidad pública** — `pkg/observe` (estable) está bien, pero `pkg/observability` está marcado **experimental** y los hooks no tienen recorrido OTel-completo aún. GoFrame y Encore venden esto mejor.
12. **Performance** — No hay benchmarks publicados. Gin/Fiber se venden por números. Sin benchmarks visibles, no convences a nadie por "es rápido".
13. **Comunidad / adopción** — N=1 maintainer aparente. Esto no se arregla con código, se arregla con marketing + showcase apps + contributions.

### 5.2. Comparativa con el ecosistema (Mayo 2026)

| Framework | Status | Cuadrante | ¿Compite con Nucleus? |
|---|---|---|---|
| **Gin** | Activo (~48% share) | Router minimalista | Sí, en el "punto de entrada". Lo bate Nucleus en cualquier app no-trivial. |
| **Echo** | Activo (~16%) | Router + baterías ligeras | Igual que Gin. |
| **Fiber** | Activo (~11%) | fasthttp performance | No directamente (rompe stdlib, gRPC, httptest). Nucleus gana en compatibilidad. |
| **Buffalo** | **Archivado feb 2024** | Rails-like full-stack | **Murió. Hueco abierto.** Nucleus es el sucesor natural. |
| **Beego** | Activo v2 (abr 2026) | "Django de Go" antiguo | Sí, en Asia. Pierde por API anticuada y sin OTel-first. |
| **GoFrame (gogf)** | Muy activo v2.8+ | Full enterprise (CN) | **Sí, rival nº1 en ambición.** Pierde por: doc primaria china, no stdlib-first, sin admin built-in, sin contratos congelados, sin multi-tenancy nativa. |
| **Kratos / go-zero** | Muy activos | Microservicios | No. Distinto target. |
| **Encore.go** | Muy activo | Backend + runtime cloud | Solo si aceptas lock-in. Nucleus ofrece la DX similar sin runtime. |
| **GoFr (gofr.dev)** | Emergente | Opinionado backend | Joven, sin tracción. |

**El cuadrante vacío que Nucleus puede capturar:**
> *Framework Go enterprise full-stack, stdlib-first, occidental (docs EN), con SLO de compatibilidad verificado en CI, multi-DB enterprise (MSSQL/Oracle incluidos), admin/CLI/scaffolding Django-like, sin lock-in cloud ni DSLs externos.*

Nadie ocupa ese cuadrante. Nucleus tiene el código para hacerlo. Le falta la ventana al mundo.

### 5.3. Plan de batalla para llegar al top (orden estricto)

#### Fase A — Arreglar el escaparate (4-6 semanas) — bloquea todo lo demás

1. **Corregir las 3 falsedades P0** (§1) — trivial, 1 día.
2. **Crear las páginas faltantes high-impact en el website:**
   - `features/plugins.md` (sistema + ABI + cómo escribir uno)
   - `features/mail.md` (drivers + plugins SendGrid/etc.)
   - `features/validate.md`
   - `features/openapi.md`
   - `features/multi-tenancy.md` (basada en `MULTISITE_GUIDE.md`)
   - `guides/deployment.md` (basada en `DEPLOYMENT_GUIDE.md`)
   - `guides/security.md` (CSRF + rate-limit + redaction + secure-by-default)
   - `guides/testing.md`
   - `cli/reference.md` (auto-generada desde `commandSpecs`)
3. **Expandir `concepts/configuration.md`** con las 5 capas de validación y la lista de keys non-nullable.
4. **Reescribir `concepts/models-and-database.md`** mencionando SchemaDrift, MSSQL/Oracle, AutoMigrate, module-scoped migrations.
5. **Reescribir `intro.md`** con el patrón canónico fluent v2 visible de un vistazo + 3 features bandera (multi-DB, compatibility SLO, admin built-in).
6. **Promover `website-drift` CI job a required gate** (ya listado como candidate #9 en `CURRENT_ITERATION.md`). Hoy es advisory — pasa a obligatorio cuando los manifests estén estables. Sin esto, este audit va a repetirse.

#### Fase B — Killer demos y benchmarks (4-8 semanas)

7. **Showcase application live** — un blog/SaaS/marketplace funcional, deployado en alguna parte (showcase.nucleus.dev), con código en `examples/`. Buffalo lo tenía. Es la prueba de existencia que más vende.
8. **Benchmarks comparativos publicados** — Nucleus vs Gin vs Echo vs Fiber vs GoFrame en throughput, latencia p99 y "tiempo desde `go install` hasta primer endpoint en prod". El último número es donde Nucleus debería arrasar.
9. **Multi-tenancy de primera clase visible** — feature page + ejemplo + guide. Hoy es un secreto bien guardado.
10. **Plugin marketplace mínimo** — 3-5 plugins oficiales más allá de los de mail (storage providers, otel exporter, paywall/billing, i18n, search).

#### Fase C — Diferenciación profunda (3-6 meses)

11. **Compatibility report público por release** — generar y publicar un diff de `contracts/baseline/*.txt` automáticamente en cada release. Esto es **único** y vendible. "Aquí está lo que cambió y lo que no — verificado en CI".
12. **`pkg/nucleus.Context` digno** — refactor del thin wrapper actual con form binding real, validación integrada, error helpers, content-negotiation. Es el punto de contacto del usuario, debe ser excelente.
13. **OTel-first observability** — `pkg/observability` sale de experimental con tracing/metrics/logs end-to-end y dashboards prefab. Es donde GoFrame y Encore están ganando.
14. **Performance-bench subagent operacional** — ya existe en el `.claude/agents/`; usarlo cada iteración no opcional para evitar regresiones que se vean en los benchmarks públicos.
15. **Pivote de comunidad** — Showcase Calendar (1 app de ejemplo nueva al mes), blog técnico (1 post al mes profundizando en SchemaDrift, ExecScript, fluent v2 internals), Discord/Slack, conferencia GopherCon proposal para v1.0.

#### Fase D — v1.0 con un mensaje afilado (cuando A+B+C estén)

16. **Mensaje de v1.0:**
    > *"El Django de Go con contratos congelados, multi-DB enterprise (incluyendo MSSQL y Oracle) y sin lock-in cloud."*
17. Activar `governance-checker` + `RELEASE_CHECKLIST.md` en modo serio. Ya están listos en el repo.

### 5.4. Riesgos a vigilar

- **GoFrame pivota a inglés** — si `gogf` publica una versión documentada en EN agresivamente, come el cuadrante. Mitigación: ser primero en el message-market-fit occidental.
- **Encore.dev abre el runtime** — si Encore se hace open-runtime, su DX gana. Mitigación: igualar la DX de panel/tracing local en `/_/config` + un dashboard de developer mode.
- **El maintainer principal se quema** — N=1 es frágil. Mitigación: hacer Fase A + B con mira a atraer contributors (la documentación pobre es lo que aleja contributors).
- **Algún competidor copia "compatibility SLO + freeze tests"** — es el feature más fácil de replicar. Mitigación: ser quien lo institucionaliza primero y mejor (incluyendo un comparator público de contratos entre versiones).

### 5.5. Conclusión franca

Nucleus tiene **más sustancia técnica que muchos frameworks que se llaman a sí mismos "enterprise"**. La disciplina de ADRs, freeze tests, `CURRENT_ITERATION.md`/`HANDOFF.md`, los subagentes especializados y la cadencia de envío (304 commits en 60 días con governance estricta) son señales de un proyecto serio.

**Pero hoy un evaluador externo no vería esto.** Vería un website con 15 páginas, un ejemplo de auth que no compila, una recomendación de Go incorrecta y un ejemplo de storage con una key inventada. Saldría pensando que es un proyecto de fin de semana.

La distancia entre lo que Nucleus **es** y lo que Nucleus **se ve** es donde está concentrado el ROI. Cierra esa distancia (Fase A + B en 8-12 semanas) y el camino a top-3 framework Go enterprise es real — con un cuadrante competitivo casi vacío esperando.

---

## 6. Apéndice — comandos rápidos para empezar

```bash
# 1. Confirmar las 3 falsedades P0 desde el repo
grep -n "VerifyPassword\|CheckPassword" pkg/auth/password.go
grep -n "Go \*\*1\." website/docs/getting-started/installation.md
grep -n "driver:" website/docs/features/storage-and-tasks.md

# 2. Listar paquetes pkg/ sin página propia en website
for p in pkg/*/; do
  name=$(basename "$p")
  if ! grep -rq "pkg/$name" website/docs/; then
    echo "MISSING: $name"
  fi
done

# 3. Listar comandos CLI estables ausentes del overview
grep -E '^\| `[a-z]' docs/reference/CLI_CONTRACT_MATRIX.md \
  | awk -F'`' '{print $2}' \
  | while read c; do
      grep -q "$c" website/docs/cli/overview.md || echo "MISSING: $c"
    done

# 4. Ejecutar el drift guard advisory ya configurado
bash scripts/ci/run_website_drift.sh || true
```

---

**Generado por una sesión de auditoría guiada por `CLAUDE.md` §2-§4. No se ha tocado el árbol de trabajo (la iteración ADR-010 §2 layer-3 sigue intacta).** Para integrar este audit a la cadencia normal, considera abrir una iteración `docs/iterations/2026-05-XX-website-audit-followup.md` que ejecute la Fase A.

---

## 7. Cotejo con el drift guard del `website-curator` (addendum metodológico)

> *Esta sección se añadió después de la auditoría principal, al constatar que la primera pasada usó subagentes genéricos (`Explore`, `general-purpose`) en lugar del subagente `website-curator` que el proyecto tiene definido en `.claude/agents/`. El cotejo con el drift guard autoritativo que ese subagente usa (`scripts/website/check-coverage.sh`) es el corte de honestidad metodológica.*

### 7.1. Output del drift guard (autoritativo)

```
$ bash scripts/website/check-coverage.sh --strict

== 1. legacy / removed-API tokens ==
  none
== 2. dangling covers: references ==
  none
== 3. pages without a covers: manifest (informational) ==
  none

== summary ==
  legacy/removed tokens : 0
  dangling covers refs  : 0
  pages w/o manifest    : 0 (informational)

OK (warn mode).
```

### 7.2. Verdict tipo `website-curator`

```
## Website Curation
**Verdict:** NO_CHANGE_NEEDED (frontmatter) + BLOCKED (body content)

### Updated
- (ninguno en esta pasada — auditoría no-edit)

### Coverage guard (scripts/website/check-coverage.sh)
- legacy tokens (goframe / removed API) : none
- dangling covers: refs                 : none
- undocumented stable surface           : ~1285 símbolos del baseline sin cobertura
                                          (127 covers únicos / 1412 símbolos en
                                          contracts/baseline/api_exported_symbols.txt = ~9%)
- pages missing a manifest              : 0

### Notes / follow-ups
- 3 P0 falsehoods en body content (§1) NO son detectables por el script actual.
  Recomendación: extender check-coverage.sh con un segundo paso que escanee los
  fenced code blocks de cada página y valide símbolos Go contra el baseline,
  y keys YAML contra CONFIG_KEY_REGISTRY.md. Sin esto, el drift guard es ciego
  a la clase de error más visible para un visitante.
```

### 7.3. Lectura honesta

- **A favor del trabajo del `website-curator` hasta hoy:** el frontmatter está limpio. Las 127 entradas `covers:` apuntan todas a símbolos que existen en el baseline contractual (cero dangling refs). No hay tokens legacy (`goframe`, `RouterGroup`, `.SQLite()/.Postgres()/.MySQL()`). Todas las páginas tienen manifest. Esto es real y vale.
- **Lo que el guard NO ve y mi auditoría sí encontró:** las 3 falsedades P0 (§1) están todas en el cuerpo del Markdown (snippets Go, ejemplos YAML, prosa), no en el frontmatter. El drift guard hoy no escanea el body — es un blind spot conocido por diseño ("heuristic, not a proof") pero corregible.
- **Lo que ni el guard ni mi audit miden:** la **suficiencia** de la cobertura. Que los 127 covers existentes sean correctos no implica que cubrir 127/1412 sea suficiente. Esa es una decisión editorial del `website-curator`, no algorítmica.

### 7.4. Hallazgo metodológico (nuevo, derivado del cotejo)

**Extender `scripts/website/check-coverage.sh` con un body scanner** evitaría que las 3 falsedades P0 de esta auditoría hubieran llegado a publicarse. Heurísticas viables, en orden de coste/valor:

1. **Go symbol scanner en code blocks `go`:** extraer identificadores con regex tipo `\bauth\.[A-Z]\w+` / `\bnucleus\.[A-Z]\w+` / `\bapp\.[A-Z]\w+`, validarlos contra `api_exported_symbols.txt`. Falsos positivos manejables con allowlist.
2. **YAML key scanner en code blocks `yaml`:** parsear keys, validarlas contra `CONFIG_KEY_REGISTRY.md`. Falsos positivos: ejemplos parciales — requieren marcador `# example, not exhaustive`.
3. **Go version pinning:** extraer cualquier mención `Go 1.XX` en prosa y validar contra `go.mod`. Trivial.

Las tres juntas son ~150 líneas de bash/awk y promueven el drift guard a algo más cercano a un type-checker de documentación.

---

## 8. Encaje en el plan ya existente (`CURRENT_ITERATION.md`)

> *Respuesta a la pregunta del owner: cómo abordamos lo descubierto sin descarrilar lo que ya está en cola.*

### 8.1. Lo que NO cambia

- **Iteración activa (ADR-010 §2 layer-3 field-semantic validation)** sigue intacta y debe terminarse primero. Esta auditoría no la toca y no debería desplazarla; las falsedades P0 son docs, no bloquean el shipping de código.
- **Carry-forward follow-ups vivos** (Oracle bootstrap → ExecScript, Oracle DDL-auto-commit vs Migrator tx, SameSite=None+Secure validation, CI mssql/oracle required-vs-exploratory, Oracle reserved-word hardening, doc sweep side-effects, GCS redaction forward-compat, /\_/config reverse-proxy note, `pkg/observability` post-v1.0 relocate, etc.) NO se solapan con esta auditoría — son código, no docs públicas.

### 8.2. Mapping de hallazgos a candidates existentes

Cada hallazgo de la auditoría ya tiene un lugar natural en la cola priorizada de `CURRENT_ITERATION.md`. La columna "Acción" es lo único nuevo:

| # | Hallazgo del audit | Candidate existente | Acción concreta |
|---|---|---|---|
| 1 | 3 falsedades P0 (§1: Go 1.25, `VerifyPassword`, `storage.driver`) | candidate **#2** *"ADR-010 Phase 4 — Docs-sync + website"* | **Sub-iteración hot-fix (1 día)** previa al Phase 4: corregir las 3 líneas, sin esperar al Phase 4 completo. Justificación: tres líneas, riesgo cero, ROI inmediato. |
| 2 | Páginas faltantes high-impact (plugins, validate, mail, openapi, multi-tenancy, deployment) | candidate **#2** | Núcleo del **Phase 4**. Orden sugerido: plugins → deployment → validate → multi-tenancy → mail → openapi (por impacto comercial). |
| 3 | `concepts/configuration.md` omite 5 capas de validación ADR-010 §2 | candidate **#2** | Parte de Phase 4. Coordinar con el cierre de la iteración layer-3 actual (cuando ella termine, las 5 capas pasan de "4 implementadas" a "5 implementadas" y la doc se reescribe una sola vez). **Dependencia explícita** entre la iteración activa y este punto. |
| 4 | `concepts/models-and-database.md` omite SchemaDrift | candidate **#5** ya existe: *"SchemaDrift end-to-end usage guide en `docs/guides/MODELING_MULTI_DATABASE.md`"* | Auditoría **refuerza** y **amplía**: el guide interno debe también reflejarse en la página pública. Subir candidate #5 de "medio" a "alto" y ampliar scope para incluir `website/docs/concepts/models-and-database.md`. |
| 5 | `concepts/models-and-database.md` + `principles.md` omiten MSSQL/Oracle como engines soportados | candidate **#2** + relación con carry-forward "Oracle reserved-word hardening" | Mencionar build tags en la página pública (trivial). El hardening Oracle es independiente. |
| 6 | CLI overview incompleto; `migrate create/refresh/reset` ausentes; sin reference exhaustiva | candidate **#2** | Auto-generar `website/docs/cli/reference.md` desde `cli.ContractPrimaryCommandNames()` / `internal/cli/root.go` (es lo que el `website-curator` ya sabe que es authoritative). Mantener `overview.md` como onboarding narrativo. |
| 7 | Drift guard solo cubre frontmatter, no body content | candidate **#9** *"Promote advisory website-drift CI job to required gate"* | **Pre-requisito implícito** para #9: antes de promover a required, hay que subir la fidelidad del guard. **Nueva sub-iteración** propuesta: *"Body content scanner en check-coverage.sh"* (§7.4 arriba). Sin esto, promover #9 da una falsa sensación de seguridad. |
| 8 | Veredicto competitivo + plan Fase B/C (showcase, benchmarks, multi-tenancy de primera clase, OTel-first, blog/comunidad) | **ninguno existente** | **Nueva iteración estratégica** (no técnica): `docs/iterations/2026-XX-positioning-and-launch-plan.md`. Es decisión de owner y no encaja en el iteration loop del `architect-reviewer`/`code-reviewer`. Probable cadencia: revisión mensual, no por iteración. |

### 8.3. Reordenamiento sugerido de candidates (delta vs `CURRENT_ITERATION.md`)

Con los hallazgos del audit, la cola priorizada cambia ligeramente. Propuesta — owner confirma:

```
ACTIVA: ADR-010 §2 layer 3 (sin cambios)
   ↓
CANDIDATE 1 [NUEVA]: Hot-fix 3 website P0 falsehoods           [~1 día]
   ↓
CANDIDATE 2 [reforzada]: ADR-010 Phase 4 — Docs-sync + website,
                         AMPLIADA con: plugins/deployment/validate/
                         multi-tenancy pages + cli/reference.md
                         auto-gen + 5 capas de validación + SchemaDrift
                         en página pública + MSSQL/Oracle visibles
                         [target v0.9.X — fits the existing Phase 4 scope]
   ↓
CANDIDATE 3 [NUEVA, pre-req de #9 original]:
   Body content scanner en scripts/website/check-coverage.sh    [~2-3 días]
   ↓
CANDIDATE 4 (era #9): Promote website-drift CI job to required gate
   ↓
CANDIDATE 5 (era #3): Cloud Secrets Provider plugin extraction
CANDIDATE 6 (era #4): Column-type comparison in SchemaDrift
CANDIDATE 7 (era #5, scope ampliada arriba en CANDIDATE 2)
CANDIDATE 8 (era #6): go mod tidy unblock
CANDIDATE 9 (era #7): tasks.Manager struct→interface DEP
CANDIDATE 10 (era #8): Audit §7 menores
   ↓
PARALELO (estratégico, no iteration-loop):
   Positioning & launch plan — Fase B/C del veredicto (§5.3)
```

### 8.4. Por qué este encaje funciona

- **Respeta `CLAUDE.md` §4 (Iteration Loop):** cada hot-fix y cada página nueva pasa por el iteration loop completo. El `website-curator` (paso 8 del loop) es ahora obligatorio para cualquier cambio reader-visible, lo cual era ya el caso pero no se había materializado en cambios concretos.
- **Cierra el bucle del propio audit:** la sub-iteración del body scanner (CANDIDATE 3) garantiza que las 3 falsedades P0 que se nos colaron **no se puedan repetir** cuando se publiquen las páginas nuevas del Phase 4. Es la mejora estructural derivada del propio descubrimiento.
- **No mueve los carry-forwards:** ninguno de los carry-forwards técnicos (Oracle, SameSite, GCS redaction, etc.) se reprioriza. Esta es estrictamente una expansión del scope de Phase 4 + dos nuevas sub-iteraciones cortas (hot-fix + body scanner) + una nueva iteración estratégica separada.
- **El veredicto competitivo se mantiene fuera del iteration loop técnico:** es trabajo de positioning, marketing y comunidad — no de `architect-reviewer`/`code-reviewer`. Vive en su propio archivo y su propia cadencia (mensual). Esto evita contaminar el flujo técnico con decisiones de producto/marketing.

### 8.5. Próxima acción concreta sugerida al owner

1. Al cerrar la iteración layer-3 actual, abrir la mini-iteración hot-fix (1 día) para las 3 P0. Está acotado a 3 ediciones de `.md`, no toca `pkg/*`, no riesgo de contract drift.
2. Inmediatamente después, abrir Phase 4 con el scope ampliado de §8.2 fila 2. Confirmar si el target v0.9.X sigue siendo realista con el scope ampliado.
3. En paralelo y de forma asíncrona, decidir si la iteración estratégica de positioning (§8.2 fila 8) entra o se difiere a post-v1.0.

---

## 9. Playbook operacional con los subagentes del repo (sin romper nada)

> *Esta sección es la respuesta directa a la pregunta del owner: "¿cómo encajamos las tareas del informe usando los subagentes sin romper lo que aún tenemos por delante?". Construida tras leer las definiciones reales en `.claude/agents/` (no inferidas de CLAUDE.md §6) y los slash commands en `.claude/commands/`. Si una salvaguarda parece estricta, lo es por diseño — sigue la disciplina del repo, no la afloja.*

### 9.1. Reglas duras heredadas (NO negociables)

Cualquier playbook debe respetar estas reglas que ya existen en el repo:

| Regla | Origen | Cómo aplica al encaje |
|---|---|---|
| **Two-docs rule** | `website-curator.md` §"two-docs rule" + `doc-updater.md` §"out of scope" | `docs/*` (interno) y `website/docs/*` (público) tienen owners distintos. NADIE cross-edita. doc-updater **delega** al website-curator cuando una página pública necesita cambios. |
| **`examples/*` está vacío hoy (scope invertido)** | `examples-maintainer.md` §"Current state" | Hasta que Phase 4 reintroduzca examples, examples-maintainer solo verifica AUSENCIA. Cualquier mention literal de `examples/*` fuera de `docs/iterations`, `docs/reports`, `docs/audits` es un hallazgo. |
| **Iteration loop con stop-on-blocker** | `iterate.md` §"When a blocker fires" | Cualquier verdict FAIL de cualquier subagente **detiene** el loop. No se sigue por inercia. |
| **`session-curator` es el único que toca `.claude/state/*` y `docs/iterations/*`** | `session-curator.md` §"Hard rules" | Toda apertura/cierre/archivado de iteración pasa por él. |
| **`contract-guardian` se dispara automáticamente** si se tocan `pkg/**/*.go`, `internal/cli/**/*.go`, `contracts/**`, schema de `nucleus.yml` | `iterate.md` step 4 + `contract-guardian.md` §"Method" | Es la red de seguridad: aunque una "iteración solo de docs" accidentalmente arrastre un .go, se detiene antes de mergear. |
| **`website-curator` debe correr `scripts/website/check-coverage.sh` y `npm run build`** | `website-curator.md` §"Method" + `sync-docs.md` step 3 | El verdict no es completo sin estas dos verificaciones. |
| **Absolute dates en state files** | `session-curator.md` §"Hard rules" | "today"/"yesterday" prohibido. Siempre `2026-05-DD`. |
| **`docs/governance/*` solo con aprobación explícita del owner** | `doc-updater.md` línea final + `sync-docs.md` §"What this command does NOT do" | Aunque CI_MATRIX o COMPATIBILITY_SLO parezcan obvios para tocar, requieren confirmación del owner. |

### 9.2. Pre-condición obligatoria: cerrar el WIP de layer-3 **antes** de cualquier nueva iteración

El working tree actual tiene cambios sin commitear en `pkg/nucleus/nucleus.go` + `pkg/nucleus/validate_semantics.go` + test (la iteración ADR-010 §2 layer-3 en curso). Ninguna nueva sub-iteración debe abrirse hasta:

1. La iteración layer-3 **completa** sus acceptance criteria (ver `CURRENT_ITERATION.md` §"Acceptance criteria"). Sigue su propio iteration loop (10 pasos) y termina con `/handoff`.
2. **O** el owner decide explícitamente pausarla: en ese caso, commit del WIP en una rama feature (`feat/adr010-layer3-validate-semantics`), `session-curator` archiva el `CURRENT_ITERATION.md` como WIP en `docs/iterations/2026-05-24-adr010-layer3-WIP.md`, y crea un placeholder para retomarla. Hasta entonces `main` no recibe nada.

Sin esto, mezclar las docs-iteraciones con el WIP de layer-3 contamina el iteration loop (el `contract-guardian` se va a disparar por los cambios en `pkg/nucleus/*`, y el `architect-reviewer` va a revisar la layer-3 cada vez).

### 9.3. Playbook por sub-iteración propuesta

Cada matriz lee: **paso del iteration loop (1-10) → ¿entra el subagente? → razón concreta**. Las celdas SKIP no son ligereza — están justificadas por las reglas de skip explícitas de `iterate.md` y CLAUDE.md §4.

#### Iteración A — Hot-fix de las 3 falsedades P0 (~1 día)

**Scope:** 3 ediciones en `website/docs/**`:
- `website/docs/getting-started/installation.md:12` (`Go 1.25` → `Go 1.26`)
- `website/docs/features/auth.md:86` (`VerifyPassword` → `CheckPassword`, ajustar firma)
- `website/docs/features/storage-and-tasks.md:74` (`driver: s3` → `provider: s3`)

**Opcional concomitante:** añadir `toolchain go1.26.3` a `go.mod` si el owner prefiere conservar floor 1.25 (esto SÍ cae bajo contract-guardian).

| # | Subagente | Verdict esperado | Justificación |
|---|---|---|---|
| 1 | `architect-reviewer` | **SKIP** | Solo docs públicas. Sin SPEC/ADR implications, sin layering changes. |
| 2 | `code-reviewer` | **SKIP** | No hay código. Si se añade `toolchain` a `go.mod`, entra mínimo. |
| 3 | `security-auditor` | **SKIP** | Skip permitido en `iterate.md` step 3: "*Skip only for pure docs/tests changes*". |
| 4 | `contract-guardian` | **SKIP** si no se toca `go.mod`. **PASS-required** si se añade toolchain (cambia config-key… no — `go.mod` no es config-key. Skip igual). | Pasa solo si tocas `pkg/`, `internal/cli/`, `contracts/`, o schema de `nucleus.yml`. Ninguno aplica. |
| 5 | `test-runner` | **SKIP** | Pure docs. |
| 6 | `examples-maintainer` | **SKIP** (verifica ausencia) | Scope invertido hoy; ningún example existe. |
| 7 | `doc-updater` | **SKIP** | Cambio es solo en `website/docs/*` (público). Two-docs rule: no es su scope. Si por revisión cruzada `docs/guides/AUTH_GUIDE.md` también dice `VerifyPassword` (verificar antes), entonces SÍ entra para sincronizar. |
| 8 | `website-curator` | **MANDATORY** | Es el dueño del scope tocado. Corre `check-coverage.sh` (debe seguir en 0 dangling). Corre `npm run build`. Verdict UPDATED. |
| 9 | `changelog-writer` | **OPCIONAL** | Estrictamente es un fix de docs públicas — entra como `docs(website): fix Go version, CheckPassword signature, storage.provider key` en `Unreleased`. Sin bump de semver. |
| 10 | `governance-checker` | **SKIP** | No toca SLO, CI matrix ni release checklist. |

**Cómo invocar:** `/sync-docs website/docs/getting-started/installation.md website/docs/features/auth.md website/docs/features/storage-and-tasks.md` — `sync-docs` ya despacha en orden examples-maintainer → doc-updater → website-curator. Como examples y doc-updater se skip-ean, el comando se reduce de facto al website-curator + reporte.

**Salvaguardas contra romper algo:**
- Branch dedicada `fix/website-p0-3-inaccuracies`. NO directo a main.
- `git diff --stat` antes del commit debe mostrar **solo** 3 archivos `.md` (4 si `go.mod` se incluye). Si arrastra cualquier otra cosa, abortar.
- `check-coverage.sh --strict` debe seguir devolviendo `OK (0/0/0)`.
- `npm run build` en `website/` debe pasar (especialmente porque `onBrokenLinks: 'throw'` está activo, ver `website-curator.md` §"Deploy facts").

#### Iteración B — ADR-010 Phase 4 (docs-sync + website + reference apps; target v0.9.X)

**Esta es la grande.** Se subdivide en 3 sub-iteraciones secuenciales para no convertirla en un mega-PR irrevisable.

##### B.1 — Reintroducir `examples/*` (mvc_api primero; los demás detrás)

**Scope:** crear `examples/mvc_api/` mínimo viable que use la fluent v2 actual (`nucleus.New().FromConfigFile().Use().Mount().Build().Run()`). Es **pre-requisito** de B.2 porque los snippets del website deben provenir de examples reales y compilables.

| # | Subagente | Verdict esperado | Justificación |
|---|---|---|---|
| 1 | `architect-reviewer` | **MANDATORY** | Toca extension points (`Use`, `Mount`), patrón de bootstrap. Verifica layering. |
| 2 | `code-reviewer` | **MANDATORY** | Go idiomatic en código nuevo. |
| 3 | `security-auditor` | **MANDATORY** | Example app implica auth, sesiones, CSRF — secure-by-default debe estar visible. |
| 4 | `contract-guardian` | **MANDATORY** | El ejemplo solo debe usar API estable. Cualquier llamada a símbolo experimental → WARN. |
| 5 | `test-runner` | **MANDATORY** | `go test ./examples/...` + `go build ./examples/mvc_api/...` desde dentro del example. |
| 6 | `examples-maintainer` | **MANDATORY — y aquí RESUME su scope completo.** Actualiza la lista en `examples-maintainer.md` §"Examples in scope". | Es el dueño del scope. |
| 7 | `doc-updater` | **MANDATORY** | `README.md` y `docs/QUICKSTART.md` mencionan los examples. |
| 8 | `website-curator` | **PARCIAL** | Si `getting-started/quickstart.md` referencia un example, sí. Si no, defer a B.2. |
| 9 | `changelog-writer` | **MANDATORY** | `feat(examples): reintroduce mvc_api on fluent v2` — relevant en CHANGELOG. |
| 10 | `governance-checker` | **OPCIONAL** | Examples están listados en el compatibility harness. Si el harness lane cambia, sí. |

**Salvaguardas:**
- Una sub-iteración por example, no todos a la vez. Empezar por `mvc_api` (el más simple) y dejar `fleetmanager`/`ecommerce_dashboard`/`showcase_demo` para iteraciones posteriores.
- NO añadir dependencias nuevas a `go.mod` sin ADR (`examples-maintainer.md` §"Method" 4).
- `examples/*/frontend/node_modules` **jamás** en commit.

##### B.2 — Páginas faltantes en website (plugins, validate, mail, openapi, multi-tenancy, deployment, security, testing, cli/reference)

**Scope:** crear ~9 páginas nuevas en `website/docs/**`. Snippets vienen de los examples de B.1.

| # | Subagente | Verdict esperado | Justificación |
|---|---|---|---|
| 1-5 | architect/code/security/contract/test | **SKIP** | Pure docs en `website/docs/*`. Iteration loop step 3-5 skips por reglas. |
| 6 | `examples-maintainer` | **OPCIONAL** | Si una página nueva referencia un example, examples-maintainer verifica que el snippet citado **siga compilando** en `examples/`. |
| 7 | `doc-updater` | **PARCIAL** | Si una página pública nueva debe ir acompañada de un guide interno actualizado (probable para `multi-tenancy`, `deployment`, `security`, `testing`), doc-updater hace su pass en `docs/guides/*`. **NO** edita `website/docs/*`. |
| 8 | `website-curator` | **MANDATORY** | Crea/edita las páginas con frontmatter completo (`covers:` + `config_keys:`). Corre `check-coverage.sh --strict`. Espera ver el ratio de cobertura subir del ~9% actual. Corre `npm run build`. |
| 9 | `changelog-writer` | **OPCIONAL** | `docs(website): publish plugins/validate/mail/... pages` — entra en `Unreleased` como un solo bullet o múltiples. |
| 10 | `governance-checker` | **SKIP** | No toca SLO/CI/release. |

**Cómo invocar:** `/sync-docs` (probablemente varias veces, una por bloque de páginas). El comando ya orquesta examples-maintainer → doc-updater → website-curator en el orden correcto.

**Salvaguardas:**
- Una **PR por página** (o por bloque coherente de 2-3 páginas). No un mega-PR de 9 páginas.
- Cada página DEBE traer su `covers:` y `config_keys:` manifest completos. Sin esto, `check-coverage.sh` cuenta el ratio mal y el guard pierde efectividad.
- `npm run build` con `onBrokenLinks: 'throw'` — un link interno roto en una página nueva tira el build.

##### B.3 — Reescribir páginas con omisiones (configuration.md → 5 capas; models-and-database.md → SchemaDrift + MSSQL/Oracle; application.md → equivalence)

**Dependencia crítica:** la iteración layer-3 actual **debe estar terminada y mergeada** antes de B.3, porque B.3 documenta "las 5 capas de validación" y no tendría sentido hacerlo a 4. Si layer-3 se difiere, B.3 también.

| # | Subagente | Verdict esperado | Justificación |
|---|---|---|---|
| 1-5 | architect/code/security/contract/test | **SKIP** | Pure docs. |
| 6 | `examples-maintainer` | **OPCIONAL** | Si los snippets nuevos vienen de los examples B.1. |
| 7 | `doc-updater` | **MANDATORY** | `docs/guides/MODELING_MULTI_DATABASE.md` debe coincidir con la nueva sección pública sobre SchemaDrift (candidate #5 ya existe — esto **lo cumple**). |
| 8 | `website-curator` | **MANDATORY** | Reescribe las 3 páginas. Actualiza `covers:` / `config_keys:`. Verifica que el drift guard sigue OK. |
| 9 | `changelog-writer` | **OPCIONAL** | `docs(website): expand configuration, models-and-database, application pages` en `Unreleased`. |
| 10 | `governance-checker` | **OPCIONAL** | No toca SLO directamente, pero clarificar MSSQL/Oracle como build-tagged toca un mensaje histórico de CI_MATRIX (ya hay carry-forward sobre required-vs-exploratory). |

#### Iteración C — Body content scanner en `scripts/website/check-coverage.sh` (~2-3 días)

**Scope:** extender el script con (1) Go-symbol scanner de code blocks `go`, (2) YAML-key scanner de code blocks `yaml`, (3) Go-version pinning vs `go.mod`. Añadir fixtures de páginas malas para testear.

| # | Subagente | Verdict esperado | Justificación |
|---|---|---|---|
| 1 | `architect-reviewer` | **SKIP** | Script de infraestructura, no toca boundaries de `pkg/*`. |
| 2 | `code-reviewer` | **MANDATORY** | Review del bash/awk. Es código real, debe seguir convenciones. |
| 3 | `security-auditor` | **OPCIONAL** | Si el script ejecuta cosas con input del usuario (file paths), un pase ligero. |
| 4 | `contract-guardian` | **SKIP** | No toca `pkg/*` ni stable CLI ni config keys. **PERO** el script **lee** `contracts/baseline/api_exported_symbols.txt` — es solo lectura, no edit, contract-guardian no se ofende. |
| 5 | `test-runner` | **MANDATORY** | Tests del script: corre contra una página fixture con `auth.VerifyPassword` y debe detectarla. Corre contra una página correcta y debe pasar. |
| 6 | `examples-maintainer` | **SKIP** | No relacionado. |
| 7 | `doc-updater` | **OPCIONAL** | Si el script tiene flags nuevas, `docs/reference/*` (probablemente PROJECT_LAYOUT o developer manual) menciona el script. |
| 8 | `website-curator` | **MANDATORY como consumer** | Corre el script extendido contra el website actual y reporta hallazgos. **Esto es el momento donde se descubre si el website actual tiene MÁS falsedades de body content que las 3 que ya conocemos.** Probablemente sí. |
| 9 | `changelog-writer` | **SKIP** | Es scripts/ de tooling interno, no user-facing. |
| 10 | `governance-checker` | **SKIP** (light) | Hasta que C se promueva a required (iter D), no afecta CI matrix de manera obligatoria. |

**Salvaguardas:**
- Los hallazgos nuevos del script (más allá de los 3 P0) se **archivan en otro audit** (`docs/audits/2026-XX-website-body-scan.md`), no se fixean en la misma PR — eso reventaría el scope. Se priorizan en una sub-iteración B.4.
- El script debe tener allowlist para falsos positivos legítimos (ejemplos parciales, snippets pseudo-código).

#### Iteración D — Promover `website-drift` CI job a required gate

**Pre-requisito ESTRICTO:** iter C debe estar shipped. Sin body scanner, promover el drift guard a required da una falsa sensación de seguridad.

**Scope:** `.github/workflows/ci.yml` — quitar el job del set advisory, añadirlo a `ci-required-gate.needs`. Posiblemente también `docs/governance/CI_MATRIX.md`.

| # | Subagente | Verdict esperado | Justificación |
|---|---|---|---|
| 1 | `architect-reviewer` | **OPCIONAL** | CI policy es governance, no architecture estricta. |
| 2 | `code-reviewer` | **MANDATORY** | Review del YAML. |
| 3-7 | security/contract/test/examples/doc | **SKIP** | CI config. |
| 8 | `website-curator` | **SKIP** | Hasta que el job verde sea reproducible localmente, no participa. |
| 9 | `changelog-writer` | **OPCIONAL** | Cambio de gobierno interno, no necesariamente user-facing. |
| 10 | `governance-checker` | **MANDATORY** | `docs/governance/CI_MATRIX.md` debe reflejar el nuevo gate. Aquí entra el owner approval explícito para tocar `docs/governance/*`. |

**Salvaguardas:**
- El cambio se mergea **en una semana de baja actividad**, no antes de un release. Una vez que pasa a required, cualquier dev que rompa una página pública bloquea el CI hasta que el website-curator la arregle.
- El job debe estar verde durante al menos 5 commits consecutivos en main antes de promover (criterio que `CURRENT_ITERATION.md` ya menciona: "Once manifests exist and the job has proven stable over several pushes").

### 9.4. Iteración estratégica paralela (Fase B/C del veredicto — positioning)

**No entra al iteration loop técnico.** Su naturaleza es producto/marketing/comunidad, no shipping de código. Cadencia sugerida: revisión mensual del owner.

Subagentes que SÍ entran ad-hoc cuando una decisión estratégica se materialice en código/docs:

- **Showcase application** (Fase B paso 7): cuando se construya, sigue el iteration loop completo como cualquier example nuevo (recipe = iteración B.1 ampliada).
- **Benchmarks publicados** (paso 8): activa `performance-bench` subagent (mencionado en CLAUDE.md §6 pero no leído por mí — habría que invocarlo bien). Output va a `docs/reports/` (interno) y a una página del website (`website-curator` para publicar).
- **Multi-tenancy de primera clase elevada** (paso 9): si se toca `pkg/app` para hacerlo first-class, dispara `architect-reviewer` con probable necesidad de **nuevo ADR-012**. Después loop completo.
- **OTel-first en `pkg/observability`** (Fase C paso 13): salida de "experimental" → `experimental → stable` requiere ADR + revisión de `contract-guardian` (firewall: no leak de tipos de otel sdk a `pkg/*`).
- **Mensaje y página de v1.0** (Fase D paso 16): pura tarea de `website-curator` + `doc-updater` + `changelog-writer`.

**Salvaguarda:** esta línea NO debe consumir el slot del iteration loop principal. Si en cualquier momento las iteraciones técnicas y las estratégicas compiten por atención, las técnicas ganan (es la disciplina de `CURRENT_ITERATION.md`: una iteración activa a la vez).

### 9.5. Checklist anti-regresión (qué NO puede ocurrir)

Si alguna de estas cosas pasa, **el iteration loop falló**:

1. **NUNCA** un cambio en `website/docs/*` sin verdict explícito del `website-curator` con `check-coverage.sh` corrido y `npm run build` OK.
2. **NUNCA** un cambio en `docs/guides/*` o `docs/reference/*` sin verdict explícito del `doc-updater`.
3. **NUNCA** un cross-edit: doc-updater editando `website/docs/*` o website-curator editando `docs/*` interno.
4. **NUNCA** los carry-forward técnicos en `CURRENT_ITERATION.md` §"Candidate next steps" se borran o reordenan sin que el owner lo confirme explícitamente.
5. **NUNCA** se commitea `examples/*/frontend/node_modules` ni `website/build` ni `website/node_modules`.
6. **NUNCA** se modifica `contracts/baseline/*.txt` para hacer pasar un freeze test. Si el test falla, o se revierte la regresión o se abre un ADR de contract change deliberado (CLAUDE.md §7).
7. **NUNCA** se invoca un subagente con `subagent_type` genérico (`Explore`, `general-purpose`) cuando existe el especializado del repo. Si la plataforma no permite invocar `website-curator` directamente, se le pasa su prompt entero a un agente que adopte el rol — pero **se invoca explícitamente** y se respeta su output contract.
8. **NUNCA** se cierra una iteración (`/handoff`) sin que `session-curator` haya re-leído `git status` y validado que el state file coincide con la realidad del working tree (este fue el "stale post-commit handoff loop" que rompió en sesiones previas — está documentado en el handoff actual).
9. **NUNCA** se abre una nueva iteración sin cerrar la anterior con `/handoff` y archivarla a `docs/iterations/YYYY-MM-DD-<slug>.md`.
10. **NUNCA** una fecha relativa (`today`, `last week`) entra a `.claude/state/*` o `docs/iterations/*`. Solo absolutas (`2026-05-24`).

### 9.6. Resumen ejecutivo del playbook

```
Pre-req: cerrar iteración layer-3 → /handoff → session-curator archiva
                            │
                            ▼
ITER A (1 día): hot-fix 3 P0 → /sync-docs → website-curator (mandatory),
                                          examples/doc-updater skipped
                            │
                            ▼
ITER B.1 (~1 semana): reintroducir examples/mvc_api → /iterate full loop,
                                          examples-maintainer resume scope
                            │
                            ▼
ITER B.2 (~2 semanas): páginas faltantes en website → /sync-docs por bloque,
                                          website-curator mandatory cada vez
                            │
                            ▼
ITER B.3 (~1 semana, requiere layer-3 cerrada): reescritura de configuration/
              models-and-database/application → /sync-docs, doc-updater +
              website-curator mandatory
                            │
                            ▼
ITER C (~2-3 días): body scanner en check-coverage.sh → /iterate light,
                                          code-reviewer + test-runner +
                                          website-curator
                            │
                            ▼
ITER D (~1 día): promote website-drift a required gate → /iterate light,
                                          governance-checker mandatory
                            │
                            ▼
ITERACIÓN ESTRATÉGICA (paralela, mensual): Fase B/C posicionamiento
              → fuera del iteration loop principal
```

Cumpliendo este orden y las salvaguardas de §9.5, **ninguno** de los 14 carry-forwards técnicos vivos en `CURRENT_ITERATION.md` se ve afectado, y la iteración activa de layer-3 termina en su propio camino sin contaminarse con docs work. El audit se convierte en cinco iteraciones cortas (A, B.1, B.2, B.3, C, D) y una iteración estratégica separada, todas reversibles, todas pasando por los subagentes que el owner ya construyó para protegerse.
