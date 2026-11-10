# nucleus/providers/tasks-asynq

Run a Nucleus application's background jobs on Redis, through
[asynq](https://github.com/hibiken/asynq).

This is the import path the asynq jobs provider has **from v2.0.0 on**
(DEP-2026-020). At the major the provider becomes a **separate Go module
inside the framework's own repository**, like the drivers, the exporters and
the other providers (ADR-030/031): it ships on the framework's release train
and under its CI, but it is not in the framework's `go.mod`, so whoever runs
jobs in-process (`jobs_provider: memory`, the default) or on the database
the application already has (`jobs_provider: sql`) does not carry asynq or
a Redis client.

**In 1.x the module forwards.** The provider's code still lives in
`pkg/tasks/providers/asynq`, in the framework's root module, and the
framework links it itself. This package imports that one and re-exports its
whole surface (the types as aliases, the errors as the same values, the
constructors as thin wrappers), so importing it today changes nothing about
what runs; it fixes the import path an application will need at 2.0. A test
in the framework holds the two surfaces equal until the move.

## Install

```bash
nucleus add tasks-asynq
```

or by hand:

```bash
go get github.com/jcsvwinston/nucleus/providers/tasks-asynq
```

Import it for the side effect, the same shape `database/sql` drivers use,
and select it in the configuration:

```go
import _ "github.com/jcsvwinston/nucleus/providers/tasks-asynq"
```

```yaml
# nucleus.yml
jobs_provider: asynq
jobs_redis_url: redis://localhost:6379/0
```

The import registers the provider with `pkg/tasks`
(`tasks.RegisterProvider`); `jobs_provider: asynq` selects it.

Until v2.0.0 the import is **optional**: the framework links the provider on
its own, so an application that selects `asynq` without it boots as before.
From v2.0.0 the import is **required**: an application that selects `asynq`
without it refuses to boot and names this module and the command that
installs it. An application that writes the import now has nothing to
change at the upgrade but the version.

## What it does

- The module jobs declared through `ModuleSpec.Jobs`, and the one-off tasks
  a module enqueues through `Runtime.Tasks()`, are kept in Redis and survive
  a restart of the process.
- With more than one replica, workers share the queue and the scheduler
  runs under leader election over a Redis lock (`jobs_scheduler_lock`, on by
  default), so exactly one replica ticks the cron entries. Setting it to
  `false` opts out, and the boot log warns that every replica fires every
  job.
- `nucleus doctor --check tasks` inspects the queues, servers and workers the
  Redis holds. The CLI links the provider, so the inspection works whether or
  not the application's own binary does.
- The same `jobs.*` metrics every provider records, labelled by provider,
  queue and task type.

## Settings

| Key | Default | Meaning |
|---|---|---|
| `jobs_provider` | `memory` | `asynq` selects this provider. |
| `jobs_redis_url` | `""` | The Redis the queue lives in. Required with `asynq`; the configuration is refused without it. |
| `jobs_concurrency` | `0` | Concurrent workers; `0` is asynq's default of 10. |
| `jobs_scheduler_lock` | `true` | Leader election for the scheduler (`SET NX` + TTL). |

`jobs_queues`, `jobs_table` and `jobs_retention` belong to the sql provider
and are ignored here; asynq takes its queue weights from its own
configuration (`tasks.Config.Queues`) when the manager is built directly.

## Using the provider directly

The package has the symbols the in-tree one has: `NewManager`,
`NewScheduler`, `NewLeaderScheduler`, `NewInspector`, `InspectRuntime` and
`OperateQueue`, the `Err*` sentinels and the scheduler lock defaults. In 1.x
they are the same types and values under either path, so code can move to
this path now, one import at a time:

```go
import (
	"github.com/jcsvwinston/nucleus/pkg/tasks"
	asynqprovider "github.com/jcsvwinston/nucleus/providers/tasks-asynq"
)

mgr, err := asynqprovider.NewManager(tasks.Config{
	RedisURL:    "redis://localhost:6379/0",
	Concurrency: 8,
}, nil)
```

At v2.0.0 the old path, `github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq`,
is gone; `docs/migration_assistants/MA-2026-020-asynq-provider-module.md`
has the one-line rewrite.

## Tests

This module's own tests need no Redis: they check that the import registers
the provider, that the forwarded types and errors are the in-tree ones, and
that the constructors refuse the way the in-tree ones do. The provider's
behaviour is tested where its code lives, `pkg/tasks/providers/asynq`
(against miniredis, and against a real server in the `jobs-redis` CI lane);
at 2.0 those tests move with the code.
