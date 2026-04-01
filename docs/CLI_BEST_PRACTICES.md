# CLI Completa: Buenas Practicas (MVC + API)

Fecha de contraste: 2026-04-01.

Este documento resume practicas observadas en CLIs de frameworks consolidados y su traduccion a GoFrame.

## Referentes contrastados

- Django `django-admin` / `manage.py`
- Rails `bin/rails`
- Laravel Artisan
- Phoenix Mix (`mix phx.*`, `mix ecto.*`)

Fuentes oficiales al final del documento.

## Patrones comunes en frameworks grandes

1. Descubribilidad y ayuda consistente
- Un comando raiz con listado corto y claro de comandos.
- Ayuda por subcomando (`help <cmd>` o `<cmd> --help`).
- Nombres de comandos estables y predecibles.

2. Comandos troncales de operacion
- Servidor local (`serve`/`server`/`runserver`).
- Migraciones con ciclo completo (`up`, `down/rollback`, `status`, `steps`).
- Listado de rutas (`routes`, `route:list`, `phx.routes`).
- Shell/console interactiva para inspeccion y debugging.
- Health/checks para validacion de entorno.

3. Generacion de codigo (scaffolding)
- Generadores para modelos, handlers/controllers y migraciones.
- Estructura de salida predecible y facil de editar.
- Prevencion de sobreescritura accidental (modo `--force` explicito).

4. Seguridad operativa y automatizacion
- Modo no interactivo para CI/CD (`--no-input`).
- Banderas de seguridad para acciones destructivas en produccion (`--force` con confirmacion previa en otros frameworks).
- Exit codes consistentes (0 ok, !=0 error) para pipelines.

5. Semantica de migraciones
- Orden por timestamp/version en nombre de archivo.
- Tabla de tracking de migraciones aplicadas.
- Soporte de rollback parcial (`step`) y estado (`status`).

## Estado actual de GoFrame CLI

Implementado en `cmd/goframe` + `internal/cli`:

- `serve`:
  - arranque del servidor con `--config`, `--host`, `--port`.
- `migrate`:
  - `up`, `down`, `steps`, `status`, `create`, `reset`, `refresh`.
  - guardrails en produccion con `--force` / `--yes` para acciones destructivas.
- `sqlmigrate` / `sqlflush` / `sqlsequencereset` / `flush`:
  - inspeccion SQL de migraciones y mantenimiento de datos/secuencias.
  - `flush` con guardrails de produccion (`--force` / `--yes`).
- `inspectdb`:
  - introspeccion de esquema SQL y generacion de structs Go con tags `db`.
- `dumpdata` / `loaddata`:
  - export/import JSON de fixtures por tabla.
  - `loaddata --truncate` con guardrails de produccion (`--force` / `--yes`).
- `routes`:
  - listado de rutas, filtro por prefijo y salida JSON.
- `health`:
  - check de DB con timeout y salida texto/JSON.
- `generate`:
  - scaffolds de `model`, `handler`, `migration` y `resource` (CRUD base).
- `new`:
  - bootstrap de proyecto completo MVC + API + Admin con estructura recomendada.
- `startapp`:
  - scaffold de modulo en proyecto existente (`internal/models/controllers/tasks/web/templates`).
- `seed`:
  - ejecucion de ficheros SQL ordenados.
  - guardrails en produccion con `--force` / `--yes`.
- `createuser`:
  - creacion/actualizacion de usuario admin, modo no interactivo con `--no-input`.
- `shell`:
  - modo interactivo y modo no interactivo (`-c` o stdin).
  - modo `--sandbox` para limitar a sentencias SQL de solo lectura.
- `test`:
  - wrapper de `go test` con defaults de proyecto y flags (`--run`, `--race`, `--cover`, `--timeout`, `--dry-run`).
- extensibilidad:
  - comandos externos `goframe-<nombre>` en `PATH` (plugin-like).
- aliases estilo Django:
  - `runserver`, `startproject`, `makemigrations`, `showmigrations`, `createsuperuser`, `dbshell`, `check`.

## Gaps para hardening posterior

1. Experiencia avanzada de shell
- Historial persistente y multilinea.

2. Cobertura de tests CLI por matriz de motores SQL
- SQLite ya cubierto; ampliar con Postgres/MySQL en CI.

## Criterio practico de salida (CLI v1)

- [x] Comandos troncales disponibles.
- [x] Ayuda raiz y por subcomando.
- [x] Exit codes consistentes para automatizacion.
- [x] Migraciones con estado y rollback.
- [x] Generacion de scaffolds basicos.
- [x] Guardrails de produccion unificados (`--force`/`--yes` + confirmacion interactiva).
- [x] Plugins/comandos custom por proyecto (via `goframe-<nombre>`).

## Fuentes oficiales

- Django: [django-admin and manage.py](https://docs.djangoproject.com/en/6.0/ref/django-admin/)
- Django: [Custom management commands](https://docs.djangoproject.com/en/6.0/howto/custom-management-commands/)
- Rails: [The Rails Command Line](https://guides.rubyonrails.org/command_line.html)
- Rails: [Active Record Migrations](https://guides.rubyonrails.org/active_record_migrations.html)
- Laravel: [Artisan Console](https://laravel.com/docs/12.x/artisan)
- Laravel: [Database Migrations](https://laravel.com/docs/12.x/migrations)
- Laravel: [Routing (`route:list`)](https://laravel.com/docs/12.x/routing)
- Laravel: [Database Seeding](https://laravel.com/docs/12.x/seeding)
- Phoenix: [Phoenix Mix Tasks (`mix phx.routes`, generators)](https://hexdocs.pm/phoenix/1.4.17/phoenix_mix_tasks.html)
- Phoenix/Ecto: [Ecto in Phoenix (`mix ecto.migrate`, `mix ecto.rollback`)](https://hexdocs.pm/phoenix/ecto.html)

Comparativa detallada GoFrame vs Django 6.0:

- `docs/CLI_DJANGO_PARITY.md`
