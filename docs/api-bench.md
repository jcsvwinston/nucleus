# API bench — what testing an application and describing its API can do today

This is the numerator of the A10 gate ("testing and OpenAPI as first-class").
It exists because that gate needs a number, and a number needs something that
produces it.

**Measured on 2026-09-25 against the checkout at this commit — the module
identical to its tagged release v1.30.1 — and kept current as the arc closes
its gaps: the numbers below are what the suite produced on its last run.** Run
it with:

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

**12 of 46 controls present. 8 partial. 26 absent.**

| family | present | partial | absent |
|---|---|---|---|
| di | 4 | 1 | 4 |
| http | 2 | 2 | 8 |
| openapi | 2 | 2 | 6 |
| testkit | 4 | 3 | 8 |
| **total** | **12** | **8** | **26** |

### di — 4 present · 1 partial · 4 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `DI-01` | modules receive a typed runtime: services by method, not by key | **present** | — |
| `DI-02` | a module provides a service another module consumes typed | **absent** | ServiceRegistration is a background runner (Run, Health); nothing lets module A hand module B a typed value, so B reaches for a package-level variable or a string key in the request context. |
| `DI-03` | request-scoped values are typed | **partial** | Context.Set(key string, v interface{}) and Get(key) interface{}: a keyed store with a type assertion at every read; no typed accessor. |
| `DI-04` | start order follows declared dependencies between modules | **absent** | Module.Requires names DATABASE aliases, not modules, and modules start in name order (sortedModuleSpecs): a module that needs another's OnStart to have run renames itself or hopes. |
| `DI-05` | a failed OnStart shuts down the modules already started | **absent** | when module B's OnStart fails, module A — started before it — never sees its OnShutdown (NU-44, documented in the source as a follow-up): whatever A opened stays open while the process reports the failure. |
| `DI-06` | application-level hooks run around the modules' hooks, in order | **present** | — |
| `DI-07` | constructors report bad input as an error, never a panic | **absent** | auth.NewJWTManager panics on a short secret while NewJWTManagerFromKeys returns an error: two constructors for one type with two contracts, the case NU-41 names; the style guide it asks for is not written. |
| `DI-08` | a module declares a typed configuration and receives it typed | **present** | — |
| `DI-09` | a module's configuration is bound from the config file under modules.<name> | **present** | — |

### http — 2 present · 2 partial · 8 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `HT-01` | a JSON body binds into a struct and is validated by its tags | **present** | — |
| `HT-02` | query parameters bind into a struct, typed | **absent** | Query(name) returns one string; a filter with a page, a size and a sort is parsed field by field in every handler. |
| `HT-03` | path parameters bind typed | **absent** | Param(name) returns a string; every numeric or UUID id is converted and checked by the handler. |
| `HT-04` | headers bind typed | **absent** | no header binding; handlers read c.Request.Header by hand. |
| `HT-05` | a validation failure names the field that failed | **present** | — |
| `HT-06` | errors are problem+json (RFC 9457) | **absent** | errors answer as application/json in the framework's own envelope ({error: {code, message, details}}), not application/problem+json (RFC 9457), so a generic client cannot read them by the standard's names. |
| `HT-07` | content negotiation by Accept: one handler, the representation asked for | **absent** | JSON(), XML(), HTML() and String() each commit to one representation; nothing reads Accept and picks, so a handler that serves two needs two. |
| `HT-08` | declarative API versioning | **absent** | neither Router nor Module declares a version; /v1 is a Prefix the author types, with no header, negotiation or deprecation behind it. |
| `HT-09` | a timeout where the route says | **partial** | one timeout for the whole router (WithTimeout) with exempt path prefixes (WithTimeoutExempt); a slow export and a fast lookup share the same limit unless one is exempted entirely. |
| `HT-10` | an unknown route answers a JSON 404 in the framework's envelope | **partial** | an unknown path under a module's prefix answers 404 with Go's plain-text "404 page not found", not the framework's JSON envelope, even with Accept: application/json — a client reading errors by the envelope reads nothing. |
| `HT-11` | the raw-HTML writer is named as such; HTML renders a template | **absent** | nucleus.Context.HTML(code, html) writes a raw string while router.Context.HTML(status, template, data) renders a template: same name, two meanings (NU-41). There is no RawHTML. |
| `HT-12` | one error envelope: a domain error and the router's 404 share a shape | **absent** | a domain error answers {error: {code, message}} and the router's own 404 answers plain text (HT-10): two shapes for one client. |

### openapi — 2 present · 2 partial · 6 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `OA-01` | an OpenAPI 3.1 document model with schemas, parameters and security | **present** | — |
| `OA-02` | the application serves its document at a route | **present** | — |
| `OA-03` | the document derives from the registered routes | **absent** | an application with routes and no document provider answers 404 at /openapi.json: the document is whatever the author writes by hand in the scaffold's internal/contracts registrars, and nothing reads the router to produce or check it. |
| `OA-04` | schemas derive from Go structs | **absent** | pkg/openapi builds schemas by hand (ObjectSchema, ArraySchema, RefSchema); nothing reads a struct's fields and tags, so every schema is written twice — once as the Go type, once as the document. |
| `OA-05` | requests are validated against the document | **absent** | no middleware validates a request against the document; validation is struct tags on whatever the handler binds (HT-01), which the document knows nothing about. |
| `OA-06` | a test asserts a response conforms to the document | **absent** | pkg/nucleustest never reads the document: a response that drifts from the contract passes every test. |
| `OA-07` | a client is generated from the document | **partial** | nucleus openapi --out exports the document to a file; nothing generates a client from it. The gate of the arc asks for a TypeScript client that consumes the starter's API in a test. |
| `OA-08` | the scaffold's document declares the application's security scheme | **absent** | the contracts the scaffold writes declare paths and schemas and no security scheme, so the document says the API is open while the application requires a bearer token. |
| `OA-09` | the document is under contract control: a breaking change turns a check red | **absent** | contracts/baseline freezes exported symbols, CLI commands, config keys and the security posture; the OpenAPI document is not among them, so a path or a field can disappear with every check green. |
| `OA-10` | the generated application publishes its document | **partial** | the CLI exports the document to a file; the generated application does not serve it — WithOpenAPIHandler exists in pkg/nucleus and no template calls it. |

### testkit — 4 present · 3 partial · 8 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `TK-01` | an application boots in-process for a test and stops on cleanup | **present** | — |
| `TK-02` | a request helper speaks JSON: encodes the body, decodes the answer | **absent** | the kit offers Client() *http.Client and URL(path): every JSON round trip is the author's own marshal, request, status check and decode. A helper that takes a body and a target and returns the status is the smallest thing every other kit has. |
| `TK-03` | cookies the application sets persist across the kit's requests | **absent** | the kit's *http.Client has no cookie jar: a session cookie the application sets on one request is dropped on the next, so cookie-based routes cannot be walked as a user. |
| `TK-04` | the kit obtains a CSRF token for state-changing requests | **absent** | nothing fetches or threads a CSRF token: a test of a form POST behind the CSRF middleware reads the token out of a page or a cookie and sets the header by hand, or turns the middleware off. |
| `TK-05` | a test acts as a user: a session the application recognises | **partial** | MintToken issues a bearer token for the JWT routes; there is no helper that signs a user in and opens the session the cookie-based routes recognise, and no client that would carry the cookie (TK-03). |
| `TK-06` | factories build persisted records with defaults | **absent** | no factories in pkg/nucleustest: every test inserts its rows by hand through DB() or a route. |
| `TK-07` | a transaction per test, rolled back on cleanup | **absent** | no transaction per test: TempSQLite gives each test its own database file, which is isolation by copy — right for SQLite, no answer for a test suite on the application's PostgreSQL. |
| `TK-08` | a mail double captures what the application sent | **absent** | mail providers registered: noop and smtp. noop discards, smtp sends; nothing a test can read back, so a flow that sends a verification mail cannot assert the mail. |
| `TK-09` | a storage double the test can read back | **partial** | the real store is reachable through Runtime().Storage() and readable (Get, List, Exists), so a test CAN look at what the local provider wrote; nothing captures for it or asserts on it, and a test against another provider talks to that provider. |
| `TK-10` | a tasks double: the test sees what was enqueued | **partial** | when a module registers jobs the memory provider's inspector reads the runtime (queues, sizes, workers) through TaskInspectorFrom — an operations view; nothing lists the enqueued payloads for a test to assert that a handler enqueued the right task without running it. |
| `TK-11` | a double for the HTTP the application makes to other services | **absent** | nothing in the kit intercepts the HTTP the application makes to other services: a test of a webhook or an outgoing call needs its own httptest server wired through configuration. |
| `TK-12` | a helper reads a server-sent event stream | **present** | — |
| `TK-13` | the runtime is reachable from the test: database, migrations, services | **present** | — |
| `TK-14` | the generated code ships a test that uses the kit | **present** | — |
| `TK-15` | contract tests for a module: a kit checks a ModuleSpec against what the framework expects | **absent** | the kit boots an application; it does not check a ModuleSpec against the contract (name, prefix, requires, migrations, hooks) on its own, so a module's mistakes surface as boot failures in an application test. |

## What the shape of it says

**The kit boots; it does not help.** Four of the testkit's present controls
are the same fact — an application comes up in-process, with its runtime, its
database and its stream reachable — and everything an author does after that
is by hand: encode, request, decode, carry no cookie, fetch no token, insert
every row, read no mail. The kit is a launcher, and `website/docs/getting-started/testing.md`
calls it experimental. The arc's first sessions are the client and the data.

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
