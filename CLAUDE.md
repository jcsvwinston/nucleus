# Nucleus — instrucciones para Claude Code

> Se carga al inicio de sesión en el repo **nucleus**. Mantenlo conciso.
> Nucleus es uno de los tres productos de la suite **Quantum** (paraguas
> `quantum/`), con repo, release y cadencia propios.

## Qué es Nucleus

Framework web MVC + REST para Go, stdlib-first (`net/http`, `database/sql`,
`log/slog`, `context`). CLI determinista estilo Django (`nucleus`). El panel
de administración NO vive aquí: se extrajo al módulo
[orbit](https://github.com/jcsvwinston/orbit) (ADR-019) y se monta
in-process sobre los accessors de `nucleus.Runtime`.

## Estado real

- La versión publicada es **v1.23.1** <!-- x-release-please-version -->
  (release-please reescribe esta línea en cada release y
  `scripts/ci/check_version_claims.sh` falla si deriva — este fichero llegó
  a dar contexto de arranque con meses de retraso; por eso ya no lleva
  fechas ni números escritos a mano).
- **Go**: la fuente es la directiva de `go.mod` (no escribas el número en
  prosa; `check_version_claims.sh` vigila que el scaffold la refleje).
- Superficies estables congeladas por los baselines de
  `contracts/baseline/` (API, CLI, claves de config, superficie de
  extensión, postura de seguridad). Cambiarlas a propósito ⇒
  `make regen-baselines` en el MISMO cambio.

## Documentos con autoridad (precedencia alta → baja)

1. `README.md`
2. Contratos y gobernanza: `docs/reference/API_CONTRACT_INVENTORY.md`,
   `docs/reference/CLI_CONTRACT_MATRIX.md`,
   `docs/reference/CONFIG_KEY_REGISTRY.md`,
   `docs/governance/COMPATIBILITY_SLO.md`
3. `SPEC.md` (baseline re-sincronizado por arcos; entre baselines, el ADR
   más nuevo de `docs/adrs/` gana sobre su prosa)
4. Guías internas (`docs/guides/`)

La narrativa VIVA de usuario es el sitio público (`website/docs/`); las
guías internas que la duplican llevan banner de puntero y ceden ante el
sitio.

## Mapa rápido

| Ruta | Qué es |
|---|---|
| `pkg/` | Superficie pública estable (no hay `pkg/admin`; ADR-019) |
| `internal/cli/` | Implementación de la CLI |
| `contracts/` | Baselines congelados + tests de freeze |
| `drivers/*`, `exporters/*`, `providers/*` | Los doce módulos opcionales (ADR-030/031), cada uno con `go.mod` y tag propios: cinco drivers, dos exportadores, tres backends de storage, `secrets-aws` y `ldap`. Pinan la última release de nucleus; `scripts/ci/check_modules_standalone.sh` exige que compilen sin workspace |
| `examples/` | `mvc_api` (la app de referencia; módulo propio que importa `drivers/sqlite`) y `showcase_demo` (suite entera; módulo propio) |
| `website/` | Docusaurus del sitio público (docs EN INGLÉS) |
| `docs/adrs/` | Decisiones; los directorios `iterations/`, `audits/`, `reports/` son actas históricas |
| `scripts/ci/` | Los guards que `make check` ejecuta |

## Cómo se trabaja aquí

1. **Rama + PR**, nunca commits directos a `main`. Commits convencionales
   **en español**, mensajes que explican el porqué. Sin emojis ni hype.
2. **`make check` antes de abrir el PR**: vet + todos los guards locales
   (claims de versión, voz de docs, índice de ADRs, marcadores de docs
   versionadas, deriva de docs internas, frescura del archivo, pins de
   examples, freeze de contratos, referencia de configuración generada,
   cobertura del sitio) + tests. Las lanes pesadas (matriz de BD,
   jobs-redis, storage-minio, smoke del showcase) corren solo en CI.
3. **Docs en el mismo PR que la API** (cultura de la suite, QADR-0003) — y
   el código SIEMPRE se fusiona antes que la prosa que lo cita
   (`check_internal_docs_drift.sh` trata un fichero de una rama sin
   fusionar como inexistente).
4. **No toques**: `website/versioned_docs/` (snapshots inmutables), ramas
   `release-please--*`, CHANGELOG.md ni números de versión (los gestiona
   release-please).
5. Principios innegociables (`SPEC.md` §2): stdlib-first, configuración y
   ciclo de vida explícitos, compatibilidad por contrato, seguridad por
   defecto, SQL-first. Desviarse es arquitectónicamente significativo ⇒ ADR.

## Coordinación de sesiones

El trabajo se coordina desde el paraguas (`quantum/` y su `/next-session`),
no desde este repo. El antiguo protocolo de handoff de `.claude/state/`
(HANDOFF/CURRENT_ITERATION) se cerró el 2026-06-21 al mover la coordinación
al paraguas; sus actas viven en `docs/iterations/` (ver su README). Los
subagentes de `.claude/agents/` siguen disponibles como revisores
especializados (contratos, seguridad, docs) cuando un cambio lo pida.
