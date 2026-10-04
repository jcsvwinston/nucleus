# Migration Assistant: `Context.Get` / `Context.Set` → typed request values

- ID: `MA-2026-011`
- Pairs with: `docs/deprecations/DEP-2026-011-context-get-set-typed-values.md`
- Severity: `low` — each call site changes shape, and a value that fed a
  template needs to be passed to it explicitly; nothing changes at run time
  until the code is rewritten. The deprecated methods keep working until
  `v2.0.0`.
- Status: `current`

## Scope

Handlers that call `Set(key, value)` or `Get(key)` on a `*nucleus.Context`.
Not affected: the router context's own `Set`, `Data` and `BindData`.

## Detection

```bash
# Candidate call sites (a handler's context is usually named c or ctx).
rg -n --type go '\b(c|ctx)\.(Get|Set)\("' .

# Exact list, once the application builds against this release:
staticcheck -checks SA1019 ./... 2>&1 | grep -E 'Context\.(Get|Set)'
```

A call site is impacted when the receiver is a `*nucleus.Context`.

## Rewrite Plan

| Surface (before) | Surface (after) | Note |
|------------------|-----------------|------|
| `c.Set("tenant", t)` | `nucleus.SetValue(c, CurrentTenant, t)` | `var CurrentTenant = nucleus.NewKey[Tenant]("tenant")`, once, at package level |
| `t, _ := c.Get("tenant").(Tenant)` | `t, ok := nucleus.Value(c, CurrentTenant)` | typed; `ok` is false when nothing was stored |
| a middleware storing under a string key in the request context | `r.WithContext(CurrentTenant.WithValue(r.Context(), t))` | read in the handler with `nucleus.Value` |
| `c.Set("title", "Blog")` before `c.Render(...)` | `c.Render(http.StatusOK, "blog/index.html", map[string]interface{}{"title": "Blog"})` | or `c.BindData(map[string]interface{}{"title": "Blog"})` |

Automatic rewrite candidates:

- none: each string key becomes a typed variable, which needs its type
  named by hand.

Manual steps:

- declare one `NewKey[T]` per string key, in the package that owns the
  value, and share the variable with the readers;
- for every `Set` whose value a template reads, pass it to `Render` or
  `BindData` instead.

## Verification

```bash
go build ./...
go test ./...
staticcheck -checks SA1019 ./... 2>&1 | grep -E 'Context\.(Get|Set)' && exit 1 || true
```

## Rollback

Both forms coexist until `v2.0.0`; reverting a call site restores the
previous behaviour. No data or configuration is involved.

## Compatibility Notes

Additive: the typed form was added beside the methods, which keep their
behaviour until the major.
