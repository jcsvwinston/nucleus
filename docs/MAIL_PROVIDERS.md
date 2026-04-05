# Mail Providers and Plugins

Reference date: 2026-04-05.
Status: Current.

GoFrame includes a pluggable mail layer in `pkg/mail`.

## Supported Drivers

Built-in drivers:

- `noop`
- `smtp`
- `sendgrid`

Extensibility options:

- in-process registration via `mail.RegisterProvider(...)`
- external binary plugins on `PATH`:
  - `goframe-plugin-<provider>` (capability discovery)
  - `goframe-mail-<driver>` (legacy mail compatibility)

## Configuration

Typical keys in `goframe.yaml`:

```yaml
mail_driver: noop
mail_from: noreply@localhost

smtp_host: ""
smtp_port: 587
smtp_user: ""
smtp_pass: ""

sendgrid_api_key: ""
sendgrid_endpoint: https://api.sendgrid.com/v3/mail/send
```

## Operational Commands

```bash
goframe sendtestemail --config goframe.yaml --to dev@example.com --dry-run
goframe sendtestemail --config goframe.yaml --driver sendgrid --to dev@example.com --dry-run
goframe mailproviders --config goframe.yaml
goframe mailproviders --config goframe.yaml --json
goframe plugin list --config goframe.yaml
goframe plugin doctor --config goframe.yaml
goframe plugin test --provider sendgrid --capability mail.send
```

## External Plugin Contract

If `mail_driver: mailgun`, GoFrame resolves in this order:

1. `goframe-plugin-mailgun` (requires capability `mail.send`)
2. `goframe-mail-mailgun` (legacy fallback)

Generic capability plugins receive `pkg/plugins` request envelope (`version: v1`) over `stdin`.

Legacy mail plugins receive JSON over `stdin`:

- `driver`
- `from`
- `to`
- `subject`
- `body`
- `headers`

Exit code contract:

- `0`: accepted
- non-zero: failed
