---
sidebar_position: 4
title: Storage & background tasks
covers:
  - pkg/storage.New
  - pkg/storage.NewLocalStore
  - pkg/storage.RegisterProvider
  - pkg/storage/provider.NormalizeKey
  - pkg/storage/provider.ValidateKey
  - pkg/storage/provider.Store.Get
  - pkg/storage/provider.Store.Put
  - pkg/storage/provider.Store.Delete
  - pkg/storage/provider.Store.Exists
  - pkg/storage/provider.Store.List
  - pkg/storage/provider.Store.SignedURL
  - pkg/storage/provider.Store.Copy
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
  - pkg/nucleus.SignWebhookBodyWithTimestamp
  - pkg/nucleus.WebhookSignatureHeader
  - pkg/nucleus.WebhookTimestampHeader
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
  - outbox.enabled
  - outbox.bridges.<n>.name
  - outbox.bridges.<n>.type
  - outbox.bridges.<n>.config.url
  - outbox.bridges.<n>.config.pattern
  - outbox.bridges.<n>.config.headers
  - outbox.bridges.<n>.config.secret
  - outbox.bridges.<n>.config.payload_encoding
---

# Storage & background tasks

This page covers four subsystems that share one trait: they all talk to
something outside your process.

- **File storage** — one API over the local filesystem, S3, GCS and Azure.
- **Background tasks**, plus the **jobs and webhooks** a module declares for
  itself — recurring work and inbound callbacks.
- **Transactional outbox** — events that become durable exactly when the
  transaction producing them commits.
- **Mail** — SMTP, and vendor providers that install as plugins.

Because any of them can fail independently, storage and mail calls are
wrapped in circuit breakers by default.

## File storage (`pkg/storage`)

`pkg/storage` is a provider-agnostic file storage abstraction, with an
interface designed to last through `v1.x`. The same code runs against:

- the local filesystem — built in, no extra dependency,
- AWS S3 (and anything S3-compatible: MinIO, R2, Spaces),
- Google Cloud Storage,
- Azure Blob Storage.

:::info Cloud backends are separate modules

The framework carries local storage only. A cloud backend is one `go get` and
one blank import away, and nothing else changes — the configuration is the
same, and so is every line of code that uses `a.Storage`:

```go
import _ "github.com/jcsvwinston/nucleus/providers/storage-s3"
// or providers/storage-gcs, or providers/storage-azure
```

They live outside the framework because a client library nobody opens is not
free: the three cloud SDKs together weighed 42.6 MB of binary and 139 modules
that every application paid for, whether or not it ever wrote to a bucket.

If you configure `provider: s3` without the import, startup fails and the
error prints the two lines above. It never quietly falls back to local
storage — a silent fallback is how uploads end up on a container disk that
disappears with the pod.

:::

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

## Using a storage backend Nucleus does not ship

The four built-in providers — local, S3, GCS, Azure — are registered the
same way anyone else registers one, so a backend this framework has never
heard of is selectable by name without patching it:

```go
package cephstore

import "github.com/jcsvwinston/nucleus/pkg/storage/provider"

func init() {
    provider.Register("ceph", New)
}
```

Note the import: `pkg/storage/provider`, not `pkg/storage`. It is a leaf
package holding the contract and nothing else — the `Store` interface, its value
types, the configuration and the registry — so writing a backend does not
drag in the AWS, Azure and Google Cloud SDKs that the built-in
implementations need. The names remain available from `pkg/storage` as
aliases, so existing code keeps compiling.

Import that package for its side effects, set `storage.provider: ceph`, and
the framework builds it. Everything it layers on top — the circuit breaker,
tenant prefixing, the public-URL mapper — is applied around whatever your
factory returns, so a provider never reimplements any of it.

Your backend reads its own settings from its own subtree:

```yaml
storage:
  provider: ceph
  ceph:
    endpoint: http://ceph.internal
    pool: 32
```

```go
func New(cfg storage.Config) (storage.Store, error) {
    var c struct {
        Endpoint string        `koanf:"endpoint" validate:"required"`
        Pool     int           `koanf:"pool" default:"8"`
        Timeout  time.Duration `koanf:"timeout" default:"5s"`
    }
    if err := cfg.BindProvider(&c); err != nil {
        return nil, err
    }
    …
}
```

The framework validates the namespace but not its contents — it cannot know
the shape of a backend it has never seen, which is the point of the
registry. What it does know is which providers are registered, so
`storage.ceph.*` is accepted when `ceph` is and rejected when it is not: a
misspelled section still fails as an unknown key rather than being ignored.

Inside `BindProvider`, a key your struct does not declare is an error too.
Provider configuration is exactly the place a typo would otherwise sit
unnoticed until the day the setting mattered.

Registering a name that is already taken is an error rather than a silent
replacement: two packages claiming `s3` would otherwise make the effective
backend depend on import order.

An unconfigured or unknown provider name now fails with the list of
registered ones. It used to fall through to the local filesystem, so a typo
wrote your uploads to disk and said nothing.

## Circuit breaker (storage)

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

`pkg/tasks` runs one-off background tasks. Payloads are encoded as JSON
and keyed by a task-type string; the manager handles enqueue, retry,
dead-letter and metrics. Two providers ship in-tree:

- `pkg/tasks/providers/memory` — in-process, no external dependency.
  Pending tasks are lost on restart.
- `pkg/tasks/providers/asynq` — **Asynq** + Redis, durable.

For *recurring* work declared by a module, use module jobs (next
section) — the framework schedules those for you. `tasks.Manager` is the
lower-level surface for one-off tasks, and there are two ways to hold one.

### The standalone cycle: build, register, run, close

`tasks.Manager` is **not** a field on `App` — you construct it from the
provider you chose, register handlers, run the worker loop, and close it
on shutdown. The complete cycle, compilable as shown:

```go
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
	asynqprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq"
)

const TypeSendWelcomeEmail = "email:welcome"

type SendWelcomeEmail struct {
	UserID int64
}

func main() {
	// memoryprovider.NewManager takes the same arguments for an
	// in-process, non-durable queue (no Redis needed).
	mgr, err := asynqprovider.NewManager(tasks.Config{
		RedisURL:    "redis://localhost:6379",
		Concurrency: 8,
	}, nil) // nil logger → slog.Default()
	if err != nil {
		log.Fatal(err)
	}

	// Register handlers before the first enqueue of each type.
	err = mgr.HandleFunc(TypeSendWelcomeEmail, func(ctx context.Context, t tasks.Task) error {
		var p SendWelcomeEmail
		if err := tasks.DecodeJSONPayload(t, &p); err != nil {
			return err
		}
		return sendWelcome(ctx, p.UserID)
	})
	if err != nil {
		log.Fatal(err)
	}

	// Run blocks until ctx is cancelled; run it in the same process as
	// the enqueuers or in a dedicated worker binary.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := mgr.Run(ctx); err != nil {
			log.Printf("tasks: %v", err)
		}
	}()

	// Enqueue from anywhere that holds the manager (payload is
	// JSON-encoded for you):
	if _, err := mgr.EnqueueJSON(TypeSendWelcomeEmail, SendWelcomeEmail{UserID: 42}); err != nil {
		log.Printf("enqueue: %v", err)
	}

	<-ctx.Done()
	_ = mgr.Close()
}

func sendWelcome(ctx context.Context, userID int64) error { return nil }
```

`EnqueueJSONWithPolicy` and friends accept a `tasks.EnqueuePolicy` for
queue, retry, timeout, delay and retention control.

### The module path: `rt.Tasks()`

An application built on `pkg/nucleus` does not need to construct a manager
at all: the framework builds one for the module-jobs runtime, and a module
reaches it through its `Runtime` handle — `rt.Tasks()` returns that shared
manager for enqueueing one-off tasks (and registering their handlers)
beyond the cron jobs it declared.

Availability is explicit: the jobs runtime is built when at least one
module registers a job, or when `jobs_provider` names a broker-backed
provider (`asynq`) — configuring a broker is the opt-in for enqueue-only
applications. `rt.Tasks()` returns nil inside `OnStart` (the runtime starts
after every module's `OnStart`); resolve it lazily from request handlers,
and treat nil as "no jobs runtime configured". `rt.Outbox()` follows the
same degrade-to-nil contract for the transactional outbox.

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

**Jobs.** Each registration needs exactly one schedule: `Every` for a fixed
interval, or `Cron` for a standard 5-field expression or a descriptor such as
`@hourly`. Cron expressions are validated at boot and mean the same thing on
every provider.

The `jobs_provider` config key selects the runtime:

- `memory` (default) — in-process. Pending jobs are lost on restart.
- `asynq` — Redis-backed and durable. Set `jobs_redis_url`.

`jobs_concurrency` caps the number of parallel workers. A broken registration
— duplicate name, invalid cron, missing handler — fails boot rather than
silently never running.

With `asynq` and more than one replica of your process, workers are safe to
replicate (they consume a shared queue) but schedulers are not — each one
would enqueue every cron entry on every tick. The framework therefore runs
the scheduler under leader election over a Redis lock (`SET NX` + TTL): one
replica ticks, the rest stand by, and a crashed leader is replaced within
the lock TTL. That is the `jobs_scheduler_lock` key, on by default; setting
it to `false` opts out and the boot log warns that every replica will fire
every job.

**Webhooks.** Each registration mounts a real route at
`<webhooks_prefix>/<module-name><path>`; the default prefix is `/webhooks`.

Incoming requests are checked in this order, all before your handler runs:

1. **Method** — POST only by default. Anything else gets 405.
2. **Body size** — 1 MiB by default. Larger bodies get 413.
3. **Signature** — with a `Secret` set, the request must carry an HMAC-SHA256
   of the raw body in the `X-Nucleus-Signature` header, as `sha256=<hex>`.
   Unsigned or mis-signed requests get 401. Senders and tests can produce the
   value with `nucleus.SignWebhookBody`.

Two more rules apply at boot:

- A webhook registered **without** a `Secret` is still mounted, but logs a
  WARN — its handler must authenticate callers itself.
- Registration paths must be canonical. A path that `path.Clean` would
  rewrite (`.` or `..` segments, duplicate or trailing slashes) fails boot
  with a clear error, instead of silently mounting a route that cleaned
  request URLs can never reach.

When `csrf_enabled` is on, the webhook prefix is exempted automatically:
webhooks authenticate by signature, not by CSRF token.

**Replay, declared honestly.** The signature authenticates content, not
freshness — a captured signed request verifies again if it is re-sent
verbatim. If your handler's effect is not idempotent, deduplicate on an event
ID carried in the payload.

To narrow the replay window, set `TimestampTolerance` on the spec. That
changes the contract for senders:

- The request must carry its send time as Unix seconds in the
  `X-Nucleus-Timestamp` header.
- That time must fall within the tolerance of the receiver's clock.
- The signature must cover `<timestamp>.<body>` rather than the body alone.
  `nucleus.SignWebhookBodyWithTimestamp` returns both header values.

A timestamp that is missing, malformed, stale, future-dated, or signed over
the body alone is rejected with 401 before your handler runs.

Because it changes what senders must sign, the scheme is opt-in: leaving
`TimestampTolerance` unset keeps the body-only behaviour. `5m` is a sensible
tolerance when senders have synced clocks, and event-ID deduplication still
closes the window the tolerance leaves open.

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
With `outbox.enabled: true` the framework starts a leasing dispatcher
that polls the table and delivers committed events through the bridges
declared under `outbox.bridges` (all keys in the
[Configuration reference](../reference/configuration.md)).

### Shutdown

Stopping the outbox is graceful: the dispatcher finishes the pass it is in
— including the delivery in flight — and only then exits. A pass is not
abandoned halfway, so a message is never left claimed by a delivery that
was cut off mid-attempt (it would have waited for its lease to expire
before anyone retried it).

Waiting is bounded. `Stop(ctx)` escalates to cancelling the pass when the
context you pass expires, or after five seconds if it carries no deadline
— a bridge that never answers cannot hold shutdown hostage. The escalation
is logged at WARN so a slow shutdown is visible rather than silent.

### Webhook bridge: delivery contract

A `webhook` bridge POSTs each outbox message as a JSON body to the
configured URL:

```json
{
  "id": "msg-01hzy4v7",
  "topic": "orders.placed",
  "payload": "eyJvcmRlcl9pZCI6NDJ9",
  "status": "pending",
  "attempts": 1,
  "available_at": "2026-07-22T12:00:00Z",
  "created_at": "2026-07-22T12:00:00Z"
}
```

Every delivery carries the header `X-Outbox-Payload-Encoding` declaring
the shape of the `payload` field for **that** message, so the receiver
never guesses:

- `base64` — the default: `payload` is a JSON string holding the base64
  encoding of the raw payload bytes (the example above). Decode the
  string to get the payload document. This is the classic wire shape —
  byte-for-byte what every release up to v1.4.0 emits — so existing
  consumers keep working unchanged; the header is purely additive.
- `json` — opt-in per bridge with `payload_encoding: json`: the payload
  document is embedded verbatim, e.g. `"payload": {"order_id": 42}`,
  and the consumer reads it directly with no base64 round-trip. Two
  edge cases: a payload that is not valid JSON (possible only for rows
  not written by the framework's own enqueue path) falls back to the
  base64 string form **and declares `base64` in the header** for that
  delivery; a message with no payload puts JSON `null` in the field.

```yaml
outbox:
  enabled: true
  bridges:
    - name: order-hooks
      type: webhook
      config:
        url: "https://consumer.example.com/hooks/outbox"
        pattern: "orders.*"
        payload_encoding: json   # opt-in; omit for the base64 default
        secret: "shared-webhook-secret"
```

### Webhook bridge: HMAC signature

With `secret` set, the bridge signs every delivery: the header
`X-Nucleus-Signature` carries `sha256=<hex>`, the HMAC-SHA256 of the
exact body bytes under the shared secret. It is the **same header and
the same scheme** module webhooks verify (previous section), so one
verifier covers both directions — and `nucleus.SignWebhookBody`
computes the expected value. Verify with a constant-time comparison,
never `==`/`!=`:

```go
import (
    "crypto/hmac"
    "io"
    "net/http"

    "github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func outboxHook(w http.ResponseWriter, r *http.Request) {
    body, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, "bad request", http.StatusBadRequest)
        return
    }
    want := nucleus.SignWebhookBody(secret, body)
    got := r.Header.Get(nucleus.WebhookSignatureHeader)
    if !hmac.Equal([]byte(want), []byte(got)) {
        http.Error(w, "bad signature", http.StatusUnauthorized)
        return
    }
    // The body is authentic. The X-Outbox-Payload-Encoding header is
    // informational and travels UNSIGNED — the signature covers the body
    // alone — so decode by the encoding you configured for this bridge
    // (base64 unless you set payload_encoding: json), not by trusting the
    // header. Treat a header that disagrees with your configured encoding as
    // a bad request rather than as an instruction to switch decoders.
}
```

Without a `secret` the bridge delivers unsigned and logs a boot
warning: the consumer must then authenticate deliveries itself (for
example with a static header under `config.headers`) — and a static
header does not authenticate the *body*, so prefer the signature.

Scope, honestly stated: the signature authenticates the body and nothing
else. There is **no anti-replay protection** here — no timestamp in the
signed material, no nonce — so a captured delivery verifies again if
replayed.

In practice that is tolerable because outbox delivery is at-least-once
anyway: consumers must already be idempotent, keyed on the message `id`.

Deliveries to plain `http://` URLs send the body in clear. Use HTTPS outside
loopback.

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
in the [Plugin SDK reference](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/PLUGIN_SDK.md).
Stated plainly: the contract is documented and frozen, but **no runnable
example plugin ships in-tree today** — writing one means implementing the
envelope in the SDK reference from scratch.

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
