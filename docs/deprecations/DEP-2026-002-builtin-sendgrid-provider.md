# Deprecation Notice: Built-in `sendgrid` mail provider

- ID: `DEP-2026-002`
- Status: `removed`
- Announced in: `v0.7.0`
- Earliest removal: `v0.7.0` (announced and removed in the same release — retroactive notice, same pattern as DEP-2026-001)
- Scope: `api|config`
- Affected lifecycle tag: `transitional`
- Owner: `@jcsvwinston`

## Summary

`pkg/mail` historically shipped three built-in providers: `noop` (development), `smtp` (protocol-universal), and `sendgrid` (vendor-specific HTTP API). The built-in `sendgrid` driver is removed in this release.

The framework's intent is to ship only protocol-universal senders in-tree. Provider-specific senders (SendGrid, Mailgun, AWS SES, Postmark, Resend, …) close the framework to one vendor by default and bloat the import graph with HTTP clients that most deployments do not exercise. The external-plugin discovery path (`nucleus-plugin-<provider>` on `PATH`, capability `mail.send`) already supports arbitrary providers and is the canonical extension point. SendGrid was the only built-in vendor and its removal closes the asymmetry.

## Affected Surfaces

- `pkg/mail/sendgrid.go` — removed entirely (constructor `newSendGridSender`, the HTTP request shape, the per-provider tests).
- `pkg/mail.Config.SendGridAPIKey` and `pkg/mail.Config.SendGridEndpoint` fields — removed.
- `pkg/app.Config.SendGridAPIKey` and `pkg/app.Config.SendGridEndpoint` fields plus the `sendgrid_api_key` and `sendgrid_endpoint` `koanf` keys — removed.
- `pkg/admin.PanelConfig.SendGridEndpoint` field — removed.
- `pkg/admin/runtime_email.go` `emailRuntimeSnapshot.SendGridEndpoint` JSON field and the per-driver case — removed; external drivers now share a single "configured" path.
- `pkg/mail.RegisterProvider("sendgrid", …)` registration in `init()` — removed.
- `internal/cli/health.go` per-driver health case for `sendgrid` — removed; external drivers fall to the default branch.
- `internal/cli/sendtestemail.go` `summariseConfig` `sendgrid` case — removed; falls to `plugin=nucleus-plugin-<driver>`.
- Contract baseline `contracts/baseline/config_key_patterns.txt` — the two `sendgrid_*` entries dropped, baseline rebaselined.

## Migration Path

- Replacement: install `nucleus-plugin-sendgrid` on `PATH` and set `mail_driver: sendgrid` in `nucleus.yml`. The framework discovers the binary via the existing external-sender path (`pkg/mail/external.go`). The `mail.send` capability contract is documented in [`docs/reference/PLUGIN_SDK.md`](../reference/PLUGIN_SDK.md); a runnable reference plugin implementation returns with v0.9.X (ADR-010 Phase 4).
- Behavior differences: the plugin owns the HTTP client, retry policy, and credential handling. The framework no longer reads `sendgrid_api_key` / `sendgrid_endpoint` from `nucleus.yml`; the plugin reads whatever environment variables or config files its own contract documents.
- Required app changes: any code that constructed `mail.Config{SendGridAPIKey: …, SendGridEndpoint: …}` must drop those fields. Any `nucleus.yml` that set `sendgrid_api_key` / `sendgrid_endpoint` must remove them (the loader rejects unknown keys for unrecognised drivers via the plugin handshake — koanf itself ignores unknown keys, but the plugin's own validation will fail loud if its expected env vars are missing).

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-002-sendgrid-builtin-to-plugin.md`
- Detection rule: `rg -n 'SendGridAPIKey|SendGridEndpoint|sendgrid_api_key|sendgrid_endpoint' .` against the consumer's repo plus a check that `mail_driver: sendgrid` in `nucleus.yml` is accompanied by a `nucleus-plugin-sendgrid` binary on `PATH`.
- Suggested rewrite: drop the four field/key occurrences from source and config; install the plugin binary; verify with `nucleus plugin doctor --config nucleus.yml`.

## Validation

- Compatibility tests updated: `yes` — `pkg/mail/mail_test.go` no longer carries `TestSendGridSenderSuccess` / `TestSendGridSenderNon2xx`; the registered-providers test asserts only `noop` and `smtp`.
- Release note updated: `yes` — see `CHANGELOG.md` `Unreleased / Removed` and `Unreleased / Changed`.
- Rollback plan documented: `yes` — pin the framework to a pre-removal commit if the plugin path is not viable for a deployment.

## Timeline

- Announcement date: `2026-05-13`
- Review checkpoint: `2026-05-13` (retroactive single-step lifecycle — pre-v1, no published consumers).
- Removal decision date: `2026-05-13`

## Notes

The removal follows the same retroactive single-step pattern as `DEP-2026-001` (the `goframe-mail-<driver>` / `goframe-plugin-*` discovery prefixes). Pre-v1 single-maintainer repo, no published consumers, no compatibility SLO accrued. The doc is filed for governance trail; the actual removal happens in the same commit.
