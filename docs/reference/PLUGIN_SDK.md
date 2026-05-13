# Plugin SDK v1

Reference date: 2026-04-05.
Status: Current (pre-v1 baseline).
Target baseline: Nucleus pre-v1.

## Goal

Provide a stable, capability-based plugin contract so Nucleus can be extended beyond mail integrations.

Examples of target domains:

- mail providers
- queue/message bus providers
- subscription and billing providers
- webhook and external service connectors

## Design Principles

- capability-first, not provider-first
- explicit request/response contracts
- deterministic error and retry semantics
- secure defaults (timeouts, allowlists, redaction)
- backward compatibility across the current pre-v1 line

## Capability Model

A plugin advertises one or more capabilities using `domain.action` naming:

- `mail.send`
- `queue.publish`
- `subscription.create`
- `subscription.cancel`
- `webhook.deliver`

A provider can implement multiple capabilities.

## Plugin Types

1. In-process providers (Go API)
- registered in runtime via framework registry
- low latency, strong typing, no process boundary

2. External executable providers
- isolated process called by Nucleus
- language-agnostic and deploy-flexible
- contract enforced through JSON envelopes and exit codes

## External Plugin Naming

Naming convention:

- `nucleus-plugin-<provider>`

This is the only external plugin discovery prefix. There is no legacy
fallback; mail providers are exposed via the standard capability
mechanism (`mail.send`).

## Request Envelope (External)

```json
{
  "version": "v1",
  "request_id": "req_01J...",
  "timestamp": "2026-04-05T12:00:00Z",
  "capability": "mail.send",
  "provider": "sendgrid",
  "timeout_ms": 10000,
  "metadata": {
    "env": "production",
    "trace_id": "...",
    "tenant": "acme"
  },
  "payload": {
    "to": ["dev@example.com"],
    "subject": "hello",
    "body": "..."
  }
}
```

## Response Envelope (External)

```json
{
  "version": "v1",
  "request_id": "req_01J...",
  "ok": true,
  "provider_request_id": "provider-123",
  "retriable": false,
  "output": {
    "accepted": true
  },
  "error": null,
  "metrics": {
    "duration_ms": 132
  }
}
```

Error response example:

```json
{
  "version": "v1",
  "request_id": "req_01J...",
  "ok": false,
  "retriable": true,
  "error": {
    "code": "PROVIDER_RATE_LIMIT",
    "message": "rate limit exceeded"
  }
}
```

## Exit Codes (External)

- `0`: success (`ok=true`)
- `10`: validation/config error (non-retriable)
- `20`: transient provider/network error (retriable)
- `30`: permanent provider rejection (non-retriable)
- `40`: timeout/deadline exceeded (retriable)
- `50`: internal plugin failure (retriable by policy)

## Runtime Safety Rules

- plugin execution must be bounded by timeout (`timeout_ms`)
- plugin binary path must be allowlisted/configured
- redact secrets in logs and error surfaces
- enforce payload size limits
- preserve `request_id` and `trace_id` across boundaries

## Configuration Model (Proposed)

Suggested `nucleus.yml` shape:

```yaml
plugins:
  enabled: true
  allow_external: true
  exec_timeout: 10s
  max_payload_bytes: 262144
  allowed:
    - provider: sendgrid
      capabilities: [mail.send]
    - provider: stripe
      capabilities: [subscription.create, subscription.cancel]
```

## Baseline Capability Schemas for v0.6.0

Minimum schemas to define and ship in `v0.6.0`:

- `mail.send`
- `queue.publish`
- `webhook.deliver`

Current baseline implementation includes typed payload/response structs in `pkg/plugins`:

- `MailSendPayload` / `MailSendOutput`
- `QueuePublishPayload` / `QueuePublishOutput`
- `WebhookDeliverPayload` / `WebhookDeliverOutput`

Stretch schema set:

- `subscription.create`
- `subscription.cancel`

## Mail Provider Plugins

Nucleus includes a pluggable mail layer in `pkg/mail` that uses the plugin SDK.

**Built-in drivers:**
- `noop`
- `smtp`

Vendor-specific drivers (SendGrid, Mailgun, AWS SES, Postmark, …) install as `nucleus-plugin-<driver>` binaries — see [MA-2026-002](../migration_assistants/MA-2026-002-sendgrid-builtin-to-plugin.md) for the migration trail away from the previously built-in `sendgrid`.

**Extensibility:**
- In-process registration via `mail.RegisterProvider(...)`
- External binary plugins on `PATH`: `nucleus-plugin-<provider>` advertising the `mail.send` capability.

**Configuration:**
```yaml
mail_driver: noop
mail_from: noreply@localhost

smtp_host: ""
smtp_port: 587
smtp_user: ""
smtp_pass: ""

# Vendor plugins read their own credentials from env vars per their
# documented contract (typically SENDGRID_API_KEY, MAILGUN_API_KEY,
# AWS_ACCESS_KEY_ID, etc.). The framework no longer surfaces those
# keys in nucleus.yml.
```

**Operational Commands:**
```bash
nucleus sendtestemail --config nucleus.yml --to dev@example.com --dry-run
nucleus sendtestemail --config nucleus.yml --driver sendgrid --to dev@example.com --dry-run
nucleus mailproviders --config nucleus.yml
nucleus mailproviders --config nucleus.yml --json
nucleus plugin list --config nucleus.yml
nucleus plugin doctor --config nucleus.yml
nucleus plugin test --provider sendgrid --capability mail.send
```

**External Plugin Contract:**
If `mail_driver: mailgun`, Nucleus looks up `nucleus-plugin-mailgun` on
`PATH` and requires it to advertise the `mail.send` capability.

Capability plugins receive a `pkg/plugins` request envelope
(`version: v1`) over `stdin`.

Exit code contract:
- `0`: accepted
- non-zero: failed

## CLI and Diagnostics (Current Baseline)

- `nucleus plugin list` (detected providers and capabilities)
- `nucleus plugin doctor` (runtime/config validation)
- `nucleus plugin test --provider <p> --capability <c>` (contract smoke)

## Official Example Plugins (Current Baseline)

Repository-shipped examples:

- `examples/plugins/mail`: `nucleus-plugin-examplemail` (`mail.send`)
- `examples/plugins/queue`: `nucleus-plugin-examplequeue` (`queue.publish`)

Reference guide:

- `docs/PLUGIN_EXAMPLES.md`

## Compatibility Commitments (pre-v1)

- `version: v1` envelope fields remain backward compatible throughout the current pre-v1 line
- breaking contract changes require a new envelope version (`v2`)

Runtime bridge status:

- `pkg/mail.NewSender` resolves external mail providers via
  `nucleus-plugin-<driver>` on `PATH` when capability `mail.send` is
  advertised. There is no legacy fallback.

## Test Strategy

Contract tests should cover:

- valid request/response lifecycle
- malformed payload handling
- timeout and retry semantics
- exit-code mapping to framework errors
- redaction and structured observability fields

## Open Decisions

- final binary discovery order when both naming patterns exist
- strict vs permissive unknown field behavior in `payload`
- provider auth secret injection strategy (env vs secret store abstraction)
