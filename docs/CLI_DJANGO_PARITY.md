# GoFrame CLI vs Django 6.0

Reference date: 2026-04-02.

This document compares the current GoFrame CLI against the official Django 6.0 command list (`django-admin` / `manage.py`) and highlights:

- direct or approximate equivalences
- commands GoFrame has that Django does not
- commands Django has that GoFrame does not yet provide

Sources:

- Django 6.0: [django-admin and manage.py](https://docs.djangoproject.com/en/6.0/ref/django-admin/)
- GoFrame: `internal/cli/root.go` + `goframe help`

## Current GoFrame commands

Canonical commands:

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
- `mailproviders`
- `plugin`
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

Django-style aliases:

- `runserver` -> `serve`
- `startproject` -> `new`
- `makemigrations` -> `migrate create <name>`
- `showmigrations` -> `migrate status`
- `createsuperuser` -> `createuser`
- `dbshell` -> `shell`
- `check` -> `health`

## GoFrame <-> Django mapping

| GoFrame | Django | Type |
| --- | --- | --- |
| `serve` / `runserver` | `runserver` | equivalent |
| `new` / `startproject` | `startproject` | equivalent |
| `startapp` | `startapp` | equivalent (module/app scaffold) |
| `createuser` / `createsuperuser` | `createsuperuser` | equivalent (admin user) |
| `changepassword` | `changepassword` | functional equivalent (admin password rotation) |
| `health` / `check` | `check` | approximate (dependency health + `--deploy` hardening checks) |
| `migrate up/down/steps/reset/refresh` | `migrate` | approximate (similar semantics, different options) |
| `migrate create <name>` / `makemigrations` | `makemigrations` | approximate (GoFrame generates SQL files; Django derives from models) |
| `migrate status` / `showmigrations` | `showmigrations` | approximate |
| `sqlmigrate` | `sqlmigrate` | functional equivalent |
| `sqlflush` | `sqlflush` | functional equivalent |
| `sqlsequencereset` | `sqlsequencereset` | functional equivalent |
| `flush` | `flush` | functional equivalent (with production guardrails) |
| `diffsettings` | `diffsettings` | functional equivalent (effective config vs defaults diff) |
| `createcachetable` | `createcachetable` | functional equivalent (SQL cache table provisioning) |
| `remove_stale_contenttypes` | `remove_stale_contenttypes` | approximate equivalent (SQL-first cleanup of stale content types vs current tables) |
| `ogrinspect` | `ogrinspect` | approximate equivalent (SQL-first geospatial introspection based on geometry/geography types) |
| `makemessages` | `makemessages` | functional equivalent (extract strings into `.po` catalogs) |
| `compilemessages` | `compilemessages` | functional equivalent (compile `.po` into JSON bundles) |
| `collectstatic` | `collectstatic` | functional equivalent (copy static assets to `static_root`) |
| `findstatic` | `findstatic` | functional equivalent (resolve static assets by path/pattern) |
| `optimizemigration` | `optimizemigration` | approximate equivalent (per-file SQL migration optimization) |
| `squashmigrations` | `squashmigrations` | approximate equivalent (range-based SQL-first squash with `.up/.down.sql` output) |
| `sendtestemail` | `sendtestemail` | functional equivalent (test email through `mail_driver`: SMTP, SendGrid, or external plugin) |
| `inspectdb` | `inspectdb` | functional equivalent (DB introspection to Go structs) |
| `dumpdata` | `dumpdata` | functional equivalent (table-based JSON export) |
| `loaddata` | `loaddata` | functional equivalent (table-based JSON import) |
| `shell` / `dbshell` | `dbshell` | functional equivalent (SQL shell) |
| `test` | `test` | functional equivalent (test runner with flags) |
| `testserver` | `testserver` | approximate (fixture + server workflow over configured DB) |
| `clearsessions` | `clearsessions` | functional equivalent (expired or full session cleanup) |
| `seed` | n/a in Django core | GoFrame-specific (operational SQL seeding) |
| `plugin list/doctor/test` | n/a in Django core | GoFrame-specific (capability-based plugin inventory, diagnostics, and smoke tests) |

## What GoFrame has and Django does not (core builtin)

- `routes` (project HTTP route listing).
- `generate` (`model`, `handler`, `migration`, `resource`) in one entry point.
- `mailproviders` (inventory of available and active mail drivers/plugins).
- `plugin list`, `plugin doctor`, `plugin test` (capability-based provider diagnostics).
- PATH-based CLI plugins: `goframe-<name>` (external executable extensions).

## What Django 6.0 has and GoFrame does not yet have

`django-admin` core commands with no direct equivalent today:

- `shell` (Python interpreter; different from `dbshell`)

Django contrib app commands with no direct equivalent today:

- none with current priority

## Scope note

This comparison focuses on built-in framework commands.

- In GoFrame, some commands are intentionally more operational and SQL-first (Bun-first architecture decision).
- In Django, several commands depend on its Python runtime stack and contrib apps (`auth`, `staticfiles`, etc.).
