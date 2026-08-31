# ADR-031: Database drivers and telemetry exporters ship as their own modules

- Status: Accepted
- Date: 2026-08-31
- Supersedes in part: the `mssql` / `oracle` build tags introduced with the
  enterprise driver support
- Follows: [ADR-030](ADR-030-cloud-backends-as-modules.md), which moved the
  cloud storage and secrets backends out on the same pattern

## Context

ADR-030 took the four cloud SDKs out of the framework and the hello-world went
from 75.6 MB to 37 MB. The target for the arc was under 30 MB and under 150
modules, and measurement said the remainder sat in two places:

| Block | Packages | Entered through |
| --- | --- | --- |
| OTLP exporter | 82 (66 of them gRPC) | `pkg/observe` |
| Prometheus exporter | 55 (37 of them protobuf) | `pkg/observe` |
| Database drivers | 63 | `pkg/db` |

Two of those numbers corrected an assumption worth writing down, because both
were plausible and both were wrong.

**gRPC does not come from a gRPC exporter.** `pkg/observe` uses the HTTP
flavour of OTLP — `otlptracehttp`, `otlpmetrichttp` — so gRPC should not be in
the graph at all. It is, because the exporter's internal configuration package
imports gRPC regardless of transport. Choosing HTTP buys nothing here.

**protobuf does not come from OTLP.** It comes from Prometheus's
`client_golang`. Removing the OTLP exporter alone would have left 37 protobuf
packages behind and made the result look like a failed change.

## Decision

Each database driver and each telemetry exporter ships as its own module.
The framework keeps the OpenTelemetry **SDK** — instrumentation all over the
code calls into it, and it is small — and keeps none of the exporters.

```
drivers/postgres   drivers/mysql   drivers/sqlite   drivers/mssql   drivers/oracle
exporters/otlp     exporters/prometheus
```

An application links what it uses with a blank import, the way `database/sql`
drivers have always been wired:

```go
import (
    _ "github.com/jcsvwinston/nucleus/drivers/postgres"
    _ "github.com/jcsvwinston/nucleus/exporters/otlp"
)
```

### The configuration does not change

This is the part that decides whether the change is an improvement or a tax.
`database_url: postgres://…`, `otlp_endpoint`, `metrics_path` all keep working
exactly as written. Nothing in anyone's YAML moves. The only new thing is an
import, and its absence is reported at startup with the line to add.

### A driver module registers two things, not one

The obvious half is the `database/sql` driver. The half that is easy to omit
and expensive to get wrong is the **error classifier**: `db.IsUniqueViolation`
recognises a MySQL duplicate-key error by matching MySQL's error TYPE, which
requires importing MySQL.

Without the classifier, that function does not fail — it answers `false`, and
the branch that turns "that email is taken" into a 409 becomes dead code. A
wrong answer is worse than a missing one, so `pkg/db/driver` treats a driver
registered without a classifier as a bug in the module.

PostgreSQL is the exception, and deliberately so: every PostgreSQL driver
exposes its SQLSTATE through a `SQLState() string` method, so `pkg/db`
classifies it without naming a driver type — and therefore covers `lib/pq` for
callers who bring their own. `drivers/postgres` registers no classifier, and a
test pins that so nobody "fixes" the apparent omission.

The properties that make consulting classifiers in turn safe are checked by
`pkg/db/driver/drivertest`, a conformance kit in the shape of `backendtest`
(ADR-027). The sharpest of them: a classifier must answer only for its own
driver's errors, because one that claims a foreign error answers for another
engine.

### The build tags are gone

`-tags mssql` and `-tags oracle` are replaced by the modules. A build tag is
invisible — nothing in the source says it exists — so a build that forgot it
failed at RUN time with `unknown driver`. An import is in the file, and its
absence fails while compiling or, for a driver named only in configuration, at
startup with a message that names the module.

### Prometheus gets a softer landing than the rest

Every other optional piece here is one an application had to ask for. The
Prometheus exporter is not: it is enabled by `metrics_path`, and
`metrics_path` has a default. Moving it out with the same strictness would
stop every existing application from booting on upgrade — for a module it
never chose.

So `pkg/app` distinguishes the two cases. If the operator wrote
`metrics_path` themselves, a missing module is a broken deployment and startup
fails with the guided error. If the value is the untouched default, startup
warns — naming the fix — and carries on without metrics.

The warning is the honest part of this: an endpoint going quiet is noticed at
three in the morning, and this is the cheapest place to say so out loud. It is
still a behaviour change for anyone scraping the default path, and the release
notes carry it.

## Consequences

Measured on the same hello-world (`pkg/app` linked, nothing else):

| | Binary | Packages | Modules |
| --- | --- | --- | --- |
| Before ADR-030 | 75.6 MB | 1008 | 346 |
| After ADR-030 | 37 MB | 576 | 176 |
| Drivers out | 26 MB | 519 | 140 |
| Exporters out as well | **19 MB** | **349** | **87** |

The arc's targets were under 30 MB and under 150 modules. Both are met with
the drivers alone; the exporters take it further.

### What this costs

- **An import to add.** The guided error names it, `nucleus add <name>` writes
  it, and the project scaffold already contains the SQLite one — but a
  developer switching from SQLite to PostgreSQL now edits two things instead
  of one.
- **A source break in a stable package**, without a deprecation window, in a
  minor. The same reasoning as ADR-030 and ADR-024: the compiler names what is
  missing and the error carries the fix, which a deprecation window would only
  delay.
- **Applications scraping the default `/metrics`** lose it until they add the
  exporter module. This is the one change here that is silent to the compiler,
  which is why it warns at startup.

### What this does not close

The graph is no longer dominated by any single block. What remains is the
framework: `koanf` and its parsers, `casbin`, `scs`, `validator`, `go-redis`.
None is a candidate for extraction on the ADR-030 pattern — they are used on
the path every application takes, not by a backend a deployment chooses.

`quark`'s own graph is a separate front with the same shape: its library
reaches into driver error types in `db_errors.go` and into pgx in
`pg_listener.go`. It is addressed on its own terms, since it reduces a
standalone quark binary rather than this hello-world.
