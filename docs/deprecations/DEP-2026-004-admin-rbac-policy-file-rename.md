# Deprecation Notice: `admin_rbac_policy_file` config key renamed to `rbac_policy_file`

- ID: `DEP-2026-004`
- Status: `active`
- Announced in: `Unreleased` (ADR-019 Slice 2 / orbit extraction)
- Earliest removal: `v0.12.0` (≥ 2 minor cycles after announcement, per `docs/governance/COMPATIBILITY_SLO.md`)
- Scope: `config`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

As part of the ADR-019 admin extraction ("orbit"), the in-core admin panel is
removed from `pkg/app`. Most `admin_*` config keys are **hard-removed** (they
have no meaning without the in-core panel; users adopting orbit pass the
equivalent values via `orbit.Config` — see the migration assistant). One key,
however, configures a **framework-level concern** (the Casbin RBAC enforcer)
rather than the admin panel itself: `admin_rbac_policy_file`. That key is
therefore **renamed** rather than removed, because the enforcer is always active
regardless of whether orbit is mounted.

The new canonical key is `rbac_policy_file` (YAML) / `Config.RBACPolicyFile`
(Go). The old key is **retained as a deprecated alias**: when `rbac_policy_file`
is empty and `admin_rbac_policy_file` is set, `pkg/app` uses the old value and
emits a one-time startup `WARN` through the application logger:

```
config: admin_rbac_policy_file is deprecated, use rbac_policy_file;
the old key will be removed in a future release
```

The alias is implemented in `resolveRBACPolicyFile` (`pkg/app/app.go`) via a
`sync.Once`-guarded logger call, so the message appears exactly once per
process regardless of how many times the resolver is called.

## Affected Surfaces

- `admin_rbac_policy_file` — YAML / environment-variable config key. Was
  `Config.AdminRBACPolicyFile string` (`koanf:"admin_rbac_policy_file"`),
  located in `pkg/app/config.go`.
- `rbac_policy_file` — new canonical key. `Config.RBACPolicyFile string`
  (`koanf:"rbac_policy_file"`), same file.

The Go struct fields are **not removed**: `Config.AdminRBACPolicyFile` is kept
in `pkg/app.Config` as a deprecated field to allow the koanf unmarshalling alias
to function. It will be removed in the same release that drops the YAML key.

## Migration Path

- Replacement: rename `admin_rbac_policy_file` → `rbac_policy_file` in
  `nucleus.yml` (or any config file / environment variable).
- Behavior differences: none. The value is passed verbatim to the Casbin file
  adapter via `authz.New`. The enforcer behaviour, default-deny policy (ADR-004),
  and policy file format (see DEP-2026-003 for the 3-column → 4-column migration)
  are unchanged.
- Required app changes: a one-line YAML key rename. No Go source changes are
  required if the key was set only via config file; if it was set via the
  `Config` struct literal (`Config{AdminRBACPolicyFile: "..."}`) the field name
  must also be updated to `Config{RBACPolicyFile: "..."}`.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-004-admin-rbac-policy-file-to-rbac-policy-file.md`
- Detection rule: presence of the key `admin_rbac_policy_file` in any YAML/TOML
  config file, or use of `Config.AdminRBACPolicyFile` in Go source, or startup
  `WARN` matching `"admin_rbac_policy_file is deprecated"` in application logs.
- Suggested rewrite: mechanical one-line rename (YAML) / one-field rename (Go).
  The MA also covers adopting the orbit module to replace the removed in-core
  admin panel.

## Validation

- Compatibility tests updated: `yes` — `contracts/baseline/config_key_patterns.txt`
  now contains `rbac_policy_file`; `admin_rbac_policy_file` is absent from the
  baseline (it is an alias, not a primary contract surface).
- Release note updated: `yes` — see `CHANGELOG.md` `Unreleased / Deprecated`.
- Rollback plan documented: `yes` — the alias means no-op rollback for config;
  see MA for Go source rollback.

## Timeline

- Announcement date: `2026-06-21`
- Review checkpoint: at `v0.11.0` release prep — confirm alias still operational
  and measure how many known consumers (fleetdesk, orbit) still reference the old
  key.
- Removal decision date: `v0.12.0` release prep — proceed to removal if zero
  known usages of the deprecated alias remain.
