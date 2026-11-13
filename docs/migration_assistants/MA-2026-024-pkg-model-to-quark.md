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
| `app.Models.Register(&Article{})` | nothing: a Quark model registers nowhere. The repository `generate resource` writes derives its client in the module's `OnStart` (`quark.NewWithDB(driver, rt.DB())`) and reads the model through `quark.For[T]` | manual |
| `model.NewCRUD(db, meta, bus).Create(ctx, &a)` and friends | `quark.For[Article](ctx, client).Create(&a)` and friends | manual |
| `bus.On(signals.PostSave, …)` for a model | Quark hooks (`AfterSave`) or Quark's event bus | manual |
| `app.Models.Register(&Article{}, model.ModelConfig{…})` and the `admin` struct tag — what the Orbit panel lists, searches, filters and hides | the same declarations as options of `quarkdatasource.Register[Article](ds, …)` (orbit `quarkdatasource` v1.14+): see the table below | manual |
| `nucleustest` factories over `pkg/model` | the same call: `nucleustest.Make[T]` persists a Quark model through Quark, with the factory of the `pkg/nucleustest/quarkmake` module installed once per test server — `srv.UseFactory(quarkmake.New(client, quarkmake.Model[T]()))` (NU-137, D12: the framework's `go.mod` requires no Quark); a registered `pkg/model` model keeps the registry path until v2.0.0 | `go get github.com/jcsvwinston/nucleus/pkg/nucleustest/quarkmake` in the test module and install the factory |

What `ModelConfig` and the `admin` tag said about a model's fields is said
to the Orbit panel at `quarkdatasource.Register`, in Go, with the same
words (Quark reads none of it: a struct tag that said "list" would be a UI
decision in the data layer):

| `model.ModelConfig` / `admin:"…"` | `quarkdatasource` option |
|---|---|
| `ListFields` / `admin:"list"` | `ListFields("email", "role")` — none named keeps every column |
| `SearchFields` / `admin:"search"` | `SearchFields("email")` — none named keeps every string column |
| `Filters` / `admin:"filter"` | `Filters("role")` — none named keeps every scalar column |
| `ExcludeFields` / `admin:"exclude"` | `Exclude("notes")` — and a column whose name looks like a credential (`password`, `secret`, `token`, `*_hash`, …) is excluded without being named; `ShowSensitive("password_hash")` keeps it |
| `ReadOnly` | `ReadOnly()` |
| `admin:"readonly"` | `ReadOnlyFields("email")` |
| `FieldLabels` / `admin:"label:…"` | `Label("display_name", "Name")` |
| `admin:"choices:a\|A;b\|B"` | `Choices("role", datasource.Choice{Value: "a", Label: "A"}, …)` |
| `Icon` | `Icon("users")` |
| `OrderBy` | `OrderBy("created_at desc")` |
| `PageSize` | `PageSize(50)` |

A field is named by its column or its Go name. An option that names no
field of the model, or contradicts another, is an error from `Register`
rather than a declaration that silently does nothing. `RejectClientPK`,
`DatabaseAlias` and the lifecycle hooks are not presentation and have their
own rows above.

The generators produce the target form since NU-137: `nucleus generate
model <Name>` writes the reference struct for a hand migration (Quark tags,
`TableName()` pinned to the table), and `generate resource` / `startapp`
write the repository that persists it on Quark.

## Verification

```bash
go build ./... && go test ./...
grep -rln 'nucleus/pkg/model"' --include='*.go' . | wc -l   # 0
```

## Rollback

Nothing changes in v1.x; the package keeps working with its markers until
v2.0.0.
