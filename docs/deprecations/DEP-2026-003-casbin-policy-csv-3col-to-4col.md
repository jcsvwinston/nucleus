# Deprecation Notice: 3-column Casbin policy CSV files

- ID: `DEP-2026-003`
- Status: `active`
- Announced in: `v0.7.0`
- Earliest removal: not scheduled — legacy 3-column rows do not parse-error, they go inert. The migration is operationally mandatory but compile-compatible.
- Scope: `config` (RBAC policy file format)
- Affected lifecycle tag: `transitional` (RBAC model under default deny-override; ADR-004)
- Owner: `@jcsvwinston`

## Summary

PR #41 replaced the default Casbin RBAC model with a deny-override variant: `e = some(where (p.eft == allow)) && !some(where (p.eft == deny))`. The new model adds a fourth policy argument `eft` (effect: `allow` or `deny`). Programmatic callers were unaffected because `Enforcer.AddPolicy` now auto-stamps `allow` and `Enforcer.Deny` auto-stamps `deny`.

CSV policy files loaded via the Casbin `fileadapter`, however, are read literally. Rows in the legacy 3-argument form

```
p, alice, /data, read
```

are still parsed, but they no longer match either branch of the effect formula (`p.eft == allow` is false, `p.eft == deny` is false). Casbin treats them as **inert**: they neither grant nor deny. With default-deny mounted by `App.New` per ADR-004, an inert allow row means the request is denied by the absence-of-allow clause, **silently breaking any application that relied on the legacy 3-argument form to grant access**.

This notice formalizes the transition and points operators at the migration helper.

## Affected Surfaces

- `admin_rbac.csv` and any path referenced by `Config.AdminRBACPolicyFile` (`pkg/app/config.go:100`).
- The default lookup paths used when `AdminRBACPolicyFile` is empty: `admin_rbac.csv`, `config/admin_rbac.csv`, `rbac/admin_rbac.csv` (`pkg/app/app.go:1441`).
- Any custom CSV passed to `authz.New(logger, policyPath)` or `authz.NewFromModel(model, logger, policyPath)` in user code.

Programmatic policy construction (`Enforcer.AddPolicy`, `Enforcer.Deny`, `Enforcer.AddRole`, `Enforcer.AddGroupingPolicy`) is unaffected. Grouping policies (`g, user, role`) are unaffected — the default model declares `g = _, _` with no effect column.

## Migration Path

- Replacement: every `p, …` row must carry a fourth field, `allow` or `deny`. Grouping rows (`g, …`) are unchanged.
- Behavior differences: post-migration, the policy enforces deny-override semantics. An explicit `deny` row now beats any matching `allow` row, including allows inherited via role grouping. This is the intended primitive for "block this user even though their role normally has access".
- Required app changes: rewrite the policy CSV. The framework ships `authz.MigrateCSVPolicyFile` (added in this release) as the canonical one-shot migrator. It is idempotent — running it against an already-migrated file does nothing.

Manual rewrite is also viable for hand-curated policies. Example:

```diff
- p, alice, /data, read
+ p, alice, /data, read, allow
- p, bob,   /data, write
+ p, bob,   /data, write, allow
  g, alice, admin
```

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-003-casbin-policy-csv-3col-to-4col.md`
- Detection rule: parse the CSV with the helper; any returned report with `PolicyLinesUpgraded > 0` indicates the file is in the legacy form.
- Suggested rewrite: call `authz.MigrateCSVPolicyFile(path, "allow")` from a one-shot Go program or use a manual rewrite. Default effect is `allow` because every existing 3-argument row was conceptually an allow rule under the legacy single-effect model.

## Validation

- Compatibility tests updated: `yes` — `pkg/authz/migrate_test.go` covers upgrade, idempotency, default-deny effect, invalid-effect rejection, mixed-file handling, and round-trip loadability via the Casbin enforcer.
- Release note updated: `yes` — see `CHANGELOG.md` `Unreleased / Added` for the migrator and `Unreleased / Changed` for the operational expectation.
- Rollback plan documented: `yes` — the helper rewrites in place and the previous CSV is overwritten atomically. Operators who want a backup should `cp admin_rbac.csv admin_rbac.csv.bak` before running the migrator. Reverting requires removing the `eft` column from every `p` row.

## Timeline

- Announcement date: `2026-05-14`
- Review checkpoint: not scheduled — this is a config-file format transition, not a code surface removal. The model itself is `transitional` because the deny-override formula is still being validated in operational use.
- Removal decision date: not applicable — the legacy 3-column form is not "removed", it is rendered inert by the model change shipped in PR #41. The migrator is the supported recovery path.

## Notes

This DEP is filed retroactively for the PR #41 behaviour change. The original PR documented the policy-shape change in its commit body and CHANGELOG entry, but no paired DEP/MA was issued at the time. The `pkg/authz/enforcer.go:31` godoc has carried a `See MigrateCSVPolicyFile for a one-shot migration helper` comment since PR #41 — the helper now exists.

This DEP is also the first in the repo with status `active` rather than the retroactive `removed` pattern of DEP-2026-001 and DEP-2026-002. The legacy format does not throw; it goes inert. There is no parse-time signal, only a runtime one: requests that used to be allowed now get `403`. That is what makes the migrator load-bearing.
