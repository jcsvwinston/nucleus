---
sidebar_position: 4
title: Storage & background tasks
covers:
  - pkg/storage.New
  - pkg/storage.NewLocalStore
  - pkg/storage.NewS3Store
  - pkg/storage.NewGCSStore
  - pkg/storage.NewAzureStore
  - pkg/storage.Store.Get
  - pkg/storage.Store.Put
  - pkg/storage.Store.Delete
  - pkg/storage.Store.Exists
  - pkg/storage.Store.List
  - pkg/storage.Store.SignedURL
  - pkg/storage.Store.Copy
  - pkg/storage.ErrNotFound
  - pkg/circuit.Breaker
  - pkg/circuit.New
  - pkg/circuit.Config
  - pkg/tasks.Manager
  - pkg/tasks.HandlerFunc
  - pkg/tasks.NewJSONTask
  - pkg/mail.NewSender
  - pkg/mail.Sender
  - pkg/mail.HealthChecker
  - pkg/mail.CircuitBreakerConfig
config_keys:
  - storage.provider
  - storage.s3.bucket
  - storage.s3.region
  - storage.local.path
  - storage.circuit_breaker.enabled
  - storage.circuit_breaker.failure_threshold
  - storage.circuit_breaker.cooldown
  - storage.circuit_breaker.half_open_max_concurrent
  - mail_driver
  - mail_circuit_breaker.enabled
  - mail_circuit_breaker.failure_threshold
  - mail_circuit_breaker.cooldown
  - redis_url
---

# Storage & background tasks

## File storage (`pkg/storage`)

`pkg/storage` is a provider-agnostic file storage abstraction with a
durable interface designed to last through `v1.x`. The same code runs
against:

- the local filesystem,
- AWS S3,
- Google Cloud Storage,
- Azure Blob Storage.

```go
import "github.com/jcsvwinston/nucleus/pkg/storage"

reader, err := a.Storage.Get(ctx, "uploads/avatar.png")
url,    err := a.Storage.SignedURL(ctx, "uploads/avatar.png", 5*time.Minute)
err           = a.Storage.Put(ctx, "uploads/avatar.png", body, storage.Metadata{
    ContentType: "image/png",
})
```

Configure the backend in `nucleus.yml`. The exact key shape is provider-
specific — the snippet below is illustrative; the canonical schema lives
in [`docs/reference/CONFIG_KEY_REGISTRY.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/CONFIG_KEY_REGISTRY.md)
and follows the layout in [`SPEC.md` §3.8](https://github.com/jcsvwinston/nucleus/blob/main/SPEC.md):

```yaml
storage:
  driver: s3              # local | s3 | gcs | azure
  s3:
    bucket: my-bucket
    region: eu-west-1
    prefix: uploads/
```

Per-driver credentials and endpoints are read from environment variables
or platform credential providers — never embedded in the config file.

The admin panel surfaces a file browser against the configured backend.

### Circuit breaker (storage)

`App.New` automatically wraps all remote provider operations
(`Put`, `Get`, `Delete`, `Exists`, `List`, `Copy`, `SignedURL`) with a
`pkg/circuit.Breaker`. The `local` provider and `PublicURL` (pure string
composition) are never wrapped. `storage.ErrNotFound` is not counted as
a failure — a missing object is a normal outcome.

When the breaker is open, wrapped operations return `circuit.ErrOpen`
immediately. The default thresholds are:

```yaml
storage:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    cooldown: 30s
    half_open_max_concurrent: 1
```

Set `enabled: false` to disable, or tune the thresholds for your
workload. Full details: [`docs/guides/STORAGE_GUIDE.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/guides/STORAGE_GUIDE.md#circuit-breaker).

## Background tasks (`pkg/tasks`)

`pkg/tasks` runs background jobs on **Asynq** + Redis. Tasks are
type-safe Go values; the framework handles enqueue, retry, dead-letter
and metrics.

```go
import "github.com/jcsvwinston/nucleus/pkg/tasks"

type SendWelcomeEmail struct {
    UserID int64
}

a.Tasks.Register(tasks.Handler(func(ctx context.Context, t SendWelcomeEmail) error {
    return mail.SendWelcome(ctx, t.UserID)
}))

// Enqueue from a request handler:
err := a.Tasks.Enqueue(ctx, SendWelcomeEmail{UserID: 42})
```

The admin panel exposes the queue inspector — pending, in-flight,
retried, dead-lettered — with one-click requeue.

## Transactional outbox (`pkg/outbox`)

The naïve "enqueue inside a SQL transaction" pattern silently loses
events when the transaction commits but the queue write fails.
`pkg/outbox` solves this with the standard outbox pattern:

```go
err := a.DB.WithTx(ctx, func(tx *db.Tx) error {
    if err := repo.Save(tx, article); err != nil {
        return err
    }
    return outbox.Publish(tx, ArticlePublished{ID: article.ID})
})
```

The outbox table is part of the migration set the framework manages.
A relay process (run inline in development, separately in production)
moves committed events into the task queue.

## Mail (`pkg/mail`)

Two drivers ship out of the box:

| Driver | Use                                                  |
| ------ | ---------------------------------------------------- |
| `noop` | Tests and development — captures payloads in memory. |
| `smtp` | Anything that speaks SMTP.                           |

Vendor-specific HTTP providers (SendGrid, Mailgun, AWS SES, Postmark,
Resend, …) install as `nucleus-plugin-<provider>` binaries on `PATH`
and are discovered via the capability-style external bridge
(`pkg/plugins`). The `mail.send` capability contract is documented
in the [Plugin SDK reference](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/PLUGIN_SDK.md);
a runnable reference skeleton returns with the v0.9.X reference
applications.

### Circuit breaker (mail)

`App.New` automatically wraps `mail.Sender.Send` with a
`pkg/circuit.Breaker`. The `noop` driver and the `Healthy` SMTP HELO
probe (used by `/healthz`) are never wrapped — so health checks can
observe that a mail relay has recovered while `Send` is still
short-circuited.

When the breaker is open, `Send` returns `circuit.ErrOpen`. The default
thresholds are:

```yaml
mail_circuit_breaker:
  enabled: true
  failure_threshold: 5
  cooldown: 30s
  half_open_max_concurrent: 1
```

Set `enabled: false` to disable. Config keys are documented in
[`docs/reference/CONFIG_KEY_REGISTRY.md`](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/CONFIG_KEY_REGISTRY.md).
