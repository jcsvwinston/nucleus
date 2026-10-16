# Migration Assistant: `pkg/model` → Quark as the data layer

- ID: `MA-2026-024`
- Pairs with: `docs/deprecations/DEP-2026-024-pkg-model-leaves-the-public-api.md`
- Severity: `high` (a rewrite of the model layer; data semantics — versions,
  tenants, `LIKE` escaping — follow Quark's rules afterwards).
- Status: `current`

---

## Scope

Applications that define models with `model.BaseModel` and `model.ModelMeta`,
register them in `app.App.Models` / `Runtime.Models()`, read or write them
through `model.CRUD`, or subscribe to the lifecycle signals `pkg/model`
emits. Tests that build rows with `nucleustest`'s factories over `pkg/model`.

Out of scope: applications already on Quark (`quark.For[T]`); the SQL-first
migrations of `pkg/db`, which stay.

## Detection

```bash
grep -rln 'nucleus/pkg/model"' --include='*.go' .
grep -rn 'Models()\|\.Models\b' --include='*.go' . | grep -v _test.go
staticcheck ./...   # SA1019 on the package
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| `type Article struct { model.BaseModel; … }` with `db:"…"` tags | a Quark model with `quark:"…"` tags, migrated by `quark migrate` or `AutoMigrate` | manual |
| `app.Models.Register(&Article{})` | the registration the starter of the minor that moves the generators writes (a Nucleus module wrapping a `*quark.Client`, as `quark init --with nucleus` generates today) | manual |
| `model.NewCRUD(db, meta, bus).Create(ctx, &a)` and friends | `quark.For[Article](ctx, client).Create(&a)` and friends | manual |
| `bus.On(signals.PostSave, …)` for a model | Quark hooks (`AfterSave`) or Quark's event bus | manual |
| `nucleustest` factories over `pkg/model` | the Quark-backed factories of the minor that moves `nucleustest` | manual |

The generators of a later 1.x minor produce the target form; `nucleus
generate model` on that minor is the reference for a hand migration.

## Verification

```bash
go build ./... && go test ./...
grep -rln 'nucleus/pkg/model"' --include='*.go' . | wc -l   # 0
```

## Rollback

Nothing changes in v1.x; the package keeps working with its markers until
v2.0.0.
