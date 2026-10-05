# Migration Assistant: external plugins and commands run unlisted → list them under `plugins`

- ID: `MA-2026-014`
- Pairs with: `docs/deprecations/DEP-2026-014-unlisted-external-plugins.md`
- Severity: `low` before v2.0.0 (a WARN line at boot and a doctor warning;
  everything runs as before); `high` at v2.0.0 for applications that still
  rely on an unlisted plugin — its `mail_driver` stops resolving and the
  application stops starting until it is listed.
- Status: `current`

---

## Scope

Applications whose `mail_driver` names an external plugin
(`nucleus-plugin-<driver>` on `PATH`), and projects that run external
commands (`nucleus <name>` → `nucleus-<name>`), whose configuration does not
list them under `plugins.allowed` / `plugins.commands`. From v2.0.0 an
external executable runs only when listed (DEP-2026-014).

Out of scope: the built-in mail drivers (`noop`, `smtp`, `memory`) and
providers registered in process with `mail.RegisterProvider`.

## Detection

**Logs — one WARN line at boot (this release onward):**

```
level=WARN msg="external mail plugin runs without an allowlist" driver=sendgrid … deprecation="DEP-2026-014: from v2.0.0 an external plugin runs only when plugins.allowed lists it"
```

**Doctor — the allowlist check warns:**

```bash
nucleus plugin doctor --config nucleus.yml
# plugin.allowlist  warning  1 external plugin(s) on PATH run without an allowlist; …
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

- The boot log has no `external mail plugin runs without an allowlist` line.
- `nucleus plugin doctor` reports `plugin.allowlist ok`.
- `nucleus plugin list` shows no `refused` for an executable the project
  uses.
