---
sidebar_position: 3
title: Start a suite app
covers:
  - pkg/nucleus.New
  - pkg/nucleus.AppBuilder.FromConfigFile
  - pkg/nucleus.AppBuilder.Mount
  - pkg/nucleus.AppBuilder.Start
  - pkg/nucleus.Module
  - pkg/nucleus.ModuleSpec
  - pkg/nucleus.PolicyRule
  - pkg/nucleus.Runtime
config_keys:
  - csrf_exempt_paths
  - session_cookie_secure
---

# Start a suite app

One command writes the Quantum suite wired together — a Nucleus application
whose domain runs on the **Quark** ORM, with the **Orbit** admin panel mounted
on top and both bridges between them — and `go run .` boots it. Start here
if you want the whole suite on the first screen; the
[Quickstart](quickstart.md) starts from the empty framework skeleton instead.

## 1 — Scaffold

```bash
nucleus new store --template suite
cd store
```

`--template suite` implies `--with orbit,quark,quarkbridge,quarkdatasource`:
the four sibling modules are fetched from the module proxy at their
published tags (`go get`, then `go mod tidy`), together with the driver
modules for `--db` (`sqlite` by default; `postgres`, `mysql`, `sqlserver`
and `oracle` move both products to that engine). The project builds as
written. `--offline` skips the network and prints the exact `go get … &&
go mod tidy` line to run later.

What is on disk:

```
.dockerignore
.gitignore
Dockerfile
README.md
go.mod
main.go
migrations/.gitkeep
nucleus.yml
rbac_policy.csv
shop/models.go
shop/module.go
shop/module_test.go
```

- `main.go` opens the Quark client on the same database `nucleus.yml`
  names, migrates and seeds the `shop` schema, backs Orbit's Data Studio
  with the Quark models (`quarkdatasource`), and mounts the `shop` module
  and the admin panel.
- `shop/` is the worked domain: two Quark models (`Author`, `Article` with a
  `belongs_to`), a JSON API, and a test that boots the module in-process.
- `Dockerfile` and `.dockerignore` build the project into a container image:
  a multi-stage build, a static binary, a distroless runtime base pinned by
  digest, and a process that runs as uid 65532. `nucleus doctor --check
  image` reads that file back — see
  [Deployment](../operations/deployment.md#container-image).
- `nucleus.yml` and `rbac_policy.csv` are the framework's own
  configuration; the scaffold sets `session_cookie_secure: false` so the
  admin login works on `http://localhost` (remove it behind TLS).

## 2 — Run and try

```bash
go run .
```

```bash
curl -s localhost:8080/api/articles
curl -s -X POST localhost:8080/api/articles \
    -H 'Content-Type: application/json' \
    -d '{"author_id":1,"title":"probe","body":"live feed"}'
```

The first call lists the seeded article; the second creates one and answers
`201` — post the same title twice and the third answers `409`, because the
Quark driver module that recognises the engine's duplicate-key error is
linked. A path nothing serves answers `404`.

Then open **http://localhost:8080/admin** — user `admin`, password
`quickstart` unless `ADMIN_BOOTSTRAP_PASSWORD` was set before the first boot
(a development credential; set the variable before the app faces anyone but
you). The **live view** shows every Quark statement the API ran, correlated
to its request; **Data Studio** browses and edits `Author` and `Article`.

`go test ./...` runs the shop module's test: the same boot path `main.go`
takes, driven over HTTP, duplicate-title probe included.

## What the scaffold decided for you

**The API is open for development, and it says so.** The full application
runs the default-deny enforcer, so every route needs a policy row. The
`shop` module carries its own rows instead of editing `rbac_policy.csv`:

```go
import "github.com/jcsvwinston/nucleus/pkg/nucleus"

Policies: []nucleus.PolicyRule{
    {Subject: "anonymous", Object: "/api/authors", Action: "read"},
    {Subject: "anonymous", Object: "/api/articles", Action: "read"},
    {Subject: "anonymous", Object: "/api/articles", Action: "create"},
},
CSRFExempt: []string{"/api/"},
```

The `create` row is what lets the `curl -X POST` above land without a
login; a module written with `nucleus generate module` opens reads only.
Scope it to a role before the app faces a network — an operator deny in
`rbac_policy.csv` always overrides. The admin panel needs no rows: Orbit
enforces its own session login under `/admin`.

**The JSON API is exempt from CSRF; the admin login is not.** `/api/` takes
a header token, which a cross-site form cannot forge, so the module exempts
it and `curl` works as written. `/admin/login` is a browser form: a browser
passes the origin check through `Sec-Fetch-Site`, a bare `curl` answers
`419` — send `-H 'Sec-Fetch-Site: same-origin'` to script it.

**Versions.** The suite tags Nucleus before Orbit in every release. Right
after a Nucleus release, the `orbit` tag on the proxy may still pin the
previous Nucleus minor; `go get` keeps the higher of the two and the next
Orbit tag closes the gap. Nothing in the scaffold pins a sibling version —
`go.mod` is what the proxy resolved on the day.

## The same thing, piece by piece

`--with` also works on the other templates:

```bash
nucleus new blog --with orbit            # mvc + the admin panel under /admin
nucleus new svc --template api --with quark   # fetch the ORM and its driver, mount nothing
```

`--with orbit` fetches the panel and writes the `Mount(orbit.Module(orbit.Config{...}))`
call into `main.go` with the same bootstrap credentials. `--with quark`
fetches the ORM and its driver module for `--db` without writing any code:
nothing in the mvc or api scaffold imports them, so the scaffold runs their
`go get` after `go mod tidy` (which would drop them) and `go.mod` keeps them
as indirect requires — `nucleus generate module <name> --data quark` then
imports them and builds with the versions the scaffold resolved, rather
than whatever the proxy serves that day. The two bridges are only useful
with both products present; only the suite template wires all four.

## The example is the template

`examples/showcase_demo` in the Nucleus repository is the committed output
of `nucleus new showcase_demo --template suite --port 8091`, and a test
fails when the two differ. If this page and that example ever disagree,
the example is right — and the scaffold writes exactly it.

## Next

- Add a feature: `nucleus generate module notes --mount --data quark`
  writes a slice on the same ORM and mounts it.
- [Using Quark with Nucleus](../features/using-quark.md) — the ORM inside a
  module, the bridges, and when to prefer the framework's SQL-first layer.
- [Project structure](project-structure.md) — where things go as the
  application grows.
