# Deprecation Notice: 3-column Casbin policy CSV files

- ID: `DEP-2026-003`
- Status: `active`
- Announced in: `v0.7.0`
- Earliest removal: `v2.0.0`, no earlier than 2027-01-17 — the notice closes as `removed` at the major (owner's decision D2, 2026-10-16). Measured the same day: the legacy rows no longer go inert, they are refused at load (see «Measured on 2026-10-16»).
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

## Measured on 2026-10-16

The inert phase this notice describes is over. With casbin v2.135.0 (the
version the framework pins) a policy file that carries a three-field `p`
row does not load: `casbin.NewEnforcer` returns `invalid policy rule size:
expected 4, got 3`, and `authz.New` fails with it. The rows do not grant and
do not deny because the enforcer never comes up. The pinned test is
`TestLegacyThreeColumnRows_RefusedWithTheMigratorNamed`
(`pkg/authz/legacy_three_column_v1_test.go`).

What this release adds is the way out in the error: when Casbin refuses a
row for its size, `authz.New` and `authz.NewFromModel` say that the file is
in the legacy form, that every `p` row needs `allow` or `deny`, that
`authz.MigrateCSVPolicyFile` rewrites it, and name this notice.

What `v2.0.0` does: nothing in code. The status moves to `removed`, because
no version of the framework reads the legacy form any more and the window
the suite's policy asks for (90 days from the announcement that carries a
date) has run. The migrator stays.

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
- Dated: `2026-10-16` (D2 of the suite's 2.0 contract, `quantum/docs/planes/quantum-2.0.md` §3.3). The notice was the only `active` one without a date, which the suite's deprecation policy does not admit.
- Review checkpoint: `2027-01-17`, when the window opens.
- Removal decision date: the `v2.0.0` major, where the status becomes `removed`. The migrator is the supported recovery path before and after.

## Notes

This DEP is filed retroactively for the PR #41 behaviour change. The original PR documented the policy-shape change in its commit body and CHANGELOG entry, but no paired DEP/MA was issued at the time. The `pkg/authz/enforcer.go:31` godoc has carried a `See MigrateCSVPolicyFile for a one-shot migration helper` comment since PR #41 — the helper now exists.

This DEP is also the first in the repo with status `active` rather than the retroactive `removed` pattern of DEP-2026-001 and DEP-2026-002. The legacy format does not throw; it goes inert. There is no parse-time signal, only a runtime one: requests that used to be allowed now get `403`. That is what makes the migrator load-bearing.
