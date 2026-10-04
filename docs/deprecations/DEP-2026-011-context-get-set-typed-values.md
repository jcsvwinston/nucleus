# Deprecation Notice: `Context.Get` / `Context.Set` → typed request values

- ID: `DEP-2026-011`
- Status: `active`
- Announced in: `Unreleased` (the minor after `v1.30.1`; A10 S9, 2026-10-04)
- Earliest removal: `v2.0.0`, no earlier than 2027-01-02 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc
- Scope: `api`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

`nucleus.Context.Set(key string, value interface{})` and
`nucleus.Context.Get(key string) interface{}` store and read request-scoped
values under a string key, with a type assertion at every read: two packages
that choose the same key overwrite each other, and a value stored as one type
and read as another fails at run time. It is the service-locator shape the
arc that replaces it names.

The typed form exists beside it from this release:

- `nucleus.NewKey[T](name) Key[T]` — one key per value, created once at
  package level; two keys are the same only if they are the same variable;
- `nucleus.SetValue(c, key, v)` and `nucleus.Value(c, key) (T, bool)` in a
  handler;
- `key.WithValue(ctx, v)` and `key.FromContext(ctx)` in an `http.Handler`
  middleware, on the request's context.

`Set` is also how a handler adds data to the template `Render` executes:
values stored with it are merged into the template's data. That use has its
own replacement: pass the data to `Render`, or call `BindData` (on the
embedded router context) before rendering.

## Affected Surfaces

- `nucleus.Context.Set` → `nucleus.SetValue` with a `nucleus.Key[T]` for a
  request value; `Render`'s data map or `BindData` for template data.
- `nucleus.Context.Get` → `nucleus.Value` with the key the value was stored
  under.
- Not deprecated: `router.Context.Set`, `router.Context.Data` and
  `router.Context.BindData` — the template binding store of the router
  context stays.

## Migration Path

- Replacement: as above.
- Behavior differences: a typed value lives in the request's
  `context.Context`, not in the template binding map, so `Render` does not
  see it — pass template data explicitly. A value set in a handler with
  `SetValue` is visible to the rest of that handler chain and to code handed
  `c` or `c.Request`; an `http.Handler` middleware that wraps the handler
  holds its own request and does not see it (it sets values, the handler
  reads them).
- Required app changes: replace each string key with a package-level
  `NewKey[T]` variable; replace `c.Set(k, v)` with `nucleus.SetValue(c, key, v)`
  and `c.Get(k).(T)` with `v, ok := nucleus.Value(c, key)`.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-011-context-get-set-to-typed-values.md`
- Detection rule: calls to `Get(` / `Set(` on a `*nucleus.Context`; gopls
  and `staticcheck` (SA1019) flag them once the application is on this
  release.
- Suggested rewrite: manual — each key becomes a typed variable.

## Validation

- Compatibility tests updated: `yes` — the additions are listed in
  `contracts/baseline/api_exported_symbols.txt` (additions only); `Get` and
  `Set` keep their behaviour and their tests.
- Release note updated: through the squash commit, which feeds the
  release-please notes.
- Rollback plan documented: `yes` — both forms coexist until `v2.0.0`.

## Timeline

- Announcement date: `2026-10-04`
- Review checkpoint: the set that certifies the release carrying this notice
  — confirm the deprecation diagnostics fire and no first-party code (the
  framework, the `nucleus new` templates, orbit, quark) calls `Get` or `Set`.
- Removal decision date: the `v2.0.0` major.

## Notes

Measured when the notice was written: no non-test code in this repository
calls `nucleus.Context.Get` or `Set`, the templates `nucleus new` writes do
not, and neither do orbit (all of its modules type-checked against this
change with both methods removed) or quark.
