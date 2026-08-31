# The minimal API

Nucleus freezes over 1 600 symbols under its compatibility contract. You do
not need them.

A complete CRUD application — the `examples/mvc_api` module the quickstart
walks you through — touches the **19 symbols on this page** and nothing else.
Read it if the size of the API surface put you off: everything outside this
list is optional, and you can discover it when a feature asks for it.

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
| `nucleus.Router` | The route registry your module's `Routes` function receives. |
| `nucleus.Handler` | The request handler signature the router mounts. |

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

The full frozen surface is inventoried per package in the
[API contract inventory](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/API_CONTRACT_INVENTORY.md) — auth, storage, jobs,
webhooks, the event bus, multi-tenancy. Each of those is opt-in: nothing on
that list is required to reach a running, authorized, migrated CRUD app.
