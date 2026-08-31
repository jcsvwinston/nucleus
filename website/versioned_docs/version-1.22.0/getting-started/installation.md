---
sidebar_position: 1
title: Installation
covers: []
config_keys: []
---

# Installation

Installing Nucleus means installing one binary: the `nucleus` CLI. This page
covers the requirements, the install command, and the two optional database
drivers that need a build tag.

## Requirements

- Go **1.26** or newer (matches the `go` directive in `go.mod`)
- One of: SQLite, PostgreSQL, MySQL
- Optional: Redis, for the Redis session store and the background-task
  runtime

Budget time for the first build. The framework's dependency graph is large
(~350 modules, on the order of 3 GB of module cache), so a cold `go build`
takes minutes. Warm builds are seconds.

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

## Build-tagged enterprise drivers

SQLite, PostgreSQL and MySQL are included by default. MSSQL and Oracle are
opt-in via Go build tags, which keeps the default binary small and free of
extra CGO requirements:

```bash
go install -tags mssql  github.com/jcsvwinston/nucleus/cmd/nucleus@latest
go install -tags oracle github.com/jcsvwinston/nucleus/cmd/nucleus@latest
```

See [`pkg/db`](https://github.com/jcsvwinston/nucleus/tree/main/pkg/db)
for the full driver list.

## Updating

Re-running `go install …@latest` overwrites the binary in place.

The CLI follows semantic versioning. On the stable `v1.x` line, minor and
patch upgrades never remove a frozen surface — removals need a new major
version and a deprecation record first. The
[`CHANGELOG`](https://github.com/jcsvwinston/nucleus/blob/main/CHANGELOG.md)
lists what each release adds.
