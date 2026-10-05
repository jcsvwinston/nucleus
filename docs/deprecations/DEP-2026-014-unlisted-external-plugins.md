# Deprecation Notice: external plugins and commands the configuration does not list → refused at v2.0.0

- ID: `DEP-2026-014`
- Status: `active`
- Announced in: `Unreleased` (A11 N10, 2026-10-05; the release that carries this change)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-02 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc (QADR-0010). This is
  a **change of a configuration default**, not a symbol removal: no
  `// Deprecated:` marker carries it.
- Scope: `config` and `plugin`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

Nucleus runs external executables found on `PATH`: a capability plugin
`nucleus-plugin-<provider>` for `mail_driver: <provider>` (and for
`nucleus sendtestemail` and the `nucleus plugin` commands), and an external
command `nucleus-<name>` for `nucleus <name>`. Until this release nothing
bounded which ones: any binary with the right name ran, and the
`plugins.*` keys the plugin reference proposed to bound them were refused as
unknown by the strict configuration check (NU-102, measured by the A11
catalog bench as `EX-12`).

This release adds the allowlist, opt-in:

```yaml
plugins:
  allow_external: true          # false: no external plugin or command runs
  allowed:                      # when set: the only capability plugins that run
    - provider: sendgrid
      capabilities: [mail.send]
  commands: [lint]              # when set: the only nucleus-<name> commands dispatched
```

With the block set, an executable it does not allow is refused before it is
executed — not even asked for its capabilities. With nothing set, every
external plugin and command runs, as before.

What changes and when:

- **This release:** nothing set means everything runs, as before. An
  application that selects an external mail plugin it does not list logs,
  once at boot:

  ```
  level=WARN msg="external mail plugin runs without an allowlist" driver=sendgrid binary=/usr/local/bin/nucleus-plugin-sendgrid fix="list it under plugins.allowed: [{provider: sendgrid, capabilities: [mail.send]}]" deprecation="DEP-2026-014: from v2.0.0 an external plugin runs only when plugins.allowed lists it"
  ```

  and `nucleus plugin doctor` reports a warning on `plugin.allowlist` while
  external plugins are on `PATH` and none is listed.
- **v2.0.0:** an absent or empty `plugins.allowed` allows no capability
  plugin, and an absent or empty `plugins.commands` allows no external
  command: an external executable runs only when the configuration lists it.
  `plugins.allow_external: false` keeps refusing everything.

## Affected Surfaces

- Applications whose `mail_driver` names an external plugin
  (`nucleus-plugin-<driver>`) and whose configuration does not list it under
  `plugins.allowed`.
- `nucleus sendtestemail`, `nucleus plugin doctor`, `nucleus plugin list`
  and `nucleus plugin test` against such a plugin.
- `nucleus <name>` for an external command `nucleus-<name>` the
  configuration does not list under `plugins.commands`.
- Outbox bridges of type `plugin` (added after this notice, A11 N11): a
  bridge whose provider the configuration does not list for its capability
  (`queue.publish` or `webhook.deliver`) runs today with one WARN at boot
  (`outbox: plugin bridge runs an external plugin without an allowlist`),
  and from v2.0.0 stops the application from starting, as an unlisted mail
  plugin does.
- Not affected: the built-in mail drivers (`noop`, `smtp`, `memory`) and
  providers registered in process with `mail.RegisterProvider`.
- No Go API is removed or changes shape. The additions are
  `app.Config.Plugins`, `app.PluginsConfig`, `app.PluginAllowance`,
  `mail.Config.Plugins`, `plugins.Policy`, `plugins.Allowance`,
  `plugins.RefusedError`, `plugins.DiscoverAllowed` and
  `plugins.Descriptor.Refused` (and, for plugin authors, `plugins.Serve`,
  `plugins.ServeIO`, `plugins.Plugin`, `plugins.Fail`, `plugins.Failure`).

## Migration Path

- Replacement: list the external plugins and commands the application uses.

  ```yaml
  plugins:
    allowed:
      - provider: sendgrid          # the mail_driver value
        capabilities: [mail.send]
    commands: [lint]                # each nucleus-<name> the project runs
  ```

- Behavior differences: with a list set, an unlisted plugin fails the
  application's boot (`New mail: mail driver "x": plugin nucleus-plugin-x is
  not allowed: plugins.allowed has no entry for provider "x" …`), and an
  unlisted command fails `nucleus <name>` with exit 1 naming
  `plugins.commands`, without executing either. `nucleus plugin list`
  shows each refused executable and why.
- Required app changes: one block in the configuration. The CLI's
  dispatcher reads `nucleus.yml` in the working directory (or the file
  `NUCLEUS_CONFIG` names); a project that runs external commands from
  elsewhere sets `NUCLEUS_CONFIG`.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-014-list-external-plugins.md`
- Detection rule: the boot log line `external mail plugin runs without an
  allowlist` carrying `deprecation="DEP-2026-014…"`, or `nucleus plugin
  doctor` warning on `plugin.allowlist`.
- Suggested rewrite: manual — add the `plugins` block; `nucleus plugin list
  --json` names every plugin and command on `PATH`.

## Validation

- Compatibility tests updated: `yes` — `pkg/app` pins that an application
  with an external mail plugin and no block still starts and logs exactly
  one WARN naming the fix and this notice, that a listed plugin starts
  without it, and that an unlisted one is refused without being executed;
  `pkg/plugins` pins the policy and that a refused binary is never probed;
  `internal/cli` pins the dispatcher (an unlisted command does not run, no
  block runs as before, a declared block that does not load refuses). The
  catalog bench measures it end to end (`EX-12`). The additions are listed
  in `contracts/baseline/api_exported_symbols.txt` and
  `contracts/baseline/config_key_patterns.txt` (additions only).
- Release note updated: through the squash commit, which feeds the
  release-please notes.
- Rollback plan documented: `yes` — before v2.0.0, removing the block
  restores the previous behaviour; after it, listing the executable does.

## Timeline

- Announcement date: `2026-10-05`
- Review checkpoint: the set that certifies the release carrying this notice.
- Behaviour flip decision date: the `v2.0.0` major.

## Notes

The umbrella guard `scripts/check_deprecations.sh` reads `// Deprecated:`
markers on Go symbols. This notice changes a default, not a symbol, so the
guard does not see it. Its version and date live in this notice, and in the
suite index of the umbrella's deprecation policy once a set publishes it,
until the guard learns to read notices that have no marker.
