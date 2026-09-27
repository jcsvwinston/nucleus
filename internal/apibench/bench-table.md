**18 of 46 controls present. 7 partial. 21 absent.**

| family | present | partial | absent |
|---|---|---|---|
| di | 4 | 1 | 4 |
| http | 2 | 2 | 8 |
| openapi | 2 | 2 | 6 |
| testkit | 10 | 2 | 3 |
| **total** | **18** | **7** | **21** |

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

### testkit — 10 present · 2 partial · 3 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `TK-01` | an application boots in-process for a test and stops on cleanup | **present** | — |
| `TK-02` | a request helper speaks JSON: encodes the body, decodes the answer | **present** | — |
| `TK-03` | cookies the application sets persist across the kit's requests | **present** | — |
| `TK-04` | the kit obtains a CSRF token for state-changing requests | **present** | — |
| `TK-05` | a test acts as a user: a session the application recognises | **present** | — |
| `TK-06` | factories build persisted records with defaults | **present** | — |
| `TK-07` | a transaction per test, rolled back on cleanup | **present** | — |
| `TK-08` | a mail double captures what the application sent | **absent** | mail providers registered: noop and smtp. noop discards, smtp sends; nothing a test can read back, so a flow that sends a verification mail cannot assert the mail. |
| `TK-09` | a storage double the test can read back | **partial** | the real store is reachable through Runtime().Storage() and readable (Get, List, Exists), so a test CAN look at what the local provider wrote; nothing captures for it or asserts on it, and a test against another provider talks to that provider. |
| `TK-10` | a tasks double: the test sees what was enqueued | **partial** | when a module registers jobs the memory provider's inspector reads the runtime (queues, sizes, workers) through TaskInspectorFrom — an operations view; nothing lists the enqueued payloads for a test to assert that a handler enqueued the right task without running it. |
| `TK-11` | a double for the HTTP the application makes to other services | **absent** | nothing in the kit intercepts the HTTP the application makes to other services: a test of a webhook or an outgoing call needs its own httptest server wired through configuration. |
| `TK-12` | a helper reads a server-sent event stream | **present** | — |
| `TK-13` | the runtime is reachable from the test: database, migrations, services | **present** | — |
| `TK-14` | the generated code ships a test that uses the kit | **present** | — |
| `TK-15` | contract tests for a module: a kit checks a ModuleSpec against what the framework expects | **absent** | the kit boots an application; it does not check a ModuleSpec against the contract (name, prefix, requires, migrations, hooks) on its own, so a module's mistakes surface as boot failures in an application test. |
