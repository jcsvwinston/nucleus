# Ronda de hallazgos — demo externa `quantum-coverage-demo` (2026-08-25)

Re-verificación de la demo externa contra lo último publicado
(nucleus v1.12.0 / quark v1.6.0 / orbit v1.6.7). Once hallazgos en tres
repos, dos de ellos de seguridad. Este fichero es el rastro de auditoría de
la parte que toca a nucleus; las notas públicas de release describen los
mismos cambios sin los identificadores internos.

Prompt de origen con los repros ejecutados y el EXIT de cada uno:
`~/Documents/Claude/Projects/Quantum/auditoria/PROMPT_CODE_FIX_RONDA_2026-08-25.md`.

## Cerrados en v1.12.1

| Id | Clase | Qué era |
|---|---|---|
| QCD-FW-13 | ADR-022 | Un módulo con `Prefix` no podía declarar su propia raíz de montaje: el objeto más corto declarable era `"/"` → `"<prefix>/"`, y el enforcer casa con `keyMatch`, donde `/consola` y `/consola/` son rutas distintas. `CSRFExempt` tenía la imagen especular, por matchear con prefijo crudo. |
| QCD-FW-15 | gobernanza / seguridad | `CSRFExempt` sin veto del operador ni rastro en el arranque. Un módulo SIN `Prefix` declarando `"/"` apagaba CSRF en toda la aplicación. |
| QCD-FW-16 | storage | `S3Store.Delete` no alcanzaba nunca el bucket público: `RemoveObject` es idempotente, así que el bucket privado devolvía `nil` siempre y el bucle retornaba ahí. `isS3NotFound` era código muerto en esa función. |
| QCD-FW-17 | config | Una entrada no parseable de `trusted_proxies` se descartaba en silencio; con una sola entrada la lista quedaba vacía. |
| QCD-FW-18 | seguridad | `doctor --check security` juzgaba `trusted_proxies` entrada a entrada: un catch-all partido pasaba limpio. El mensaje nombraba además la cabecera equivocada. |
| QCD-FW-14 | testing | `nucleustest` remitía a fijar `Databases` en la config y `*AppBuilder` no tenía setter. La capa de entorno redirigía la base bajo el test sin decirlo. |
| QCD-CLI-6 | CLI | `nucleus version` reportaba `dev` para un binario instalado del proxy a versión exacta. |

## Lo que la ronda confirmó como correcto

`nucleus generate module` ejecutable en ambos dialectos,
`Runtime.ApplyModuleMigrations`, la precedencia de plantillas, las capas 3–4
de config en el CLI y la parada grácil del outbox.

## Notas de método

Los dos de seguridad (QCD-FW-15 y, en quark, QCD-QK-2) se cerraron con test
que FABRICA la condición insegura y exige que el producto la detecte: un
test del camino feliz no habría valido, porque el hallazgo era precisamente
que el check pasaba en verde sobre algo peligroso.

QCD-FW-13 y QCD-FW-15 no son defectos sueltos sino del CONTRATO DE
EXTENSIÓN de ADR-022, y quedan enmendados en el propio ADR.
