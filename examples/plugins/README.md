# Official Plugin SDK Examples

This directory contains the official GoFrame `v0.6.x` Plugin SDK v1 examples required by the roadmap:

- `mail/`: provider `examplemail` implementing `mail.send`
- `queue/`: provider `examplequeue` implementing `queue.publish`

Both examples:

- implement `capabilities` and `capabilities --json`
- read SDK v1 envelopes from `stdin`
- return SDK v1 response envelopes to `stdout`
- map validation/internal failures to framework exit-code semantics

## Build Example Binaries

From repository root:

```bash
mkdir -p .tmp/plugins

go build -o .tmp/plugins/goframe-plugin-examplemail ./examples/plugins/mail
go build -o .tmp/plugins/goframe-plugin-examplequeue ./examples/plugins/queue
```

Then prepend to `PATH`:

```bash
export PATH="$(pwd)/.tmp/plugins:$PATH"
```

## Verify Discovery and Diagnostics

```bash
goframe plugin list --config goframe.yaml
goframe plugin doctor --config goframe.yaml

goframe plugin test --provider examplemail --capability mail.send --execute
goframe plugin test --provider examplequeue --capability queue.publish --execute
```

## End-to-End Mail Smoke

Set in `goframe.yaml`:

```yaml
mail_driver: examplemail
mail_from: noreply@example.com
```

Run:

```bash
goframe sendtestemail --config goframe.yaml --to dev@example.com --subject "mail plugin smoke"
```

## Queue Request Smoke (Direct Envelope Call)

```bash
cat <<'JSON' | goframe-plugin-examplequeue
{
  "version": "v1",
  "request_id": "req_queue_demo",
  "timestamp": "2026-04-05T12:00:00Z",
  "capability": "queue.publish",
  "provider": "examplequeue",
  "timeout_ms": 5000,
  "payload": {
    "topic": "events.users",
    "body": {"event": "user.created", "id": 42}
  }
}
JSON
```
