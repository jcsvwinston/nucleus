# GoFrame CLI vs Django 6.0

Fecha de contraste: 2026-04-02.

Este documento compara la CLI actual de GoFrame con la lista oficial de comandos de Django 6.0 (`django-admin` / `manage.py`) y marca:

- equivalencias directas o aproximadas
- comandos que GoFrame tiene y Django no
- comandos que Django tiene y GoFrame aun no tiene

Fuentes:

- Django 6.0: [django-admin and manage.py](https://docs.djangoproject.com/en/6.0/ref/django-admin/)
- GoFrame: `internal/cli/root.go` + `goframe help`

## Comandos actuales de GoFrame

Comandos canonicos:

- `serve`
- `migrate`
- `sqlmigrate`
- `sqlflush`
- `sqlsequencereset`
- `flush`
- `diffsettings`
- `createcachetable`
- `makemessages`
- `compilemessages`
- `collectstatic`
- `optimizemigration`
- `remove_stale_contenttypes`
- `ogrinspect`
- `squashmigrations`
- `sendtestemail`
- `findstatic`
- `inspectdb`
- `dumpdata`
- `loaddata`
- `new`
- `startapp`
- `createuser`
- `changepassword`
- `clearsessions`
- `seed`
- `shell`
- `generate`
- `test`
- `testserver`
- `routes`
- `health`

Aliases estilo Django:

- `runserver` -> `serve`
- `startproject` -> `new`
- `makemigrations` -> `migrate create <name>`
- `showmigrations` -> `migrate status`
- `createsuperuser` -> `createuser`
- `dbshell` -> `shell`
- `check` -> `health`

## Equivalencias GoFrame <-> Django

| GoFrame | Django | Tipo |
| --- | --- | --- |
| `serve` / `runserver` | `runserver` | equivalente |
| `new` / `startproject` | `startproject` | equivalente |
| `startapp` | `startapp` | equivalente (scaffold de modulo/app) |
| `createuser` / `createsuperuser` | `createsuperuser` | equivalente (admin user) |
| `changepassword` | `changepassword` | equivalente funcional (rotacion de password de admin user) |
| `health` / `check` | `check` | aproximado (salud dependencias + `--deploy` para hardening) |
| `migrate up/down/steps/reset/refresh` | `migrate` | aproximado (semantica similar, opciones distintas) |
| `migrate create <name>` / `makemigrations` | `makemigrations` | aproximado (GoFrame genera archivo SQL; Django deriva desde modelos) |
| `migrate status` / `showmigrations` | `showmigrations` | aproximado |
| `sqlmigrate` | `sqlmigrate` | equivalente funcional |
| `sqlflush` | `sqlflush` | equivalente funcional |
| `sqlsequencereset` | `sqlsequencereset` | equivalente funcional |
| `flush` | `flush` | equivalente funcional (con guardrails en produccion) |
| `diffsettings` | `diffsettings` | equivalente funcional (diff de config vs defaults) |
| `createcachetable` | `createcachetable` | equivalente funcional (provision de tabla SQL de cache) |
| `remove_stale_contenttypes` | `remove_stale_contenttypes` | equivalente aproximado (limpieza SQL-first de content types huerfanos respecto a tablas vigentes) |
| `ogrinspect` | `ogrinspect` | equivalente aproximado (inspeccion de tablas geoespaciales SQL-first basada en tipos geometry/geography) |
| `makemessages` | `makemessages` | equivalente funcional (extraccion de cadenas a catalogos `.po`) |
| `compilemessages` | `compilemessages` | equivalente funcional (compilacion de `.po` a bundles JSON) |
| `collectstatic` | `collectstatic` | equivalente funcional (copiado de assets estaticos a `static_root`) |
| `findstatic` | `findstatic` | equivalente funcional (resolucion de assets estaticos por ruta/patron) |
| `optimizemigration` | `optimizemigration` | equivalente aproximado (optimizacion SQL por archivo de migracion) |
| `squashmigrations` | `squashmigrations` | equivalente aproximado (squash SQL-first por rango con salida `.up/.down.sql`) |
| `sendtestemail` | `sendtestemail` | equivalente funcional (envio de email de prueba via `mail_driver`: SMTP, SendGrid o plugin externo) |
| `inspectdb` | `inspectdb` | equivalente funcional (introspeccion DB a structs Go) |
| `dumpdata` | `dumpdata` | equivalente funcional (export JSON por tablas) |
| `loaddata` | `loaddata` | equivalente funcional (import JSON por tablas) |
| `shell` / `dbshell` | `dbshell` | equivalente funcional (shell SQL) |
| `test` | `test` | equivalente funcional (runner de test con flags) |
| `testserver` | `testserver` | aproximado (pipeline fixture + server sobre DB configurada) |
| `clearsessions` | `clearsessions` | equivalente funcional (limpieza de sesiones expiradas o completas) |
| `seed` | n/a builtin Django | especifico GoFrame (SQL seeds operativos) |

## Lo que GoFrame tiene y Django no (core builtin)

- `routes` (listado de rutas HTTP del proyecto).
- `generate` (`model`, `handler`, `migration`, `resource`) en una sola entrada.
- plugins CLI por PATH: `goframe-<name>` (extensiones externas ejecutables).

## Lo que Django 6.0 tiene y GoFrame aun no

Comandos core de `django-admin` sin equivalente directo hoy:

- `shell` (interprete Python; distinto a `dbshell`)

Comandos de apps contrib de Django sin equivalente directo hoy:

- ninguno de prioridad actual

## Nota de alcance

La comparativa esta centrada en comandos builtin de framework.

- En GoFrame algunos comandos son mas operativos SQL-first (por decision de arquitectura Bun-first).
- En Django varios comandos dependen de su stack Python/runtime y de apps contrib (`auth`, `staticfiles`, etc.).
