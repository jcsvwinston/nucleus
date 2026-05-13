# Migration Assistant: Built-in `sendgrid` → `nucleus-plugin-sendgrid`

- ID: `MA-2026-002`
- Pairs with: `docs/deprecations/DEP-2026-002-builtin-sendgrid-provider.md`
- Severity: `medium`
- Status: `current` (paired deprecation is `removed`)

## Scope

Applications that have any of the following in their tree:

- A `nucleus.yml` (or env-var override) carrying `mail_driver: sendgrid`, `sendgrid_api_key`, or `sendgrid_endpoint`.
- Go source that constructs `mail.Config{SendGridAPIKey: …, SendGridEndpoint: …}` or reads `app.Config.SendGridAPIKey` / `SendGridEndpoint`.
- Admin telemetry consumers parsing `sendgrid_endpoint` from `pkg/admin`'s email runtime snapshot JSON.

This MA covers the move from the in-tree SendGrid sender to an out-of-process plugin discovered via the existing `nucleus-plugin-<provider>` mechanism. The framework's plugin protocol is unchanged — only the *built-in* provider is gone.

## Detection

Run from the consumer repo root:

```
# 1. Source-level references to the removed fields.
rg -n 'SendGridAPIKey|SendGridEndpoint' .

# 2. Config-file references to the removed koanf keys.
rg -n 'sendgrid_api_key|sendgrid_endpoint' .

# 3. Plugin binary on PATH? Required if mail_driver is still sendgrid.
ls $(echo "$PATH" | tr ':' '\n') 2>/dev/null \
  | grep -E '^nucleus-plugin-sendgrid$' || true

# 4. Framework's own view post-migration.
nucleus mailproviders --config nucleus.yml
nucleus plugin doctor --config nucleus.yml
nucleus sendtestemail --config nucleus.yml --to dev@example.com --dry-run
```

A consumer is impacted if (1) or (2) returns anything, OR (3) is empty while the config still names `sendgrid`.

## Rewrite Plan

Mechanical changes plus one new dependency (the plugin binary).

| Surface (before)                                                              | Surface (after)                                                              | Note                                                                              |
|--------------------------------------------------------------------------------|------------------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| `mail.Config{Driver: "sendgrid", SendGridAPIKey: x, SendGridEndpoint: y}`     | `mail.Config{Driver: "sendgrid"}`                                            | Plugin reads its own credentials per its documented contract.                     |
| `nucleus.yml: sendgrid_api_key: …`                                            | `nucleus.yml`: remove key. Configure the plugin per its own README.          | Avoid leaking secrets via tracked YAML — use the plugin's env-var path.           |
| `nucleus.yml: sendgrid_endpoint: …`                                           | `nucleus.yml`: remove key.                                                   | Endpoint is a plugin concern; the framework no longer routes around it.            |
| Built-in HTTP call: `pkg/mail/sendgrid.go`                                    | Plugin executable: `nucleus-plugin-sendgrid` on `PATH`                       | The plugin process is invoked per `mail.send` capability.                         |
| Admin runtime snapshot: `sendgrid_endpoint` JSON field                        | Generic `external` provider type, no per-vendor fields                       | Admin no longer models SendGrid specifics; the plugin's own admin route (if any) handles them. |

Automatic rewrite candidates:

- Drop the four field/key occurrences from source and config.
- `git rm` any test fixtures that hard-coded `sendgrid_api_key` / `sendgrid_endpoint`.

Manual steps:

- Install the plugin binary on `PATH`. A reference implementation skeleton lives at `examples/plugins/mail/` — clone it, replace the request shape with SendGrid's `/v3/mail/send`, build the binary `nucleus-plugin-sendgrid`, place it on `PATH`.
- Move the API key out of tracked YAML into the env-var path the plugin reads (typical: `SENDGRID_API_KEY`).
- If you maintained a fork of the in-tree `pkg/mail/sendgrid.go`, port its behaviour into the plugin executable.

## Verification

After migrating:

```
# Fail-fast: the framework must not detect the removed config keys.
rg -n 'sendgrid_api_key|sendgrid_endpoint' . && { echo "remove these keys"; exit 1; } || true

# Plugin discovery green.
nucleus plugin doctor --config nucleus.yml

# Driver advertises through the plugin path.
nucleus mailproviders --config nucleus.yml | grep -q nucleus-plugin-sendgrid

# Dry-run shows the plugin shape.
nucleus sendtestemail --config nucleus.yml --to dev@example.com --dry-run
# Expected: provider=plugin=nucleus-plugin-sendgrid
```

If the consumer also has unit tests asserting `mail.Config.SendGridAPIKey` shape, those need updating to construct the new `mail.Config` (only `Driver`, `SMTPHost`, `SMTPPort`, `SMTPUser`, `SMTPPass` for built-in drivers).

## Rollback

The built-in sender is removed code-side. The only rollback path is pinning the framework to a pre-removal commit (anything ≤ `fcc4f68` on `main`). Practically, no consumer should need this — the plugin path is operationally equivalent and the API key handling is better (no more YAML).

If a deployment must temporarily keep the API key in tracked YAML (CI smoke tests with placeholder credentials, for example), a small wrapper plugin can read `MAIL_SENDGRID_API_KEY` from a CI secret store and source-of-truth that environment instead.

## Compatibility Notes

- Additive-first: installing the plugin binary is non-destructive. The destructive step (dropping the YAML keys + source fields) is safe once verification passes.
- Reproducible in CI: detection + verification steps are scriptable and exit non-zero on regression.
