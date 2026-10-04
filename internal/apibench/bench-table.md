**45 of 46 controls present. 1 partial. 0 absent.**

| family | present | partial | absent |
|---|---|---|---|
| di | 9 | 0 | 0 |
| http | 12 | 0 | 0 |
| openapi | 9 | 1 | 0 |
| testkit | 15 | 0 | 0 |
| **total** | **45** | **1** | **0** |

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

### openapi — 9 present · 1 partial · 0 absent

| id | control | verdict | what is missing |
|---|---|---|---|
| `OA-01` | an OpenAPI 3.1 document model with schemas, parameters and security | **present** | — |
| `OA-02` | the application serves its document at a route | **present** | — |
| `OA-03` | the document derives from the registered routes | **present** | — |
| `OA-04` | schemas derive from Go structs | **present** | — |
| `OA-05` | requests are validated against the document | **present** | — |
| `OA-06` | a test asserts a response conforms to the document | **present** | — |
| `OA-07` | a client is generated from the document | **partial** | nucleus openapi --out exports the document to a file; nothing generates a client from it. The gate of the arc asks for a TypeScript client that consumes the starter's API in a test. |
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
