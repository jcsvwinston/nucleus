# Nucleus / GoFrame — Auditoría exhaustiva (carriles ejecutados)

> Fecha: 2026-06-21 · Rama en el momento de la auditoría: `main` @ `d85cb7c`
> (árbol de trabajo limpio salvo la auditoría `2026-06-14`, sin commitear de forma
> intencionada).
> Disparada por: la tarea programada `auditora` — reverificar de forma exhaustiva
> la corrección del framework y cazar bugs / vulnerabilidades / falsedades en
> **código y documentación**, midiendo contra el listón de "el mejor framework web
> del mercado". Instrucción permanente: verificar contra el **código**, no contra
> lo que el proyecto documenta sobre sí mismo.
> Estado: **AUDITORÍA COMPLETA. Remediación sin empezar** (ejecución de solo
> informe; según el flujo de `main` protegido, los arreglos aterrizan vía rama → PR
> por el mantenedor).
> Predecesora: `docs/audits/2026-06-14-exhaustive-audit.md`. Esta pasada
> recalifica los hallazgos abiertos de ese informe contra el arco posterior a
> `v0.9.0` — sobre todo la **extracción admin→orbit (ADR-019, PRs #148–155)**, que
> eliminó `pkg/admin` del núcleo — y añade hallazgos de primera mano sobre la
> superficie nueva.

---

## 0. Alcance, método, entorno

Carriles ejecutados en esta pasada:

1. **Funcional** — `go build` / `go vet` / `go test` sobre el módulo raíz.
2. **Contratos** — tests de freeze + firewall; arnés de compatibilidad.
3. **Concurrencia** — `go test -race` sobre todos los paquetes con goroutines y la
   ruta de petición (incluido el nuevo EventBus de `pkg/nucleus`).
4. **CLI** — binario `nucleus` compilado y probado de extremo a extremo + una
   prueba scaffold → generate → **build → run → sondeo-de-endpoints**.
5. **Seguridad / código** — revisión estática de CORS, mail, sesiones, la nueva
   superficie de ADR-019 (`Router.Mount`, `Runtime.DatabaseHandle(s)`, el
   `EventBus` de primera parte, `SessionManager.ActiveSessions`); un arnés CORS en
   tiempo de ejecución; un arnés de arranque del scaffold en tiempo de ejecución.
6. **Fidelidad de la documentación** — verificación de contenido del cuerpo §9
   (símbolos Go vs el baseline de freeze, claves YAML vs `CONFIG_KEY_REGISTRY.md`,
   versiones de Go vs `go.mod`) en las guías + el sitio web público, **además** de
   una ejecución de primera mano de la propia herramienta del repo
   `scripts/website/bodycheck` sobre ambos árboles.

**Entorno — el carril Go se ejecutó de verdad.** Go 1.26.4 (linux/arm64), la caché
de módulos exportada del mantenedor servida offline (`GOMODCACHE` +
`GOPROXY=off`), caché de build en disco local, `GOWORK=off`, `GOTOOLCHAIN=local`,
`GOSUMDB=off`. El módulo raíz (`pkg/*`, `internal/*`, `cmd/nucleus`, `contracts`,
`examples/mvc_api`) se construyó, testeó y pasó por el detector de carreras. Los
motores de la matriz de BD en vivo (PostgreSQL/MySQL/MSSQL/Oracle) **no** se
ejecutaron localmente (sin Docker) — siguen `[ci-delegado]`. El repo **`orbit`**
(el nuevo hogar del panel de administración) es un módulo aparte y **no** se montó
en esta sesión — ver §7.

**Confianza:** `[verificado]` ejecutado/inspeccionado de primera mano en esta
pasada · `[reportado]` evidencia con archivo:línea · `[ci-delegado]` cubierto por
la puerta de CI requerida, no reejecutado aquí.

**Severidad:** P0 ruptura funcional / falsedad documental que un lector copia y
falla en una ruta por defecto · P1 defecto real / deriva de contrato / brecha de
seguridad · P2 higiene/latente · P3 cosmético.

---

## 1. Veredicto ejecutivo

| Pregunta | Veredicto |
|---|---|
| **¿El framework compila, pasa vet, testea y pasa el detector de carreras?** | **SÍ.** `go build ./...` rc=0, `go vet ./...` rc=0, `go test ./pkg/... ./contracts/...` en verde, `go test -race` en verde en todos los paquetes con goroutines + la ruta de petición. Los únicos fallos son el artefacto offline largamente documentado: cinco tests de *build-smoke* de scaffold que ejecutan `go mod tidy` y no pueden resolver las dependencias de test de testify a sus versiones fijadas bajo `GOPROXY=off`. Ningún fallo de aserción. §3. |
| **¿La extracción admin→orbit es limpia a nivel de código?** | **SÍ.** `pkg/admin` desapareció, el baseline de freeze se rebaselineó (los símbolos eliminados están ausentes), la plantilla generada `nucleus.yml` apunta a orbit, y una app generada **compila y arranca** sin admin. El código del núcleo hizo bien su mitad de la migración. §2. |
| **¿La migración dejó falsedades atrás?** | **SÍ — y en la ruta más transitada.** Las **plantillas del scaffold siguen anunciando el admin in-core eliminado** (el banner de `nucleus new`, los comentarios de `main.go` y el `README.md` generado), y el README ordena una edición de configuración que ahora **rompe el arranque**. NUEVO P1 (**S-1**). El sitio web público sigue enviando toda una historia de `/admin` (quickstart "sign in at /admin", una página `features/admin.md`, YAML `admin_prefix:` que rompe el arranque). §4–§5. |
| **¿Los hallazgos de código previos están arreglados?** | **NO — los tres arrastrados.** `CookieSessionStore` sigue silenciosamente no funcional (**N-1, P1**); `cors_origins:["*"]` + credenciales sigue reflejando cualquier origen con credenciales (**N-2, P2**) — ahora *consagrado en un test de regresión que pasa*; `mail.Message.Headers` sigue sin sanear (**N-3, P2**). §4. |
| **¿La documentación es fiel?** | **Falsedad en guía interna todavía viva** (`AUTH_GUIDE.md` enseña 2 claves inexistentes + un campo fantasma — **N-4, P1**, sin cambios desde 2026-06-14). El verificador de contenido §9 que pidieron las últimas tres auditorías **se construyó** (`scripts/website/bodycheck`) — pero **no está cableado a ningún workflow de CI**, solo apunta a `website/docs`, tiene un bug de precisión y se pierde la prosa. §6. |
| **¿Posición de clase empresarial?** | **El núcleo está en buena forma; los bordes y la documentación son el lastre.** Build/test/race/contratos en verde, la nueva superficie de ADR-019 está bien construida y libre de carreras, pero los mismos pequeños agujeros reaparecen auditoría tras auditoría porque la corrección de la documentación todavía no se fuerza y los hallazgos de código arrastrados no se han recogido. §8. |

**Posición en una línea:** Nucleus es un **framework que compila limpio, libre de
carreras, estable en contratos y que ejecutó bien una refactorización dura
(admin→orbit) a nivel de código** — pero envió esa refactorización con su
**experiencia estrella de primer arranque (el scaffold) todavía describiendo una
funcionalidad que ya no existe**, y sigue arrastrando todos los hallazgos
código/documentación de la auditoría anterior. Ninguno es profundo; todos son del
tamaño de un PR; el bloqueo hacia "el mejor del mercado" es seguimiento, no
arquitectura.

---

## 2. Lo que el arco posterior a v0.9.0 hizo bien — `[verificado]`

Crédito donde toca, confirmado contra el **código** en esta pasada:

- **La extracción admin→orbit (ADR-019) es limpia en la fuente.** `pkg/admin`
  está eliminado; `MountAdmin`, `AdminAgentConfig`, `App.Admin` están ausentes de
  `contracts/baseline/api_exported_symbols.txt` (rebaselineado, no falseado); el
  registro de configuración marca `admin_prefix`/`admin_title`/`admin_bootstrap_*`
  como `removed` con un puntero a `modules.orbit.*`, y `admin_rbac_policy_file`
  como alias `deprecated` de `rbac_policy_file`. Una app generada compila y arranca
  sin admin y sin referencias colgantes en su `nucleus.yml`.
- **La nueva superficie pública de ADR-019 está genuinamente bien diseñada.**
  `SessionManager.ActiveSessions`/`SessionInfo` (`pkg/auth/session_enumerate.go`)
  lleva un docblock `SECURITY:` ejemplar (Token es una credencial bearer, Values
  puede contener secretos, solo para operador in-process), maneja manager nil,
  stores no iterables, y payloads no decodificables. El `EventBus` de primera parte
  (`pkg/nucleus/eventbus.go`) es código concurrente cuidadoso: disciplina de
  Release, cancel con `sync.Once`, drenaje al cancelar, y una copia de slice para
  evitar aliasing del array subyacente. El redirect canónico de `Router.Mount`
  apunta al patrón estático suministrado por el desarrollador (sin open-redirect
  desde la entrada de la petición). Todo libre de carreras (§3).
- **Los arreglos del arco previo siguen sosteniéndose** (revisados de nuevo): F-3
  rebind de dialecto CRUD, F-4 firewall `/vN`, SEC-1 default de credenciales CORS,
  allow-list de `OrderBy`, `Router.Resource("")`, frescura de `scaffoldGoVersion`
  del CLI. Ninguno regresó.

---

## 3. Carriles funcional, contratos, concurrencia, CLI — **PASA** `[verificado]`

| Paso | Resultado |
|---|---|
| `go build ./...` | **rc=0** |
| `go vet ./...` | **rc=0** |
| `go test ./pkg/...` | **todos PASAN** (25 paquetes) |
| `go test ./contracts/...` | **PASA** (freeze + firewall + arnés) |
| `go test ./internal/... ./cmd/... ./examples/...` | **PASA salvo los 5 tests de smoke offline-tidy documentados** — `TestRunGenerateResourceBuilds`, `TestRun_GenerateResource`, `TestRun_GenerateModelAndHandler`, `TestRun_StartAppScaffold`. Cada fallo es de la *misma* clase: el test ejecuta `go mod tidy` dentro de un proyecto scaffoldeado y la caché offline tiene `go-spew`/`go-difflib` solo en pseudo-versiones, no las versiones fijadas `v1.1.1`/`v1.0.0` de testify. **No es un defecto de código** — ver la prueba abajo. |
| `go test -race ./pkg/{nucleus,signals,outbox,circuit,health}` | **PASA, libre de carreras** (incluido el nuevo EventBus/runtime) |
| `go test -race ./pkg/{router,auth,tasks,observability,db}` | **PASA, libre de carreras** |
| Smoke del CLI (`version`, `new`, `generate resource`, run, sondeo) | **todos rc=0** |

**Prueba de compilación del código generado `[verificado]`.** Scaffoldeé
`nucleus new myapp`, `generate resource Widget name:string price:int`, apunté el
módulo al nucleus local vía `replace`, y ejecuté `go build ./...` → **rc=0**.
Luego compilé y **ejecuté** la app y sondeé endpoints: `/healthz` → **200**,
`/admin` → **403**, `/_/config` → **403**. El código generado es correcto y
arranca; el resultado `/admin` es la evidencia para S-1 abajo.

---

## 4. Hallazgos — código

### S-1 · P1 · `[verificado, confirmado en runtime]` — el scaffold sigue anunciando el admin in-core eliminado, y su README ordena una edición que rompe el arranque

Esto es **nuevo en esta pasada** y está en la única ruta más transitada de todo
el proyecto: lo que un usuario nuevo obtiene de `nucleus new`. La migración
admin→orbit actualizó la plantilla `nucleus.yml` (dice correctamente *"To add an
admin UI, mount the orbit module"*) pero dejó **tres superficies hermanas
obsoletas**:

1. **`internal/cli/new.go:114`** — para la plantilla por defecto (`mvc`) el CLI
   imprime `Running endpoints: http://localhost:8080/admin  (and /healthz)`.
   No hay `/admin` — confirmado en runtime **403**.
2. **`internal/cli/scaffold/templates/mvc/main.go.tmpl:21-25`** — los comentarios
   del `main.go` generado afirman *"the admin panel is mounted at /admin"*, que
   `admin_rbac_policy_file` *"grants anonymous access"*, y que *"the app serves
   /admin and the built-in endpoints."* Todo falso (y el `nucleus.yml` generado en
   realidad usa `rbac_policy_file`, no `admin_rbac_policy_file`, así que el
   comentario es incluso internamente inconsistente con el archivo que tiene al
   lado).
3. **`internal/cli/scaffold/templates/_common/README.md.tmpl:16-26`** — el README
   generado tiene toda una sección *"First boot: admin account & password"*: le
   dice al usuario que el primer arranque crea un admin de bootstrap desde
   `admin_bootstrap_email`, que *"sign in at /admin"*, y — el filo cortante — que
   *"set `admin_bootstrap_password` in `nucleus.yml` and reboot."*

**Por qué es P1, no cosmético — prueba de daño en runtime.** `admin_bootstrap_email`
y `admin_bootstrap_password` tienen estado de registro **`removed`**. La app
generada usa carga de configuración *estricta*, así que seguir la propia
instrucción del README inutiliza la app:

```
$ printf '\nadmin_bootstrap_password: hunter2\n' >> nucleus.yml && ./myapp
myapp: nucleus: unknown configuration key(s) in nucleus.yml:
  - admin_bootstrap_password
```

La experiencia literal de primer arranque de un usuario nuevo es un README que
describe un panel inexistente y le entrega un paso de remediación que se niega a
arrancar.

**Arreglo:** actualizar las tres superficies para que coincidan con la plantilla
`nucleus.yml` ya correcta — quitar la línea de banner `/admin` (o apuntarla a
orbit), reescribir el bloque de comentarios de `main.go.tmpl`, y reemplazar la
sección admin del README con el puntero al módulo orbit. Dueño:
`examples-maintainer` es el agente equivocado (esto son plantillas del CLI); rutar
por la vía CLI/`doc-updater`. Añadir una aserción de smoke del scaffold de que el
README/banner generado no contiene `/admin`.

### N-1 · P1 · `[verificado]` — `auth.CookieSessionStore` sigue siendo un store exportado, congelado y silenciosamente no funcional *(arrastrado, sin cambios)*

`pkg/auth/session_store_cookie.go`. `CommitCtx` (99-128) todavía marshalea,
encripta, codifica en base64 el payload — **y lo descarta**:

```go
encoded := base64.URLEncoding.EncodeToString(ciphertext)
// Store the encrypted data in the session using the token as key
_ = encoded // In a real implementation, this would be set as a cookie   // línea 126
return nil  // reporta éxito
```

Nada se persiste; toda lectura falla. El godoc (línea 15) sigue afirmando
*"CookieSessionStore persists sessions in encrypted cookies"* — una falsedad
viviendo en código enviado. Sigue siendo **API pública congelada** (baseline
líneas 251, 266-273, 348) y sigue **sin test de ida y vuelta** en ningún lugar de
`pkg/auth` (confirmado con grep). Mitigante sin cambios: no seleccionable por
configuración (`session_store` solo acepta memory/sql/redis), alcanzable solo vía
`SetStore`. Señalado en la auditoría 2026-06-14; **no recogido.** Opciones de
arreglo sin cambios: implementarlo de verdad (necesita cooperación del middleware
— un cambio de diseño), hacerlo fallar ruidosamente (`"not implemented"`), o
deprecar-y-eliminar. Rutar por `contract-guardian`.

> Corolario (P3): el docstring de `SessionManager.ActiveSessions` afirma que el
> cookie store devuelve `ErrSessionStoreNotIterable`, pero
> `CookieSessionStore.AllCtx` devuelve un mapa vacío, así que `ActiveSessions`
> sobre un cookie store devuelve silenciosamente un **slice vacío**, no el error
> centinela. Otra pequeña mentira que existe solo porque N-1 nunca se resolvió.

### N-2 · P2 · `[verificado, confirmado en runtime]` — `cors_origins:["*"]` + `cors_allow_credentials:true` sigue reflejando cualquier Origin con credenciales *(arrastrado; ahora fijado por un test)*

Reconfirmado en runtime contra el código **actual** (la refactorización FW-6
cambió la forma pero no el resultado). Arnés vía el `router.CORSMiddleware`
público:

```
config: cors_origins=["*"], cors_allow_credentials=true
request Origin: https://evil.attacker.example
-> Access-Control-Allow-Origin     = https://evil.attacker.example
-> Access-Control-Allow-Credentials = true
```

Cualquier sitio puede leer respuestas autenticadas cross-origin. Mecanismo sin
cambios: `pkg/app/app.go:268` condiciona el pase de credenciales a
`len(CORSOrigins) > 0` — y `["*"]` tiene longitud 1, así que toma esa rama; solo
la lista *vacía* está protegida (`else if`, 273). `pkg/router/corsmw.go` entonces
refleja el origen porque el cortocircuito `allowAll && !AllowCredentials` (línea
~71) es falso cuando las credenciales están activas.

**Nuevo matiz:** `pkg/router/corsmw_fw6_test.go:15`
(`TestCORSMiddleware_WildcardWithCredentialsReflectsOrigin`) ahora **asevera este
comportamiento como correcto**. El trabajo de FW-6 arregló el modo de fallo del
*header inválido* (`*` + credenciales, que los navegadores rechazan) pero codificó
el modo de fallo de *seguridad* (reflejar-cualquier-origen + credenciales) como el
resultado pretendido. El propio comentario de la app (app.go:264-265) nombra
exactamente este desenlace como aquello a prevenir. **Arreglo:** rechazar allow-all
+ credenciales en la ruta de carga de `pkg/nucleus` (error de arranque ruidoso) y
descartar credenciales con un WARN en `CORSMiddleware` cuando `allowAll`; reapuntar
el test FW-6 a ese rechazo. Un framework del mejor del mercado rechaza el
footgun en vez de enviar un test que lo bendice.

### N-3 · P2 · `[verificado]` — inyección de cabeceras SMTP vía `mail.Message.Headers` sin sanear *(arrastrado, sin cambios)*

`pkg/mail/mail.go` `validateMessage` (208-245) protege `From`/`Subject` contra
`\r\n` y valida los destinatarios con `ParseAddress`, pero **nunca inspecciona el
mapa `Headers`** (la función retorna en 245 sin referencia a `msg.Headers`).
`pkg/mail/message.go:23-28` todavía solo hace `TrimSpace` a cada valor (el CRLF
interior sobrevive) y nunca valida la clave, luego escribe `key: value` en el
mensaje de cable unido por `\r\n`. Un valor `"x\r\nBcc: attacker@evil.com"`
inyecta una cabecera arbitraria. Latente (ninguna ruta del framework enruta
entrada no confiable a `Headers` por defecto → lo mantiene en P2), pero el
framework es dueño del ensamblado SMTP, así que es dueño del saneo. **Arreglo:** en
`validateMessage`, rechazar claves de cabecera que no sean tokens válidos de
field-name RFC-822 y cualquier valor que contenga `\r`/`\n`.

### Revisión de superficie nueva (ADR-019) — `[verificado]`, sin defectos

`Router.Mount`, `Runtime.DatabaseHandle(s)`, `EventBus` y
`SessionManager.ActiveSessions` se revisaron línea por línea y son sólidos (§2).
Dos notas de baja severidad, ambas P3:

- **`EventBus.HTTPEvent.PayloadPreview`** enmascara claves de query que contienen
  `KEY/SECRET/PASSWORD/TOKEN` pero, según su propia doc, **no** `code`/`state` de
  OAuth. Un `code` de autorización OAuth es un secreto bearer de vida corta; en un
  feed solo-operador esto está documentado y es de bajo riesgo, pero enmascarar
  también `code`/`state` lo cerraría.
- **`Runtime.DatabaseHandle(s)`** entregan a un módulo el `*db.DB` crudo del
  framework, que **evita** el scoping por petición de tenant de `DBForRequest`. Por
  diseño y documentado ("framework-owned; do not Close"), pero merece una advertencia
  de una línea en el godoc de que las consultas tenant-aware deben usar la ruta con
  scope de petición.

---

## 5. Hallazgos — documentación (falsedades)

### N-4 · P1 · `[verificado]` — `docs/guides/AUTH_GUIDE.md` sigue enseñando dos claves de configuración inexistentes y un campo Go fantasma *(arrastrado, sin cambios)*

- **L468-469** `authz_model_path:` / `authz_policy_path:` — **ausentes** de
  `CONFIG_KEY_REGISTRY.md`. La carga estricta rechaza claves desconocidas, así que
  un lector que copie este bloque `# nucleus.yml` **falla al arrancar**. Clave
  canónica: `rbac_policy_file` (el modelo Casbin es interno; no hay clave de
  model-path).
- **L531** `enforcer, err := authz.New(logger, cfg.AuthzPolicyPath)` —
  `cfg.AuthzPolicyPath` está **ausente** del baseline de freeze *y* de la fuente de
  `pkg/app`; el snippet **no compila**. Campo canónico: `cfg.RBACPolicyFile`.

El gemelo del sitio web público se arregló hace tiempo; solo la guía interna se
rezaga — porque el verificador de contenido del cuerpo no apunta a `docs/guides`
(§6). Rutar por `doc-updater` → `docs-content-verifier`.

### D-WEB · P1 · `[verificado]` — el sitio web público sigue enviando una historia de `/admin` in-core que ya no existe

La ruptura admin→orbit dejó al sitio web (`website/docs/**`) cargando la vieja
superficie admin a lo largo de ~11 páginas. Los casos más dañinos, el-lector-copia-y-falla:

- **`website/docs/getting-started/quickstart.md`** (la página de onboarding más
  transitada) — L42/L47 anuncian `/admin` para el esqueleto `mvc`, y L169-177 tiene
  toda una sección *"5 — Create an admin user … sign in to the admin panel at
  /admin"*. `/admin` es un 403; no hay flujo de usuario admin.
- **`website/docs/features/admin.md`** — toda una página para la funcionalidad
  eliminada; su frontmatter `covers:` lista `pkg/app.App.MountAdmin` /
  `App.RegisterAdminModels` (ausentes del baseline).
- **YAML que rompe el arranque** en bloques copy-paste: `features/admin.md:30` y
  `concepts/configuration.md:99` ambos muestran `admin_prefix: /admin` — una clave
  `removed` que falla en la carga estricta.

Este clúster es el **job de CI "Website Docs Drift (advisory)" que falla**
(`.github/workflows/ci.yml:373`, que ejecuta `check-coverage.sh --strict`) y ya
está capturado por el chip de seguimiento abierto **`task_b8cbc177`**. Está
trackeado — pero sigue vivo, así que pertenece a esta auditoría. Nótese que el job
de CI está marcado como **advisory** (no bloqueante), por lo que main está "verde"
mientras envía la deriva; considerar hacerlo bloqueante una vez resuelto. Rutar por
`website-curator`.

### N-5 · P3 · `[verificado]` — deriva §9 residual *(arrastrado)*

- `docs/guides/ERROR_HANDLING.md:434` — prosa "Go 1.13" (el piso es 1.26). El
  propio `bodycheck` del repo lo marca como violación dura cuando se apunta a las
  guías (§6).
- `CONTRIBUTING.md:10` — "matches the `go 1.26.3` directive in `go.mod`";
  `go.mod` declara **`go 1.26.4`**. (README/QUICKSTART/installation son correctos.)
- Los bloques históricos/ilustrativos de `docs/guides/{MAIL,STORAGE}_GUIDE.md`
  (`sendgrid_api_key`, `s3_bucket`, `s3_region`) siguen sin el marcador exacto
  `# illustrative` / `# deprecated, use …` que §9 requiere.

---

## 6. Estructural — la guarda de contenido del cuerpo §9 se construyó pero está dormida `[verificado]`

Esta es la actualización estructural de titular, y es un resultado *mixto*.

**Buena noticia:** el verificador de contenido del cuerpo que las últimas tres
auditorías pedían **ahora existe** — `scripts/website/bodycheck/main.go`. Verifica
(1) afirmaciones de versión de Go vs `go.mod` [dura], (2) referencias `pkg.Symbol`
en bloques cercados ```go vs el baseline de freeze [dura], y (3) claves YAML vs el
registro de configuración [advisory].

**Mala noticia — no puede cazar las falsedades de hoy, por cuatro razones
independientes:**

1. **No está cableado a ningún workflow de CI.** `grep -rl bodycheck .github/` →
   *nada*. La única puerta documental en CI es `check-coverage.sh` (solo
   dangling-`covers:` de frontmatter). La herramienta está construida y dormida.
2. **Solo apunta a `website/docs`.** El default `-docs website/docs` significa que
   `docs/guides/**` — donde viven **N-4** y **N-5** — nunca se escanea.
   *Demostración:* `go run ./scripts/website/bodycheck -docs docs/guides -strict`
   sale con **1** y marca `ERROR_HANDLING.md:434` (N-5) de inmediato. Una sola flag
   lo sacaría a la luz; no se ejecuta.
3. **Se pierde la prosa.** Las falsedades de `/admin` en el quickstart y el
   rompe-arranque `admin_prefix` son prosa / YAML plano, no símbolos
   package-qualified en bloques ```go — así que incluso sobre `website/docs`
   reporta **0 violaciones duras**. La deriva la caza solo el chequeo de
   *frontmatter*.
4. **Tiene un bug de precisión que generaría falsos positivos en las guías.** Al
   correrlo sobre `docs/guides` marca `app.DatabaseForRequest` y `app.Database`
   (MULTISITE_GUIDE) como "not in baseline" — pero esos métodos **sí existen**
   (`App.DatabaseForRequest`/`App.Database`, baseline 155-156, `pkg/app/app.go:993`).
   La herramienta malinterpreta el receptor `h.app.Method` como un cualificador de
   paquete `app.` y no puede emparejar métodos de `App` invocados sobre un valor.
   Este riesgo de falsos positivos es plausiblemente *por qué* no se ha apuntado a
   las guías — y debe arreglarse antes de poder hacerlo.

**Recomendación (sin cambios en espíritu, más afilada en detalle):** (a) arreglar
el parseo `h.app.`/`App.method` para que la herramienta sea confiable sobre código
real; (b) extender su alcance a `docs/guides/**` y `docs/reference/**`; (c)
cablearla en CI como carril **bloqueante**. Hasta que (a)–(c) aterricen, las
falsedades de guía clase N-4/N-5 seguirán reapareciendo — esta es la cuarta
auditoría consecutiva que lo dice. La disciplina del subagente
`docs-content-verifier` sigue siendo necesaria incluso tras cablear la herramienta,
porque la herramienta deliberadamente omite símbolos cualificados localmente como
`cfg.AuthzPolicyPath` (exactamente el campo fantasma de N-4).

---

## 7. Alcance movido a `orbit` — re-domiciliar los hallazgos admin arrastrados `[reportado]`

`pkg/admin` ya no existe en este repo, así que los hallazgos del área admin de las
auditorías previas — **C-1** (`sanitizeNext` same-origin `/admin/../x`), **SEC-2**
(INSERT de bootstrap admin con `fmt.Sprintf`), **SEC-4** (confianza en
`X-Forwarded-For` en `RealIP`/rate-limiter), **SEC-5** (la subida admin usa
`header.Filename` crudo), y el arreglo del **oráculo de timing de login** — ahora
viven en el módulo separado `orbit` (`github.com/jcsvwinston/orbit`). **No fueron
auditables aquí** en esta pasada.

**Acción:** el repo `orbit` nunca ha tenido su propia pasada de seguridad dedicada
y ahora es dueño de la superficie más sensible de seguridad del stack (authn admin,
gestión de sesión/RBAC, subida de archivos). Programar una ejecución equivalente a
`auditora` contra `orbit` (montar el repo, reverificar C-1/SEC-2/4/5 + timing de
login contra el código movido). Hasta entonces, tratar esos cinco como
*arrastrados, no verificados, en orbit*.

---

## 8. Cuadro de mando de preparación empresarial (actualizado desde 2026-06-14)

| Track | 2026-06-14 | Ahora (2026-06-21) | Nota |
|---|---|---|---|
| A — Freeze de contrato e inventario | HECHO | **HECHO** | rebaselineado limpiamente a través de la eliminación admin; freeze/firewall en verde. |
| B — Arnés de compatibilidad | PARCIAL | **PARCIAL** | sigue solo `core-build`. Sin cambios. |
| C — Firewall de dependencias | HECHO | **HECHO** | en verde. |
| D — Cobertura de datos empresarial | ~95% | **~95%** | la portabilidad de dialecto CRUD se sostiene; PG en vivo gateado en CI. |
| E — Baseline de seguridad y cumplimiento | EMPEZADO | **EMPEZADO** | sin movimiento en N-1/N-2/N-3; el código admin sensible se movió a orbit (ahora sin auditar allí). CSRF sigue opt-in; sink de auditoría sigue en memoria. |
| F — Integración cloud | PARCIAL | **PARCIAL** | sin cambios. |
| G — Productividad del desarrollador | FUERTE | **REGRESÓ a BUENO** | codegen/CLI siguen fuertes, pero S-1 significa que la *propia experiencia de primer arranque* ahora envía una falsedad que inutiliza el arranque si se sigue. Esta es la verruga más visible para el usuario del proyecto. |

**Distancia a "el mejor del mercado":** arquitectónicamente corta, estancada en
ejecución. La refactorización del núcleo aterrizó bien; la brecha es que **cada
hallazgo de la auditoría anterior sigue abierto** y la cola documental/scaffold de
la migración no se barrió. Toda la remediación es del tamaño de un PR.

---

## 9. Hoja de ruta de remediación priorizada (del tamaño de un PR, vía `main` protegido)

**Sprint 1 — dejar de enviar la falsedad en la ruta por defecto:**
1. **S-1** — barrer el residuo admin del scaffold (`new.go:114`,
   `main.go.tmpl:21-25`, `README.md.tmpl:16-26`) para que coincida con la plantilla
   `nucleus.yml` ya correcta; añadir una aserción de smoke del scaffold de que el
   README/banner generado no contiene `/admin`.
2. **D-WEB** — resolver `task_b8cbc177` (website-curator): retirar/reescribir
   `features/admin.md`, la sección admin del quickstart, y el YAML `admin_prefix:`;
   añadir el puntero a orbit. Considerar pasar el job de deriva a bloqueante.
3. **N-4** — arreglar las claves de `AUTH_GUIDE.md` (`rbac_policy_file`) + campo
   (`cfg.RBACPolicyFile`) vía `doc-updater` → `docs-content-verifier`.

**Sprint 2 — los hallazgos de código arrastrados + hacer real la guarda:**
4. **N-1** — hacer `CookieSessionStore` real o ruidosamente no implementado; añadir
   un test de ida y vuelta a través de `SessionManager`. (`contract-guardian`.)
5. **N-2** — rechazar allow-all + credenciales en la carga; descartar credenciales
   con WARN en `CORSMiddleware` cuando `allowAll`; reapuntar el test FW-6 al rechazo.
6. **Endurecimiento + cableado de bodycheck** (§6): arreglar el parseo
   `h.app.`/`App.method`, extender el alcance a `docs/guides` + `docs/reference`,
   cablearlo en CI como carril bloqueante. Este es el arreglo estructural de mayor
   apalancamiento.

**Sprint 3 — cola de endurecimiento:**
7. **N-3** — validar claves/valores de `mail.Message.Headers` contra CR/LF + forma
   de token.
8. **N-5** — barrido de versión de Go + marcadores ilustrativos (ERROR_HANDLING,
   CONTRIBUTING, guías MAIL/STORAGE).
9. **Pasada de seguridad de orbit** (§7) — re-domiciliar y reverificar
   C-1/SEC-2/4/5 + timing de login contra el código movido.

---

## 10. Lo que aguantó (no gastar esfuerzo aquí)

Build, vet, la suite completa unit/integración, las puertas de
freeze/firewall/arnés, y el detector de carreras están todos en verde; el CLI
scaffoldea, genera, compila y arranca; la extracción admin→orbit es limpia a nivel
de código y la nueva superficie de ADR-019 (Mount, EventBus, ActiveSessions,
DatabaseHandle) está bien construida, bien documentada y libre de carreras. El
núcleo está sano. El trabajo está en los bordes — un scaffold/web que no terminó la
cola documental de la migración, tres hallazgos de código arrastrados, una falsedad
de guía arrastrada, y una guarda §9 que existe pero no está encendida.

---

## 11. Limitaciones

- Los motores de la matriz de BD en vivo (pg/mysql/mssql/oracle) fueron
  `[ci-delegado]`, no ejecutados localmente (sin Docker). La evidencia en runtime de
  N-2 es contra el `CORSMiddleware` in-process, que es el código exacto que la app
  cablea.
- El repo `orbit` no se montó; los hallazgos arrastrados de §7 están sin verificar
  allí.
- Los cinco tests de *build-smoke* de scaffold no pueden ejecutarse offline (las
  dependencias solo-test de testify ausentes en las versiones fijadas); la
  compilación **y el arranque** del código generado se probaron de primera mano vía
  `replace` en su lugar.
- La revisión de seguridad es estática más dos arneses en runtime (CORS, arranque
  de scaffold); sin pentest/fuzzing. Benchmarks fuera de alcance.
