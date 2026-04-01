# GoFrame CLI vs Django 6.0

Fecha de contraste: 2026-04-01.

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
- `inspectdb`
- `dumpdata`
- `loaddata`
- `new`
- `startapp`
- `createuser`
- `seed`
- `shell`
- `generate`
- `test`
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
| `health` / `check` | `check` | aproximado (salud dependencias vs system checks Django) |
| `migrate up/down/steps/reset/refresh` | `migrate` | aproximado (semantica similar, opciones distintas) |
| `migrate create <name>` / `makemigrations` | `makemigrations` | aproximado (GoFrame genera archivo SQL; Django deriva desde modelos) |
| `migrate status` / `showmigrations` | `showmigrations` | aproximado |
| `sqlmigrate` | `sqlmigrate` | equivalente funcional |
| `sqlflush` | `sqlflush` | equivalente funcional |
| `sqlsequencereset` | `sqlsequencereset` | equivalente funcional |
| `flush` | `flush` | equivalente funcional (con guardrails en produccion) |
| `inspectdb` | `inspectdb` | equivalente funcional (introspeccion DB a structs Go) |
| `dumpdata` | `dumpdata` | equivalente funcional (export JSON por tablas) |
| `loaddata` | `loaddata` | equivalente funcional (import JSON por tablas) |
| `shell` / `dbshell` | `dbshell` | equivalente funcional (shell SQL) |
| `test` | `test` | equivalente funcional (runner de test con flags) |
| `seed` | n/a builtin Django | especifico GoFrame (SQL seeds operativos) |

## Lo que GoFrame tiene y Django no (core builtin)

- `routes` (listado de rutas HTTP del proyecto).
- `generate` (`model`, `handler`, `migration`, `resource`) en una sola entrada.
- plugins CLI por PATH: `goframe-<name>` (extensiones externas ejecutables).

## Lo que Django 6.0 tiene y GoFrame aun no

Comandos core de `django-admin` sin equivalente directo hoy:

- `compilemessages`
- `createcachetable`
- `diffsettings`
- `makemessages`
- `optimizemigration`
- `sendtestemail`
- `shell` (interprete Python; distinto a `dbshell`)
- `squashmigrations`
- `testserver`

Comandos de apps contrib de Django sin equivalente directo hoy:

- `changepassword`
- `remove_stale_contenttypes`
- `clearsessions`
- `collectstatic`
- `findstatic`
- `ogrinspect`

## Nota de alcance

La comparativa esta centrada en comandos builtin de framework.

- En GoFrame algunos comandos son mas operativos SQL-first (por decision de arquitectura Bun-first).
- En Django varios comandos dependen de su stack Python/runtime y de apps contrib (`auth`, `staticfiles`, etc.).
