# API bench — what testing an application and describing its API can do today

This is the numerator of the A10 gate ("testing and OpenAPI as first-class").
It exists because that gate needs a number, and a number needs something that
produces it.

**Measured on 2026-09-25 against v1.30.1 — 12 of 46 — and kept current as
the arc closed its gaps: `S1`–`S4` the kit (client, data, doubles, the
starter and the module check), `S5`–`S7` the document (derived from the
code, enforced, and a TypeScript client generated from it), `S8` binding and
errors, `S9` how modules find each other.** Run it with:

```bash
go test ./internal/apibench/ -run TestAPIBench -v
go test ./internal/apibench/ -run TestAPIBenchSummary -v                 # per-family counts
NUCLEUS_API_BENCH_TABLE=1 go test ./internal/apibench/ -run TestAPIBenchTable   # writes bench-table.md next to the cases
```

The last command writes a generated `bench-table.md` next to the cases, a
file that is not committed; the tables under "The result" — the per-family
summary and the catalogue — are pasted from it when a verdict moves, so the
page and the catalogue say the same thing, and a guard compares both with the
cases.

The bench is not prose. Every control is a Go probe in `internal/apibench/`
that boots a real application with one small module, calls a real route and
reads the answer — or, for a helper an author would call, asks the kit's own
method set and the module's own source for it under any reasonable name, and
logs what it found so the gap is concrete. `TestAPIBench` asserts the
**recorded verdict** rather than success, so closing a gap turns the suite red
with "this one is present now, update the verdict" — which is what keeps this
page honest.

There was already a description of this: the maturity audit of 2026-09-03
scored Nucleus testing at 3 of 5 and routing/HTTP at 3 of 5 by reading the
code, and the plan for the arc listed what to build. Reading is a hypothesis.
The measurement confirmed the shape and moved the emphasis (below), and it
found two things the plan did not know.

## The verdicts

| verdict | meaning |
|---|---|
| **present** | the control exists and its probe exercised it end to end |
| **partial** | a piece exists; the case records exactly what is missing |
| **absent** | no surface at all — the probe measures the absence: a method set without the helper, a 404 that is not JSON, a shutdown hook that never ran |

A control that cannot be probed does not belong in the bench. A control whose
surface appears (a new method on the kit, a new middleware) turns its probe red
against the recorded verdict, and the probe then grows the behaviour check the
new surface makes possible.

A method-set probe is the honest form for a helper that does not exist yet:
there is no name to compile against, so the probe asks for the capability
under the names the other kits use and says which ones it looked for. Once a
name exists, the probe calls it.

## The result

**46 of 46 controls present. 0 partial. 0 absent.**

| family | present | partial | absent |
|---|---|---|---|
| di | 9 | 0 | 0 |
| http | 12 | 0 | 0 |
| openapi | 10 | 0 | 0 |
| testkit | 15 | 0 | 0 |
| **total** | **46** | **0** | **0** |

### di — 9 present · 0 partial · 0 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `DI-01` | modules receive a typed runtime: services by method, not by key | **present** | — |
| `DI-02` | a module provides a service another module consumes typed | **present** | — |
| `DI-03` | request-scoped values are typed | **present** | — |
| `DI-04` | start order follows declared dependencies between modules | **present** | — |
| `DI-05` | a failed OnStart shuts down the modules already started | **present** | — |
| `DI-06` | application-level hooks run around the modules' hooks, in order | **present** | — |
| `DI-07` | constructors report bad input as an error, never a panic | **present** | — |
| `DI-08` | a module declares a typed configuration and receives it typed | **present** | — |
| `DI-09` | a module's configuration is bound from the config file under modules.<name> | **present** | — |

### http — 12 present · 0 partial · 0 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `HT-01` | a JSON body binds into a struct and is validated by its tags | **present** | — |
| `HT-02` | query parameters bind into a struct, typed | **present** | — |
| `HT-03` | path parameters bind typed | **present** | — |
| `HT-04` | headers bind typed | **present** | — |
| `HT-05` | a validation failure names the field that failed | **present** | — |
| `HT-06` | errors are problem+json (RFC 9457) | **present** | — |
| `HT-07` | content negotiation by Accept: one handler, the representation asked for | **present** | — |
| `HT-08` | declarative API versioning | **present** | — |
| `HT-09` | a timeout where the route says | **present** | — |
| `HT-10` | an unknown route answers a JSON 404 in the framework's envelope | **present** | — |
| `HT-11` | the raw-HTML writer is named as such; HTML renders a template | **present** | — |
| `HT-12` | one error envelope: a domain error and the router's 404 share a shape | **present** | — |

### openapi — 10 present · 0 partial · 0 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `OA-01` | an OpenAPI 3.1 document model with schemas, parameters and security | **present** | — |
| `OA-02` | the application serves its document at a route | **present** | — |
| `OA-03` | the document derives from the registered routes | **present** | — |
| `OA-04` | schemas derive from Go structs | **present** | — |
| `OA-05` | requests are validated against the document | **present** | — |
| `OA-06` | a test asserts a response conforms to the document | **present** | — |
| `OA-07` | a client is generated from the document | **present** | — |
| `OA-08` | the scaffold's document declares the application's security scheme | **present** | — |
| `OA-09` | the document is under contract control: a breaking change turns a check red | **present** | — |
| `OA-10` | the generated application publishes its document | **present** | — |

### testkit — 15 present · 0 partial · 0 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `TK-01` | an application boots in-process for a test and stops on cleanup | **present** | — |
| `TK-02` | a request helper speaks JSON: encodes the body, decodes the answer | **present** | — |
| `TK-03` | cookies the application sets persist across the kit's requests | **present** | — |
| `TK-04` | the kit obtains a CSRF token for state-changing requests | **present** | — |
| `TK-05` | a test acts as a user: a session the application recognises | **present** | — |
| `TK-06` | factories build persisted records with defaults | **present** | — |
| `TK-07` | a transaction per test, rolled back on cleanup | **present** | — |
| `TK-08` | a mail double captures what the application sent | **present** | — |
| `TK-09` | a storage double the test can read back | **present** | — |
| `TK-10` | a tasks double: the test sees what was enqueued | **present** | — |
| `TK-11` | a double for the HTTP the application makes to other services | **present** | — |
| `TK-12` | a helper reads a server-sent event stream | **present** | — |
| `TK-13` | the runtime is reachable from the test: database, migrations, services | **present** | — |
| `TK-14` | the generated code ships a test on the kit that speaks through the kit's client, not a bare http.Client | **present** | — |
| `TK-15` | contract tests for a module: a kit checks a ModuleSpec against what the framework expects | **present** | — |

## What the shape of it says

**The kit boots and, since `S1`, talks.** On the day of the baseline four of
the testkit's present controls were the same fact — an application comes up
in-process, with its runtime, its database and its stream reachable — and
everything an author did after that was by hand. `S1` gave the kit its client:
a request that takes a body and decodes the answer, a cookie jar that keeps
the application's Secure cookies over the loopback test server, the CSRF token
fetched and sent, and a session opened in the application's own store so the
routes that read one see a signed-in user. `S2` gave it the data: `Make`
persists a record of a registered model with defaults from its own metadata,
and `Transactional` runs the whole test — the application's routes included —
inside one transaction rolled back at the end, one level below the pool, with
the application's own transactions as savepoints; measured on SQLite,
PostgreSQL and MySQL. `S3` gave it the doubles: a `memory` mail driver the
kit selects when the application would discard mail and reads back with
`SentMail`; a `memory` storage provider read back with `Stored` and
`StoredKeys`; the in-process job provider's record of every enqueue, read
with `EnqueuedTasks`; and an `HTTPRecorder` that stands in for another
service and remembers what the application sent it. What is left of the
family is the module contract kit (`TK-15`).

**The document is a file somebody writes.** `pkg/openapi` is a faithful 3.1
model and the application serves whatever document it is handed, but nothing
reads the router or a struct to produce it, nothing validates a request or a
response against it, nothing freezes it, and the generated application does
not even serve the one its scaffold writes. The gate — a generated client
consuming the starter's API in a test — has every step ahead of it.

**Binding stops at the body.** JSON binding with tag validation and a
field-level error message are present; query, path and headers are strings;
errors are the framework's envelope, which a generic client cannot read as
problem+json; and the router's own 404 is not even in that envelope.

**Modules are wired by name.** The runtime is typed, module configuration is
typed and bound from the file, and application hooks bracket the modules —
but a module cannot hand another one a value, cannot say it starts after
another, and a failed start leaves the earlier modules running.

## What the bench found that the reading did not

1. **The router's own 404 is Go's plain text, everywhere.** Under a module's
   prefix and outside any module, an unknown path answers
   `404 page not found` as `text/plain`, with `Accept: application/json` set.
   The framework's error envelope covers what handlers return and not what
   the router says on its own, so a client that reads errors by the envelope
   reads nothing on the most common error. Recorded as `HT-10` partial and
   `HT-12` absent, and as a finding in the umbrella's registry.
2. **The scaffold's contract says the API is open.** The contracts `nucleus
   new` writes declare paths and schemas and no security scheme, while the
   application they describe requires a bearer token on those paths — and the
   application does not serve the document anyway (`OA-08`, `OA-10`). A
   document that misdescribes authentication is worse than none for the
   client generator the gate asks for.
3. **The jobs runtime does not exist until a module registers a job.**
   `Runtime().Tasks()` is nil in a default application (documented, NF-13);
   the probe had to mount a job to measure the tasks double at all. Not a
   defect; a fact the kit's design has to know.
4. **Two of the plan's four lines are one line.** "Typed binding of query,
   path and headers" and "problem+json" are the same session as the JSON 404
   and the error envelope (`HT-02`…`HT-12`): one shape for input and one for
   errors, or neither is done.

## What this bench does not measure

The panel that would show a test run or an API document is Orbit's surface
and is measured where it lives. Performance of the kit (how long a boot takes)
is A12's. And whether the arc's light dependency injection REPLACES the
service locator is not a control here: QADR-0010 keeps the locator until the
major, so what `DI-02` and `DI-03` measure is whether the typed form exists
beside it.
