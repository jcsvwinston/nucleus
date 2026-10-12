# The minimal API

Nucleus freezes more than 2 300 symbols under its compatibility contract.
You do not need them.

A complete CRUD application — the `notes` module the quickstart walks you
through — touches the **19 symbols on this page** and nothing else.
Read it if the size of the API surface put you off: everything outside this
list is optional, and you can discover it when a feature asks for it.

The count is a test, not an estimate: it reads the quickstart's listings,
collects every package-level name of the framework they reference — the
methods on those names, such as `Mount` or `c.JSON`, come with them — plus
the interface each REST verb they select requires, and checks that this page
lists exactly that set.

## Booting (2 symbols)

| Symbol | One sentence |
|---|---|
| `nucleus.New` | Starts the fluent builder; chain config, mounts and options off it. |
| `nucleus.Runtime` | What your module's hooks receive: the DB handle, logger and framework services. |

## Defining a module (4 symbols)

| Symbol | One sentence |
|---|---|
| `nucleus.Module` | The generic module definition — name, models, routes, hooks — you `Build()` into a spec. |
| `nucleus.ModuleSpec` | The built, mountable form of a module; what `Mount(...)` accepts. |
| `nucleus.PolicyRule` | One access row the module carries for its own routes, so mounting it needs no policy-file edit. |
| `nucleus.Router` | The route registry your module's `Routes` function receives. |

## REST resources (12 symbols)

| Symbol | One sentence |
|---|---|
| `nucleus.Context` | Per-request context: params, decoding, responses. |
| `nucleus.Methods` | Selects which of the five REST verbs a `Resource` exposes. |
| `nucleus.Index` / `nucleus.Indexer` | List endpoint: the verb selector and the interface your controller implements. |
| `nucleus.Show` / `nucleus.Shower` | Fetch-one endpoint, same pair. |
| `nucleus.Create` / `nucleus.Creator` | Create endpoint, same pair. |
| `nucleus.Update` / `nucleus.Updater` | Update endpoint, same pair. |
| `nucleus.Destroy` / `nucleus.Destroyer` | Delete endpoint, same pair. |

## Models (1 symbol)

| Symbol | One sentence |
|---|---|
| `model.BaseModel` | Embeds `id`/`created_at`/`updated_at`/`deleted_at` so your struct matches the scaffolded migrations. |

## Where the rest lives

The first name past this list you are likely to meet is `nucleus.Handler`,
the signature of a function registered one route at a time with `r.Get` or
`r.Post` — the quickstart's module registers a REST resource instead.

The full frozen surface is inventoried per package in the
[API contract inventory](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/API_CONTRACT_INVENTORY.md) — auth, storage, jobs,
webhooks, the event bus, multi-tenancy. Each of those is opt-in: nothing on
that list is required to reach a running, authorized, migrated CRUD app.
