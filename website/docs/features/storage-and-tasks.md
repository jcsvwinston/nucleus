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
  - pkg/storage.PutOptions
  - pkg/storage.URLConfig
  - pkg/storage.ObjectInfo
  - pkg/circuit.Breaker
  - pkg/circuit.New
  - pkg/circuit.Config
  - pkg/tasks.Manager
  - pkg/tasks.HandlerFunc
  - pkg/nucleus.JobRegistry.Register
  - pkg/nucleus.JobSpec
  - pkg/nucleus.WebhookRegistry.Register
  - pkg/nucleus.WebhookSpec
  - pkg/nucleus.SignWebhookBody
  - pkg/nucleus.WebhookSignatureHeader
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
  - jobs_provider
  - jobs_redis_url
  - jobs_concurrency
  - webhooks_prefix
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

// Get returns a ReadCloser and object metadata; always close the reader.
reader, info, err := a.Storage.Get(ctx, "uploads/avatar.png")

// SignedURL requires an opts argument (use zero value for defaults).
url, err := a.Storage.SignedURL(ctx, "uploads/avatar.png", 5*time.Minute, storage.URLConfig{})

// Put returns the stored ObjectInfo and an error.
info, err = a.Storage.Put(ctx, "uploads/avatar.png", body, storage.PutOptions{
    ContentType: "image/png",
})
_ = reader
_ = info
```

Configure the backend in `nucleus.yml`. The exact key shape is provider-
specific — the snippet below is illustrative; the canonical schema is the
[Configuration reference](../reference/configuration.md):

```yaml
# illustrative — see the Configuration reference for the full storage.* schema
storage:
  provider: s3            # local | s3 | gcs | azure
  s3:
    bucket: my-bucket
    region: eu-west-1
```

Per-driver credentials and endpoints are read from environment variables
or platform credential providers — never embedded in the config file.

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

`pkg/tasks` runs background jobs on **Asynq** + Redis. Payloads are
encoded as JSON and keyed by a task-type string; the framework handles
enqueue, retry, dead-letter and metrics.

`tasks.Manager` is an interface constructed by the application's task
wiring — it is **not** exposed as a field on `App`. Hold the `Manager`
your wiring builds and use it directly:

```go
import (
    "context"

    "github.com/jcsvwinston/nucleus/pkg/tasks"
)

const TypeSendWelcomeEmail = "email:welcome"

type SendWelcomeEmail struct {
    UserID int64
}

// mgr is a tasks.Manager held by your task wiring.
// Register a handler for the task type. tasks.HandlerFunc is
// func(ctx context.Context, task tasks.Task) error.
mgr.HandleFunc(TypeSendWelcomeEmail, tasks.HandlerFunc(
    func(ctx context.Context, task tasks.Task) error {
        var payload SendWelcomeEmail
        if err := tasks.DecodeJSONPayload(task, &payload); err != nil {
            return err
        }
        return sendWelcome(ctx, payload.UserID)
    },
))

// Enqueue from a request handler (payload is JSON-encoded for you):
id, err := mgr.EnqueueJSON(TypeSendWelcomeEmail, SendWelcomeEmail{UserID: 42})
```

## Module jobs and webhooks

Modules declare recurring background jobs and inbound webhook receivers
directly on their `Module[C]` definition; the framework schedules the
jobs on `pkg/tasks` and mounts the webhook routes at boot.

```go
nucleus.Module[BillingConfig]{
    Name: "billing",
    Jobs: func(j nucleus.JobRegistry, cfg BillingConfig) {
        _ = j.Register("reconcile", nucleus.JobSpec{
            Every:     15 * time.Minute, // or Cron: "0 3 * * *"
            Timeout:   5 * time.Minute,
            Singleton: true, // skip a tick while the previous run is live
            Handler: func(ctx context.Context) error {
                return reconcileInvoices(ctx)
            },
        })
    },
    Webhooks: func(w nucleus.WebhookRegistry, cfg BillingConfig) {
        _ = w.Register("/stripe", nucleus.WebhookSpec{
            Secret: cfg.StripeWebhookSecret,
            Handler: func(rw http.ResponseWriter, r *http.Request) {
                // Body is verified against X-Nucleus-Signature before
                // this handler runs; read it as usual.
            },
        })
    },
}
```

**Jobs.** Each registration needs exactly one schedule: `Every` (a fixed
interval) or `Cron` (standard 5-field expression or a descriptor such as
`@hourly`; validated at boot, identical semantics on every provider).
The `jobs_provider` config key selects the runtime: `memory` (default —
in-process, pending jobs are lost on restart) or `asynq` (Redis-backed,
durable; set `jobs_redis_url`). `jobs_concurrency` caps parallel
workers. A broken registration — duplicate name, invalid cron, missing
handler — fails boot instead of silently never running.

**Webhooks.** Each registration mounts a real route at
`<webhooks_prefix>/<module-name><path>` (default prefix `/webhooks`).
With a `Secret` set, requests must carry an HMAC-SHA256 signature of the
raw body in the `X-Nucleus-Signature` header (`sha256=<hex>` — the
`nucleus.SignWebhookBody` helper produces it for senders and tests);
anything unsigned or mis-signed is rejected with 401 before your handler
runs. Method allow-list (default POST-only → 405) and a body cap
(default 1 MiB → 413) are enforced first. When `csrf_enabled` is on, the
webhook prefix is exempted automatically — webhooks authenticate by
signature, not by CSRF token. A webhook registered *without* a `Secret`
is mounted but logged as a boot WARN: its handler must authenticate
callers itself.

## Transactional outbox (`pkg/outbox`)

The naïve "enqueue inside a SQL transaction" pattern silently loses
events when the transaction commits but the queue write fails.
`pkg/outbox` solves this with the standard outbox pattern:

```go
import (
    "database/sql"

    "github.com/jcsvwinston/nucleus/pkg/outbox"
)

// App.DB.Tx runs fn inside a transaction (tx is a *sql.Tx).
// App.Outbox is a *outbox.ManagedOutbox; EnqueueTx writes the event row
// in the SAME transaction, so the event is durable iff the commit lands.
err := a.DB.Tx(ctx, func(tx *sql.Tx) error {
    if err := repo.Save(tx, article); err != nil {
        return err
    }
    _, err := a.Outbox.EnqueueTx(ctx, tx, outbox.Entry{
        Topic:   "article.published",
        Payload: ArticlePublished{ID: article.ID},
    })
    return err
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

Set `enabled: false` to disable. Config keys are documented in the
[Configuration reference](../reference/configuration.md).
