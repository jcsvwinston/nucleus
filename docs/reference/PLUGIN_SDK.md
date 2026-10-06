# Plugin SDK v1

Reference date: 2026-10-05.
Status: Current.

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

The same delivery can be written either way, and the repository carries
both as tested fixtures: `internal/fixtures/inprocess/dirqueue` is a module
that registers an outbox bridge written in Go, and
`internal/fixtures/plugins/nucleus-plugin-relay` is the external plugin that
does the same `queue.publish` delivery through the envelope (see
[Outbox Bridge](#outbox-bridge-queuepublish-and-webhookdeliver)). In process
is one Go type, typed configuration and no process per message, compiled into
the application; out of process is any language, an isolated process and an
allowlist entry.

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

## Writing a Plugin

The plugin side of the contract is `plugins.Serve`
(`github.com/jcsvwinston/nucleus/pkg/plugins`, standard library only): a
`plugins.Plugin` with one typed handler per capability, and `Serve` speaks
the rest — it answers `capabilities` and `capabilities --json` with the
capabilities whose handler is set, reads the request envelope from stdin,
decodes the payload into the capability's type, writes the response
envelope to stdout and exits with the contract's code.

```go
package main

import (
	"context"

	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

func main() {
	plugins.Serve(plugins.Plugin{
		MailSend: func(ctx context.Context, req plugins.RequestEnvelope, m plugins.MailSendPayload) (plugins.MailSendOutput, error) {
			if len(m.To) == 0 {
				return plugins.MailSendOutput{}, plugins.Fail(plugins.ExitCodeValidation, "INVALID_MESSAGE", "the message has no recipient")
			}
			// deliver m …
			return plugins.MailSendOutput{ProviderRequestID: "provider-123"}, nil
		},
	})
}
```

- A handler that returns a nil error has accepted the request; `Serve` sets
  the output's `accepted`, echoes `request_id`, and lifts the output's
  provider id into `provider_request_id`.
- To fail, return `plugins.Fail(exitCode, code, message)`: the exit code is
  the process's, the code and message are the response's `error`, and
  `retriable` follows the exit code (20, 40 and 50 are retriable). Any
  other error is exit `50`; a handler still running at `timeout_ms` sees its
  context cancelled, and a deadline error becomes exit `40`.
- A malformed envelope, another envelope version, a payload of the wrong
  shape and a capability the plugin does not serve are exit `10`, with the
  reason on stderr. Payloads decode permissively: a field the plugin does
  not know is ignored, so a host that adds one does not break it.
- Capabilities without a schema in `pkg/plugins` go in `Plugin.Custom`,
  keyed by name; the handler decodes its own payload.
- Write logs to stderr. Stdout carries the response envelope and nothing
  else.

`plugins.ServeIO` is the same function with the arguments and streams
passed in and the exit code returned, for a plugin's own tests.

## Example Plugin

`internal/fixtures/plugins/nucleus-plugin-maildir` is a complete plugin
built on `Serve`: a `mail.send` provider that delivers each message into a
Maildir (`$MAILDIR`, `$HOME/Maildir` when unset), written to `tmp/` and
renamed into `new/`. A line break in a header is exit `10`; a Maildir that
cannot be written is exit `20`. Its test builds it into an executable, puts
it on `PATH` and reaches it through the real runtime — the mail sender
`mail_driver: maildir` builds, and `nucleus plugin test --execute` — so CI
runs it on every change. It is the starting point to copy into a module of
your own:

```bash
go build -o "$(go env GOPATH)/bin/nucleus-plugin-maildir" ./internal/fixtures/plugins/nucleus-plugin-maildir
nucleus plugin test --provider maildir --execute
```

The repository ships no `examples/` directory: examples live as tested
fixtures, so an example that stops working is a red build.

## Outbox Bridge (`queue.publish` and `webhook.deliver`)

An outbox bridge of type `plugin` hands each outbox message to an external
plugin, so a committed event can reach a broker or an endpoint the framework
has no code for:

```yaml
outbox:
  enabled: true
  bridges:
    - name: events
      type: plugin
      config:
        provider: relay              # runs nucleus-plugin-relay
        capability: queue.publish    # or webhook.deliver
        pattern: "orders.*"          # default "*"
        timeout: 10s                 # default; the plugin is killed at it
        topic: shop-events           # queue.publish: default, the message's topic
plugins:
  allowed:
    - provider: relay
      capabilities: [queue.publish]
```

At boot the bridge checks it can deliver: the `plugins` block allows the
provider to run the capability, `nucleus-plugin-<provider>` is on `PATH`,
and asked `capabilities` it lists this one. Any of the three failing stops
the application from starting, with the reason. Without an allowlist the
plugin runs, and the boot log says once that it will need listing
(DEP-2026-014), as for a mail plugin.

Each delivery is one request envelope. Its `metadata` names the message —
`outbox_message_id`, `outbox_topic`, `outbox_attempt`, `outbox_bridge` —
and its payload is:

- `queue.publish`: a `QueuePublishPayload` — `topic` (the message's topic,
  or `config.topic`), `key` (the message id), `body` (the payload's JSON
  document, verbatim) and `headers` (`config.headers`).
- `webhook.deliver`: a `WebhookDeliverPayload` carrying exactly the request
  the `webhook` bridge would send — the same JSON body, the same
  `X-Outbox-Payload-Encoding` header and, with `config.secret`, the same
  `X-Nucleus-Signature` — to `config.url` with `config.method` (POST by
  default) and `config.headers`. A consumer cannot tell which transport
  delivered it, and verifies it the same way. `timeout_ms` is four fifths
  of the bridge's timeout, so the request ends before the plugin is killed.

Delivery is at least once, as everywhere in the outbox: a plugin that must
not act twice deduplicates on `outbox_message_id` (the `queue.publish` key).

The plugin's answer decides what happens to the message, through the exit
codes above:

| Plugin answer | Outbox |
|---|---|
| `0`, accepted (and, for `webhook.deliver`, a reported status in 2xx) | delivered |
| `10` validation or `30` rejection, not marked `retriable` | the dead letter (`failed`) on that attempt, its reason in `last_error` |
| `20`, `40`, `50`, any other exit, a crash, a timeout, a missing binary | retried with the outbox's backoff until `outbox.max_retries`, then the dead letter |

A message in the dead letter comes back with `nucleus outbox requeue`. In
Go, the rule is `outbox.Permanent`: a bridge that returns an error wrapped
with it sends the message to the dead letter without spending its other
attempts; `outbox.PluginBridge` returns it for exits `10` and `30`.

`internal/fixtures/plugins/nucleus-plugin-relay` serves both capabilities:
`queue.publish` writes each message into a directory queue
(`$RELAY_QUEUE_DIR/<topic>/new/<key>.json`, written to `tmp/` and renamed,
so a redelivery replaces its own file), and `webhook.deliver` sends the
request over HTTP — a 2xx is a delivery, 408, 425, 429 and 5xx are exit
`20`, any other status exit `30`. Its test runs it behind an application's
outbox, so CI delivers through it on every change; it is the starting point
for a bridge to a broker of your own.

## Runtime Safety Rules

- plugin execution is bounded by a timeout (`timeout_ms` in the envelope;
  the host kills the process at the deadline)
- the configuration can allowlist the external executables that run
  (`plugins.*`, below); an executable it does not allow is never executed,
  not even to read its capabilities
- redact secrets in logs and error surfaces
- `Serve` refuses a request larger than 32 MiB
- preserve `request_id` and `trace_id` across boundaries (`Serve` echoes
  `request_id`; `nucleus plugin test --execute` warns when a response does
  not)

## Configuration

```yaml
plugins:
  allow_external: true          # false: no external plugin or command runs
  allowed:                      # when set: the only capability plugins that run
    - provider: sendgrid
      capabilities: [mail.send]
    - provider: stripe
      capabilities: [subscription.create, subscription.cancel]
  commands: [lint]              # when set: the only nucleus-<name> commands dispatched
```

- `allowed` — a provider runs a capability only when an entry names the
  provider and lists the capability. An entry needs a provider and at least
  one `domain.action` capability; the strict configuration check refuses
  one that could never match.
- `commands` — `nucleus <name>` runs `nucleus-<name>` only when the list
  names it.
- `allow_external: false` refuses every external executable whatever the
  lists say (`NUCLEUS_PLUGINS__ALLOW_EXTERNAL=false` from the environment).
- Enforced by the runtime (the mail sender `mail_driver` selects), by
  `nucleus sendtestemail` and the `nucleus plugin` commands, and by the
  dispatcher of `nucleus <name>`, which reads the configuration in the
  working directory (`nucleus.yml`, or the file `NUCLEUS_CONFIG` names).
  A configuration that declares a `plugins` block and does not load refuses
  the external command rather than running it unchecked.

**Opt-in until v2.0.0.** With nothing set, every external plugin and command
runs, as before the block existed; an empty list restricts nothing. An
application that selects an external mail plugin without listing it logs
one WARN at boot, and `nucleus plugin doctor` warns. From v2.0.0 an external
plugin or command runs only when the configuration lists it
(DEP-2026-014).

`enabled`, `exec_timeout` and `max_payload_bytes`, proposed in earlier
revisions of this page, are not implemented, and the configuration refuses
them as unknown keys.

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

<!-- retired-claims-allow: names the removed built-in driver on purpose -->
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

Exit code contract: the one above — `0` accepted, `10`…`50` failed, with
the response's `error` saying why.

## CLI and Diagnostics

- `nucleus plugin list` — the capability plugins on `PATH` with their
  capabilities, the external commands (`nucleus-<name>`), and what the
  configuration's `plugins` block refuses (`--json` for the report)
- `nucleus plugin doctor` — runtime and configuration checks, the allowlist
  among them
- `nucleus plugin test --provider <p> [--capability <c>]` — discovery: the
  plugin is found and advertises the capability
- `nucleus plugin test --provider <p> [--capability <c>] --execute` — sends
  the plugin one real request envelope per capability (the one named, or
  every one it advertises) with a sample payload addressed nowhere real
  (`example.invalid`), or the payload `--payload <file|->` holds; checks the
  exit code, the response envelope, `accepted` and the echoed `request_id`;
  and fails with the plugin's exit code and stderr. The envelope's metadata
  carries `source: nucleus plugin test`. A real provider delivers what it
  is sent: pass `--payload` with a message you mean to send.

## External Commands

An executable named `nucleus-<name>` on `PATH` runs as `nucleus <name>`,
with the arguments, stdin, stdout, stderr and exit code passed through.
`nucleus --help` lists the ones it finds, `nucleus help <name>` runs
`nucleus-<name> --help`, and `nucleus plugin list` lists them with their
paths. A built-in command or alias of the same name wins, and `plugin list`
marks the executable shadowed. `plugins.commands` and
`plugins.allow_external` decide which ones may run.

## Official Example Plugins

- `internal/fixtures/plugins/nucleus-plugin-maildir` — `mail.send` (see
  Example Plugin above).
- `internal/fixtures/plugins/nucleus-plugin-relay` — `queue.publish` and
  `webhook.deliver`, behind the outbox (see Outbox Bridge above).
- `internal/fixtures/inprocess/dirqueue` — not a plugin: the in-process
  counterpart of the relay's `queue.publish`, a module that registers an
  outbox bridge written in Go.

The pair that shipped under `examples/plugins/` until the ADR-010 Phase 1
iteration (2026-05-16) went with the rest of `examples/`; the fixtures
replace it, tested instead of documented.

## Compatibility Commitments

- `version: v1` envelope fields remain backward compatible across the v1 line
- breaking contract changes require a new envelope version (`v2`)

Runtime bridge status:

- `pkg/mail.NewSender` resolves external mail providers via
  `nucleus-plugin-<driver>` on `PATH` when capability `mail.send` is
  advertised and the configuration's `plugins` block allows it. There is
  no legacy fallback.
- `queue.publish` and `webhook.deliver` reach an external plugin through
  the outbox: a bridge of type `plugin` (`outbox.NewPluginBridge`), under
  the same allowlist.
- `subscription.create` and `subscription.cancel` have no schema and no
  bridge.

## Test Strategy

Contract tests should cover:

- valid request/response lifecycle
- malformed payload handling
- timeout and retry semantics
- exit-code mapping to framework errors
- redaction and structured observability fields

## Open Decisions

- provider auth secret injection strategy (env vs secret store abstraction)

Decided: there is one naming pattern (`nucleus-plugin-<provider>`), and
`Serve` decodes payloads permissively — an unknown field is ignored.
