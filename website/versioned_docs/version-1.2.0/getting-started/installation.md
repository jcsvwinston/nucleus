---
sidebar_position: 1
title: Installation
covers: []
config_keys: []
---

# Installation

## Requirements

- Go **1.26** or newer (matches the `go 1.26.4` directive in `go.mod`)
- One of: SQLite, PostgreSQL, MySQL
- Optional: Redis (for the Redis session store and the background-task
  runtime)

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

`nucleus doctor` runs a self-check of the local environment: Go version,
required system tools, optional dependencies, and a write-test against the
working directory.

## What gets installed

The `nucleus` binary is the only artifact. There is no daemon, no agent,
no global configuration file. Each project ships its own `nucleus.yml`
and reads it from the project root by default.

## Build-tagged enterprise drivers

SQLite, PostgreSQL and MySQL are included by default. MSSQL and Oracle are
opt-in via Go build tags so that the default binary stays small and free
of additional CGO requirements:

```bash
go install -tags mssql  github.com/jcsvwinston/nucleus/cmd/nucleus@latest
go install -tags oracle github.com/jcsvwinston/nucleus/cmd/nucleus@latest
```

See [`pkg/db`](https://github.com/jcsvwinston/nucleus/tree/main/pkg/db)
for the full driver list.

## Updating

Re-running `go install …@latest` overwrites the binary in place. The CLI
follows semantic versioning; on the stable `v1.x` line, minor and patch
upgrades preserve the frozen surfaces (removals require a major and a
deprecation record). The
[`CHANGELOG`](https://github.com/jcsvwinston/nucleus/blob/main/CHANGELOG.md)
lists what each release adds.
