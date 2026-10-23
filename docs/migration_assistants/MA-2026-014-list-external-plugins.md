# Migration Assistant: external plugins and commands run unlisted → list them under `plugins`

- ID: `MA-2026-014`
- Pairs with: `docs/deprecations/DEP-2026-014-unlisted-external-plugins.md`
- Severity: `high` — an application that relies on an unlisted plugin does
  not start until the plugin is listed, and an unlisted command is not
  dispatched (before v2.0.0 everything ran, with a WARN line at boot and a
  doctor warning).
- Status: `current`

---

## Scope

Applications whose `mail_driver` names an external plugin
(`nucleus-plugin-<driver>` on `PATH`), and projects that run external
commands (`nucleus <name>` → `nucleus-<name>`), whose configuration does not
list them under `plugins.allowed` / `plugins.commands`. An external
executable runs only when listed (DEP-2026-014).

Out of scope: the built-in mail drivers (`noop`, `smtp`, `memory`) and
providers registered in process with `mail.RegisterProvider`.

## Detection

**Boot — the application refuses the driver before the binary runs:**

```
New mail: mail driver "sendgrid": plugin nucleus-plugin-sendgrid is not allowed: plugins.allowed has no entry for provider "sendgrid" — list it under plugins.allowed with capabilities [mail.send], or choose another mail_driver
```

**CLI — `nucleus <name>` is refused, naming the key and the file:**

```
external command nucleus-lint is not allowed: plugins.commands does not list "lint" — list it under plugins.commands in nucleus.yml (or the file NUCLEUS_CONFIG names) to run it
```

**Doctor — the allowlist check names the plugins on `PATH` no list allows:**

```bash
nucleus plugin doctor --config nucleus.yml
# plugin.allowlist  error  1 external plugin(s) on PATH and no allowlist, so none of them runs; …
```

**Inventory — every plugin and command on `PATH`:**

```bash
nucleus plugin list --config nucleus.yml --json
# .providers[] with source external_generic, and .commands[]
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| `mail_driver: sendgrid`, no `plugins` block | the same, plus `plugins.allowed: [{provider: sendgrid, capabilities: [mail.send]}]` | manual |
| `nucleus lint` runs `nucleus-lint`, no `plugins` block | `plugins.commands: [lint]` | manual |
| a host where no external executable may run | `plugins.allow_external: false` (or `NUCLEUS_PLUGINS__ALLOW_EXTERNAL=false`) | manual |

```yaml
plugins:
  allowed:
    - provider: sendgrid
      capabilities: [mail.send]
  commands: [lint]
```

Check the result before deploying it:

```bash
nucleus plugin list --config nucleus.yml          # refused executables say why
nucleus plugin test --provider sendgrid --config nucleus.yml
```

`nucleus <name>` reads `nucleus.yml` in the working directory, or the file
`NUCLEUS_CONFIG` names — set it when external commands run from another
directory.

## Verification

- The application starts, and `nucleus <name>` runs the listed command.
- `nucleus plugin doctor` reports `plugin.allowlist ok`.
- `nucleus plugin list` shows no `refused` for an executable the project
  uses.
