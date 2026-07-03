# showcase_demo — the Quantum suite, wired together

A Nucleus application whose domain runs on the **Quark** ORM, with the **Orbit**
admin mounted on top and both integration bridges wired (quantum
[QADR-0006](https://github.com/jcsvwinston/quantum/blob/main/docs/adr/QADR-0006-integracion-quark-orbit.md)):

- **Live SQL feed (Caso 1)** — the shop module's HTTP handlers query Quark
  through a client wrapped with
  [`orbit/quarkbridge`](https://github.com/jcsvwinston/orbit/tree/main/quarkbridge):
  every statement appears in Orbit's live view, correlated to the request, with
  bind arguments redacted by default.
- **Data Studio on Quark (Caso 2)** — Orbit's Data Studio is backed by
  [`orbit/quarkdatasource`](https://github.com/jcsvwinston/orbit/tree/main/quarkdatasource)
  over the same models (`orbit.Config.DataSource`), so `/admin` browses and
  edits them.

Two Quark models (`Author`, `Article` with a `belongs_to`), one sqlite file
shared by the app database (admin auth) and the domain schema.

## Run it

This example has its own Go module (Quark and Orbit must not enter the
framework's dependency graph), and it resolves through the **Quantum suite
workspace**:

```bash
# from the quantum umbrella checkout (quantum/go.work lists this module)
cd nucleus/examples/showcase_demo
go run .
```

Until Orbit cuts its next tag (suite Fase 3), standalone module-proxy builds are
not available — the workspace is the supported path, and it is also how the
suite's integration CI exercises the example.

## Try it

```bash
curl -s localhost:8091/api/articles | jq .
curl -s -X POST localhost:8091/api/articles \
    -H 'Content-Type: application/json' \
    -d '{"author_id":1,"title":"probe","body":"live feed"}' | jq .
```

Then open **http://localhost:8091/admin** (user `admin`, password
`showcase-demo` — local demo credentials):

- **Live view**: hit the API and watch the Quark statements arrive with their
  `request_id`.
- **Data Studio**: browse `Author`/`Article`, create or edit a record, and see
  it through the public API.

## What to read

- [`main.go`](main.go) — the whole wiring: Quark client + migrate/seed,
  `quarkdatasource.Register[T]`, `orbit.Config.DataSource`, open authz for the
  demo API.
- [`shop/module.go`](shop/module.go) — the bridge derivation in `OnStart`
  (`client.WithOptions(quark.WithMiddleware(quarkbridge.New(rt.Observability())))`)
  and the handlers.
