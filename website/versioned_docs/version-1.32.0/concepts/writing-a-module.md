---
sidebar_position: 4.5
title: Writing a module
covers:
  - pkg/nucleus.Module
  - pkg/nucleus.ModuleSpec
  - pkg/nucleus.CheckModule
  - pkg/nucleus.ModuleCheck
  - pkg/nucleus.PolicyRule
  - pkg/nucleus.Handle
  - pkg/nucleus.Provide
  - pkg/nucleus.Resolve
  - pkg/nucleus.Runtime.Outbox
---

# Writing a module

A module is the unit an application is built from, and the unit it is
extended with: its routes, the policy rows that open them, its typed
configuration, its migrations, its jobs and the values it provides travel
together, and an application gets all of them with one `Mount`. This page is
about writing one that lives in a repository of its own — a module somebody
else's application `go get`s. Inside one application, `nucleus generate
module` writes a slice of it instead (see the [quickstart](../getting-started/quickstart.md));
how mounted modules meet each other is [Modules — wiring and lifecycle](./modules.md).

## Start from the template

```bash
nucleus new greeter --template module --module github.com/acme/greeter
cd greeter
go test ./...
```

| File | What it is |
|---|---|
| `go.mod` | the module path, and `require` of the Nucleus release the CLI belongs to and of the SQLite driver the tests open a database with |
| `module.go` | the module: a route under `/greeter`, the policy row that opens it, typed configuration, and a `Greeter` it provides |
| `module_test.go` | a test that calls `nucleustest.CheckModule`, and two that mount the module on an in-process application |
| `README.md` | how an application installs, mounts and configures it |
| `.github/workflows/test.yml` | `go vet` and `go test` on every push to `main` and every pull request, the actions pinned to a commit |
| `.github/dependabot.yml` | the next action pins and the next Nucleus release, proposed weekly |

The name is the module's: `Order-Notes` becomes the module name
`order_notes` (lowercase letters, digits and underscores, starting with a
letter — it is the `modules.<name>` configuration key and the route prefix)
and the Go package `ordernotes`. A name that cannot be both is refused, and
so are `--db`, `--port` and `--with`: a module has no `main.go` or
`nucleus.yml` for them — the application that mounts it chooses its
database, its port and its catalog entries. Without `--offline` the CLI
fetches the SQLite driver and tidies; with it, it prints the command.

## What a module carries

```go
func Module() nucleus.ModuleSpec {
	var greeter *Greeter
	return nucleus.Module[Config]{
		Name:   Name,
		Prefix: "/" + Name,
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/hello", Action: "read"},
		},
		OnStart: func(_ context.Context, rt nucleus.Runtime, cfg Config) error {
			greeter = &Greeter{greeting: cfg.Greeting}
			return nucleus.Provide(rt, greeter)
		},
		Routes: func(r nucleus.Router, _ Config) {
			nucleus.Handle(r, http.MethodGet, "/hello", func(_ *nucleus.Context, q HelloQuery) (Greeting, error) {
				return Greeting{Message: greeter.Greet(q.Name)}, nil
			})
		},
	}.Build()
}
```

- **`Name` and `Prefix`.** The name is how the application addresses the
  module everywhere; the prefix is where its routes mount, and what its
  policy rows and CSRF exemptions are relative to.
- **`Config`.** The application writes it under `modules.<name>` in
  `nucleus.yml`. At boot the framework binds that subtree into the typed
  struct, fills what is still zero from `default:` tags and checks the
  `validate:` tags; a module whose configuration it cannot work with stops
  the boot and names the key. A value set in Go is the baseline the file
  overrides.
- **`Policies`.** The application denies by default. A module opens its own
  routes with rows relative to its prefix, so the host does not edit its
  policy file by hand; a deny row in that file still wins.
- **`OnStart`.** Runs before the routes register, with the configuration
  bound: build what the routes use here, from the `Runtime` the framework
  hands over (`DB()`, `Logger()`, `Mailer()`, `Storage()`, `Outbox()`, …),
  and `Provide` what other modules may `Resolve`.
- **`Routes`.** A typed endpoint (`nucleus.Handle`) binds and validates its
  input, writes its output and describes both in the application's OpenAPI
  document.
- **The rest.** `Migrations` (an embedded `fs.FS` of `.up.sql`/`.down.sql`
  files, applied on start), `Jobs` and `Webhooks`, `Templates`, `Models`,
  `CSRFExempt`, `DependsOn`, `Requires` (database aliases it needs) and
  `OnShutdown` — each with its own section in [Modules — wiring and
  lifecycle](./modules.md) and the pages it links.

## How it is checked

```go
func TestConformance(t *testing.T) {
	nucleustest.CheckModule(t, greeter.Module())
}
```

`nucleustest.CheckModule` runs the checks the framework applies when an
application mounts the module, one at a time, and fails the test once for
every check the module does not pass, naming it and saying what the
framework says: the name and the prefix, the configuration and its
defaults, `DependsOn`, the databases it requires, every policy row (well
formed, and about a route the module serves), the CSRF exemptions,
templates, models, `OnStart`, migrations, jobs, webhooks, the routes and
`OnShutdown`. Boot stops at the first error; the check reports all of them,
so a module with three defects meets them in one run. It is the test that
tells a module's author what an application would have told them at boot —
keep it.

A module that needs configuration of the application to start —
`outbox.enabled: true`, a database by name, its own `modules.<name>` keys —
is checked with `nucleustest.CheckModuleIn`, against an application built
from a configuration file:

```go
nucleustest.CheckModuleIn(t, nucleus.New().FromConfigFile("testdata/nucleus.yml"), billing.Module())
```

The template's other tests boot an in-process application with
`nucleustest.Start`, call the route the way a client does, and mount a
second module that declares `DependsOn` and resolves the `Greeter` — the
two halves of a module's surface an application uses.

## Publishing it

- **Versions.** Tag the repository with semantic versions; an application
  pins one with `go get github.com/acme/greeter@v0.1.0`. `go.mod` starts on
  the Nucleus release the CLI belongs to, and Dependabot proposes the next
  one — the CI run on that pull request is the module's compatibility check.
- **Drivers.** The module's code links no database driver: the
  application that mounts it chooses its engine. Only the test binary links
  SQLite, through the blank import in `module_test.go`.
- **CI.** The workflow runs `go vet` and `go test ./...`, the conformance
  test among them, with the actions pinned to a commit and the tag beside
  the pin.

## Extending the framework from a module

A module's `OnStart` can register on the framework's seams, which is how an
extension is written in process. The tested example in the repository,
[`internal/fixtures/inprocess/dirqueue`](https://github.com/jcsvwinston/nucleus/tree/main/internal/fixtures/inprocess/dirqueue),
adds an outbox bridge written in Go — every committed message on a routed
topic becomes a JSON file in a directory queue:

```go
OnStart: func(_ context.Context, rt nucleus.Runtime, cfg Config) error {
	box := rt.Outbox()
	if box == nil {
		return errors.New("dirqueue: the application has no outbox — set outbox.enabled: true")
	}
	if err := box.RegisterBridge(&Bridge{Dir: cfg.Dir}); err != nil {
		return err
	}
	box.AddRoute(cfg.Pattern, BridgeName)
	return nil
},
```

The bridge is four methods — `Name`, `Send`, `Healthy`, `Close` — and the
outbox owns delivery around it: a `Send` that returns an error is retried
with backoff until `outbox.max_retries`, then the message goes to the dead
letter (`nucleus outbox requeue` brings it back). A refusal no attempt will
change is returned wrapped with `outbox.Permanent`, and the message goes to
the dead letter on that attempt. Bridges are registered in `OnStart`
because the dispatcher starts after every module has started, so its first
pass already routes to them.

Providers that are selected by name in the configuration register by
import instead, from the package's `init`, the way the first-party modules
under `providers/` and `exporters/` do: a storage backend
([Storage & background tasks](../features/storage-and-tasks.md#using-a-storage-backend-nucleus-does-not-ship)),
a mail provider, a federated sign-in provider, a metrics exporter, a
secrets resolver, a request interceptor.

## Out of process: plugins

When the extension should not be compiled into the application — another
language, an isolated process, a binary operations install on their own —
it is a capability plugin: an executable `nucleus-plugin-<provider>` that
speaks a JSON envelope on stdin and stdout. The mail runtime sends it
`mail.send`, and an outbox bridge of type `plugin` sends it `queue.publish`
or `webhook.deliver` ([the outbox's plugin bridge](../features/storage-and-tasks.md#plugin-bridge-an-external-plugin-delivers)).
The [Plugin SDK reference](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/PLUGIN_SDK.md)
has the contract and `plugins.Serve`, which writes the plugin side in Go;
the relay example plugin does the same `queue.publish` delivery as
`dirqueue`, out of process, so the two can be read side by side.
