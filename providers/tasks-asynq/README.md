# nucleus/providers/tasks-asynq

Run a Nucleus application's background jobs on Redis, through
[asynq](https://github.com/hibiken/asynq).

It is a **separate Go module inside the framework's own repository**: it
ships on the framework's release train, in the framework's documentation
and under the framework's CI, but it is not in the framework's `go.mod`.
Whoever runs jobs in-process (`jobs_provider: memory`, the default) or on the
database the application already has (`jobs_provider: sql`) does not carry
asynq or a Redis client (ADR-030/031, DEP-2026-020).

## Install

```bash
nucleus add tasks-asynq
```

or by hand:

```bash
go get github.com/jcsvwinston/nucleus/providers/tasks-asynq
```

Import it for the side effect — the same shape `database/sql` drivers use —
and then select it in the configuration:

```go
import _ "github.com/jcsvwinston/nucleus/providers/tasks-asynq"
```

```yaml
# nucleus.yml
jobs_provider: asynq
jobs_redis_url: redis://localhost:6379/0
```

The import registers the provider with `pkg/tasks`
(`tasks.RegisterProvider`); `jobs_provider: asynq` selects it. An
application that selects it without the import refuses to boot and names
this module and the command that installs it.

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
  Redis holds: the CLI links this module, so the inspection works whether or
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

The package keeps the symbols it had inside the framework: `NewManager`,
`NewScheduler`, `NewLeaderScheduler`, `NewInspector`, `InspectRuntime` and
`OperateQueue`. Code that constructed a manager from the old import path
changes the path and nothing else:

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

## Tests

The suite runs against miniredis, started out of process by the framework's
`internal/testredis` (so this module's `go.mod` does not carry it either),
and needs a checkout of the repository. `TestProvider_AgainstRealRedis` runs
only with `NUCLEUS_JOBS_REDIS_URL` set, which the `jobs-redis` CI lane does
against a real server.
