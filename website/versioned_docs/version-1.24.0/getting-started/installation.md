---
sidebar_position: 1
title: Installation
covers: []
config_keys: []
---

# Installation

Installing Nucleus means installing one binary: the `nucleus` CLI. This page
covers the requirements, the install command, and how an application picks
its database driver — each driver is a module the application imports.

## Requirements

- Go **1.26** or newer (matches the `go` directive in `go.mod`)
- One database: SQLite, PostgreSQL, MySQL, SQL Server or Oracle — the
  driver is a module your application imports (see below)
- Optional: Redis, for the Redis session store and the background-task
  runtime

Budget a few minutes for the first build. A freshly scaffolded application
with the SQLite driver resolves 138 modules and links to a 60 MB binary
(45 MB stripped); the framework without any driver is 87 modules and 31 MB.
A cold `go build` downloads that graph once; warm builds are seconds.

## Install the CLI

```bash
go install github.com/jcsvwinston/nucleus/cmd/nucleus@latest
```

This places the `nucleus` binary in `$GOBIN` (or `$GOPATH/bin`). Make sure
that directory is on your `PATH`.

Verify the install:

```bash
nucleus --version
nucleus doctor
```

`nucleus doctor` reads the project's `nucleus.yml` and checks the
framework subsystems it configures — the jobs provider, outbox, storage,
observability exporters, tenancy, RBAC, security posture and the
authentication chain. Optional subsystems you have not enabled are
reported as *not configured* (`-`) and do not count against the verdict,
so a freshly scaffolded project reports `Overall Status: HEALTHY`.
Warnings are reserved for half-configured subsystems, errors for broken
ones.

Run from a directory without a `nucleus.yml`, it checks the built-in
defaults — useful to confirm the binary works, but the interesting run is
inside a project.

## What gets installed

The `nucleus` binary is the only artifact. There is no daemon, no agent,
no global configuration file. Each project ships its own `nucleus.yml`
and reads it from the project root by default.

## Database drivers are modules

The framework links no database driver. Each engine ships as its own Go
module — `drivers/sqlite`, `drivers/postgres`, `drivers/mysql`,
`drivers/mssql`, `drivers/oracle` — and an application imports the one it
uses for its side effect, the way `database/sql` drivers have always been
wired. `nucleus new` already writes the SQLite import into `main.go`;
switching engines is one command, which runs `go get` and adds the blank
import:

```bash
nucleus add postgres     # or mysql, sqlite, sqlserver, oracle
```

Set a `database_url` for an engine whose module is not imported and the
application refuses to start, printing the `go get` and `import _` lines to
add. Every driver is pure Go, so no C toolchain is needed for any of them.

The `nucleus` CLI itself links all five engines: it is a tool you install
once and point at whatever database is in front of it, and `nucleus migrate`
has to work there without a rebuild.

The same pattern covers the other optional pieces: the telemetry exporters
(`nucleus add otlp`, `nucleus add prometheus`), the cloud storage backends
(`nucleus add s3|gcs|azure`) and the LDAP authentication backend
(`nucleus add ldap`). `nucleus add --help` lists them all.

## Updating

Re-running `go install …@latest` overwrites the binary in place.

The CLI follows semantic versioning. On the stable `v1.x` line, minor and
patch upgrades never remove a frozen surface — removals need a new major
version and a deprecation record first. The
[`CHANGELOG`](https://github.com/jcsvwinston/nucleus/blob/main/CHANGELOG.md)
lists what each release adds.
