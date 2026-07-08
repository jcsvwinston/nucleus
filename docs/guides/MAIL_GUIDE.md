# Mail Guide

A comprehensive guide to the Nucleus mail layer (`pkg/mail`) — a provider-agnostic outbound mail abstraction. Two protocol-universal drivers ship in-tree (`noop`, `smtp`); vendor-specific HTTP providers (SendGrid, Mailgun, AWS SES, Postmark, Resend, …) install as external plugins on `PATH`.

## Table of Contents

1. [Overview](#overview)
2. [Supported Providers](#supported-providers)
3. [Configuration](#configuration)
4. [Basic Usage](#basic-usage)
5. [External Providers via Plugins](#external-providers-via-plugins)
6. [Health Probe](#health-probe)
7. [Circuit Breaker](#circuit-breaker)
8. [Production Checklist](#production-checklist)
9. [Migration from Legacy Config](#migration-from-legacy-config)

---

## Overview

The `pkg/mail` package provides a single, stable `Sender` interface that abstracts over multiple delivery backends. Application code never changes when switching from a development `noop` sender to production SMTP, or when swapping HTTP providers via plugins.

Key design principles:

- **Protocol-universal in-tree**: only `noop` (dev/tests) and `smtp` are built in. Vendor lock-in is an explicit out-of-tree decision the operator makes by installing a plugin.
- **Capability-driven plugins**: vendor-specific HTTP providers (SendGrid et al.) install as `nucleus-plugin-<provider>` binaries on `PATH`, discovered via the `mail.send` capability of `pkg/plugins`.
- **Health-probe friendly**: senders that implement the optional `HealthChecker` interface are probed by `/healthz`; the circuit breaker never gates `Healthy()`, so a recovering dependency is observable while `Send` is short-circuited.
- **Configuration-driven**: provider selection is entirely controlled by `mail_driver` in `nucleus.yml`.
- **Durable interface**: `Sender` is designed to remain stable through v1.x.

Core types:

| Type                       | Purpose                                                                          |
|----------------------------|----------------------------------------------------------------------------------|
| `mail.Sender`              | The main interface — `Send(ctx, Message) error`.                                 |
| `mail.HealthChecker`       | Optional `Healthy(ctx) error`; the `/healthz` handler type-asserts for it.       |
| `mail.Message`             | Outbound email: `From`, `To`, `Subject`, `Body`, `Headers`.                      |
| `mail.Config`              | Sender construction: `Driver`, `Timeout`, `SMTPHost/Port/User/Pass`, `CircuitBreaker`. |
| `mail.CircuitBreakerConfig`| Breaker tuning: `Enabled`, `FailureThreshold`, `Cooldown`, `HalfOpenMaxConcurrent`. |
| `mail.ProviderFactory`     | `func(cfg Config) (Sender, error)` — register a custom in-process provider.      |

---

## Supported Providers

| Driver    | Class            | Use cases                                                                                 |
|-----------|------------------|-------------------------------------------------------------------------------------------|
| `noop`    | built-in         | Tests, local development. `Send` is a no-op; `Healthy` always returns `nil`. Never wrapped by the circuit breaker, so dev loops don't accumulate breaker state. |
| `smtp`    | built-in         | Anything that speaks SMTP (Postfix, Mailgun SMTP, AWS SES SMTP, Mailtrap, …).            |
| `sendgrid`, `mailgun`, `ses`, `postmark`, `resend`, … | external plugin | Vendor-specific HTTP APIs. Install `nucleus-plugin-<provider>` on `PATH`. The framework discovers the binary via the `mail.send` capability of `pkg/plugins`. A reference skeleton was previously shipped at `examples/plugins/mail/`; it was removed in the ADR-010 Phase 1 iteration (2026-05-16) and will be re-authored in v0.9.X. The plugin contract — `mail.send` capability, request-response JSON over a process boundary — is documented in [`docs/reference/PLUGIN_SDK.md`](../reference/PLUGIN_SDK.md). |

If `mail_driver` is empty, the framework normalises it to `noop`. An unknown driver name fails `App.New` with a clear error pointing at the plugin path.

---

## Configuration

### SMTP

```yaml
mail_driver: smtp
smtp_host: "smtp.gmail.com"
smtp_port: 587
smtp_user: "${SMTP_USER}"
smtp_pass: "${SMTP_PASS}"
mail_from: "noreply@example.com"
```

Authentication is `PLAIN`/`LOGIN` over STARTTLS when the server advertises STARTTLS, plain SMTP otherwise. Set `smtp_port: 465` for implicit TLS.

### Noop (development)

```yaml
mail_driver: noop
```

No further keys are read. The noop sender captures `Send` invocations in memory; useful in tests that want to assert on outbound mail without standing up an SMTP server.

### External plugin (SendGrid, Mailgun, …)

```yaml
mail_driver: sendgrid
```

The `mail_driver` value names the plugin: the framework looks for `nucleus-plugin-sendgrid` on `PATH` and dispatches `mail.send` calls to it. Authentication and provider tuning are owned by the plugin — typically via env vars its README documents (e.g. `SENDGRID_API_KEY`). Nothing else lives in `nucleus.yml` for plugin-backed providers.

### Circuit breaker

```yaml
mail_circuit_breaker:
  enabled: true                   # default
  failure_threshold: 5            # default
  cooldown: 30s                   # default
  half_open_max_concurrent: 1     # default
```

See [Circuit Breaker](#circuit-breaker) for behavior.

---

## Basic Usage

`App.New(cfg)` constructs `App.Mailer` from the resolved configuration. Application code receives a `mail.Sender`:

```go
err := a.Mailer.Send(ctx, mail.Message{
    From:    "noreply@example.com",
    To:      []string{"user@example.com"},
    Subject: "Welcome",
    Body:    "Hello!",
})
if err != nil {
    if errors.Is(err, circuit.ErrOpen) {
        // breaker is open; downstream dependency is failing fast
    }
    return err
}
```

### Custom headers

`mail.Message.Headers` appends custom top-level headers after the generated ones (`From`, `To`, `Subject`, `MIME-Version`, `Content-Type`). The built-in senders (SMTP and external plugins) validate the map on `Send`: a key must be non-empty, and neither key nor value may contain CR/LF — a value like `"1\r\nBcc: x@evil.example"` is rejected rather than smuggling an extra header into the message. Values are trimmed; a header whose value is empty after trimming is omitted. This is the same newline discipline already applied to `From` and `Subject`. A custom in-process provider registered via `RegisterProvider` performs its own emission and is responsible for the same discipline.

For custom in-process providers (rare — most needs are met by the SMTP driver or by an external plugin), register a `ProviderFactory` before `App.New`:

```go
_ = mail.RegisterProvider("custom-http", func(cfg mail.Config) (mail.Sender, error) {
    return &myCustomSender{timeout: cfg.Timeout}, nil
})
```

The framework normalises driver names case-insensitively. `RegisterProvider` errors on duplicates.

---

## External Providers via Plugins

Vendor-specific HTTP providers are installed as standalone binaries discovered through `pkg/plugins`:

1. Build a binary named `nucleus-plugin-<provider>` (e.g. `nucleus-plugin-sendgrid`).
2. Place it on `PATH` reachable by the running app.
3. Set `mail_driver: <provider>` in `nucleus.yml`.

The binary implements the `mail.send` capability — request-response JSON over a process boundary. The capability contract is documented in [`docs/reference/PLUGIN_SDK.md`](../reference/PLUGIN_SDK.md); a runnable skeleton will return with the v0.9.X reference applications (ADR-010 Phase 4).

Why external? The framework refuses to vendor an HTTP client per provider:

- import-graph bloat (each vendor SDK pulls retry, telemetry, auth deps),
- credential exposure (vendor-specific YAML keys leak through `nucleus.yml`),
- coupling: a vendor going EoL becomes a framework problem.

The plugin path keeps the framework neutral and lets providers ship at their own cadence.

Verify a plugin is discoverable:

```bash
nucleus mailproviders --config nucleus.yml      # lists discovered drivers
nucleus plugin doctor --config nucleus.yml      # validates the binary handshake
nucleus sendtestemail --config nucleus.yml --to dev@example.com --dry-run
```

---

## Health Probe

`pkg/health` registers a mail probe when `App.Mailer` implements `HealthChecker`. The probe is observable at `/healthz`:

```json
{
  "status": "healthy",
  "checks": [
    { "name": "mail", "status": "healthy", "latency_ms": 12 }
  ]
}
```

Senders that ship in-tree:

- `noop` — `Healthy` always returns `nil` (no I/O).
- `smtp` — `Healthy` performs a TCP dial + HELO/QUIT against the configured `smtp_host:smtp_port`. No mail is sent.

External plugin providers expose health via the `mail.health` capability; the framework type-asserts for `HealthChecker` on the wrapping sender, so plugin authors get probing "for free" by implementing `Healthy` in their binary's response shape.

**Key invariant:** the circuit breaker never gates `Healthy()`. When the breaker is open (a recovering SMTP server), `Send` short-circuits with `circuit.ErrOpen` while `Healthy` still reaches the underlying probe. The result: `/healthz` correctly reports `healthy` the moment the dependency recovers, and operators see a clean transition from open → half-open → closed without the breaker fighting the health signal.

---

## Circuit Breaker

`App.New` wraps the resolved `mail.Sender` with a `pkg/circuit.Breaker` by default. The wrap is skipped for the `noop` driver because dev-mode loops do not need breaker state.

Behavior:

- **Closed state**: `Send` calls pass through. Failures increment a counter.
- **Open state** (after `failure_threshold` consecutive failures): `Send` short-circuits with `circuit.ErrOpen` without invoking the underlying provider.
- **Half-open state** (after `cooldown` elapses): up to `half_open_max_concurrent` probe calls are admitted. Success closes the breaker; failure re-opens it.

`ErrOpen` is the load-bearing signal callers should `errors.Is`-check:

```go
if err := a.Mailer.Send(ctx, msg); err != nil {
    if errors.Is(err, circuit.ErrOpen) {
        // Mail backend is being protected; degrade gracefully.
        // Examples: persist the message for later delivery, drop a metric.
        return queueForRetry(msg)
    }
    return err
}
```

What does **not** count as a failure:

- The breaker only counts errors returned by the underlying provider. There is no separate retry policy in `pkg/mail` — retries are the provider's concern (SMTP redelivery, plugin-level retry).
- `Healthy()` calls bypass the breaker entirely (see [Health Probe](#health-probe)).

Config keys are registered in [`docs/reference/CONFIG_KEY_REGISTRY.md`](../reference/CONFIG_KEY_REGISTRY.md) under `mail_circuit_breaker.*` and marked `stable` (shape finalized 2026-07-07, v1 gate A-1d).

Opt out:

```yaml
mail_circuit_breaker:
  enabled: false
```

Tune for a noisy dependency:

```yaml
mail_circuit_breaker:
  enabled: true
  failure_threshold: 10
  cooldown: 2m
  half_open_max_concurrent: 3
```

---

## Production Checklist

Before shipping:

- [ ] `mail_driver` is set explicitly. Default-empty resolves to `noop` and silently drops outbound mail.
- [ ] For SMTP: `smtp_host`, `smtp_port`, `smtp_user`, `smtp_pass`, `mail_from` populated. Credentials sourced from env vars (`${SMTP_USER}`), never tracked in YAML.
- [ ] For plugin providers: `nucleus-plugin-<provider>` is on `PATH` of the running process. Verified via `nucleus plugin doctor`.
- [ ] `nucleus sendtestemail --to ops@example.com` succeeds at deploy time.
- [ ] `/healthz` reports `mail` as `healthy` after warm-up.
- [ ] Application code `errors.Is`-checks `circuit.ErrOpen` and degrades gracefully (queue, retry, alert).
- [ ] If `mail_circuit_breaker.enabled: false`, an explanatory comment in `nucleus.yml` justifies the decision — opting out the breaker means every `Send` will dial the provider even during sustained outages.
- [ ] Secret rotation plan in place: SMTP creds and plugin API keys come from a secret manager, not a static YAML file.

---

## Migration from Legacy Config

### Built-in SendGrid → external plugin (DEP-2026-002)

The built-in SendGrid driver was removed; SendGrid is now an external plugin like every other vendor.

Before:

```yaml
mail_driver: sendgrid
sendgrid_api_key: "${SENDGRID_API_KEY}"
sendgrid_endpoint: "https://api.sendgrid.com/v3/mail/send"
```

After:

```yaml
mail_driver: sendgrid
# nucleus-plugin-sendgrid on PATH reads SENDGRID_API_KEY from env.
```

Migration steps live in [`docs/migration_assistants/MA-2026-002-sendgrid-builtin-to-plugin.md`](../migration_assistants/MA-2026-002-sendgrid-builtin-to-plugin.md):

1. Drop `sendgrid_api_key` and `sendgrid_endpoint` from `nucleus.yml`.
2. Drop the `SendGridAPIKey` / `SendGridEndpoint` fields from any Go code constructing `mail.Config` or `app.Config`.
3. Install `nucleus-plugin-sendgrid` on `PATH` — implement the `mail.send` capability contract described in [`docs/reference/PLUGIN_SDK.md`](../reference/PLUGIN_SDK.md). The runnable reference skeleton returns with v0.9.X (ADR-010 Phase 4).
4. Set the plugin's documented env vars (typically `SENDGRID_API_KEY`).
5. Verify with `nucleus plugin doctor --config nucleus.yml` and `nucleus sendtestemail --dry-run`.

### Legacy single-secret SMTP (no change required)

The SMTP driver has been stable since `v0.5.x`. No migration needed; the same `smtp_host`/`smtp_port`/`smtp_user`/`smtp_pass`/`mail_from` keys continue to work.

### New: opting out of the circuit breaker

The autowrap shipped enabled-by-default. If your app already had its own breaker (or wraps `Send` in a retry layer that does not want short-circuiting), set `mail_circuit_breaker.enabled: false` once, before upgrading. Existing applications without the key get the default-enabled behaviour; the breaker is non-invasive in the absence of repeated failures, so the upgrade is typically silent.

---

## Related Reading

- [`docs/reference/CONFIG_KEY_REGISTRY.md`](../reference/CONFIG_KEY_REGISTRY.md) — full list of registered mail config keys (stability tags).
- [`docs/reference/API_CONTRACT_INVENTORY.md`](../reference/API_CONTRACT_INVENTORY.md) — `pkg/mail` surface stability.
- [`docs/guides/STORAGE_GUIDE.md`](STORAGE_GUIDE.md) — sister guide for the storage layer (same circuit-breaker pattern).
- [`docs/guides/OBSERVABILITY_BASELINE.md`](OBSERVABILITY_BASELINE.md) — where mail metrics show up in `/metrics` and `/healthz`.
- [`docs/reference/PLUGIN_SDK.md`](../reference/PLUGIN_SDK.md) — `mail.send` capability contract for external-provider plugins. A runnable reference skeleton returns with v0.9.X.
