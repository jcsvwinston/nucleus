// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

// controls is the bench: every capability the testing and API-description
// surface is measured on, with the verdict this repository RECORDS for it.
//
// The list is a policy choice, not a derivation of what happens to exist. It
// comes from what an application author needs to TEST an application and to
// DESCRIBE its API, taken from the products that already answer it — Rails'
// integration tests and Encore's client for the kit, FastAPI and Goa for the
// contract, Echo for binding and negotiation, Spring for how modules find
// each other — which is the same comparison the maturity audit of 2026-09-03
// scored this repository against.
//
// A verdict is what the probe MEASURED on the day it was recorded. Moving one
// is a deliberate edit in the change that moves the code.
func controls() []control {
	return []control{
		// ---- testkit -----------------------------------------------------
		{id: "TK-01", family: "testkit", title: "an application boots in-process for a test and stops on cleanup",
			want: present, probe: probeBootInProcess},
		{id: "TK-02", family: "testkit", title: "a request helper speaks JSON: encodes the body, decodes the answer",
			want: absent, note: "the kit offers Client() *http.Client and URL(path): every JSON round trip is the author's own marshal, " +
				"request, status check and decode. A helper that takes a body and a target and returns the status is the " +
				"smallest thing every other kit has.",
			probe: probeJSONRequestHelper},
		{id: "TK-03", family: "testkit", title: "cookies the application sets persist across the kit's requests",
			want: absent, note: "the kit's *http.Client has no cookie jar: a session cookie the application sets on one request is dropped on " +
				"the next, so cookie-based routes cannot be walked as a user.",
			probe: probeCookieJar},
		{id: "TK-04", family: "testkit", title: "the kit obtains a CSRF token for state-changing requests",
			want: absent, note: "nothing fetches or threads a CSRF token: a test of a form POST behind the CSRF middleware reads the token out " +
				"of a page or a cookie and sets the header by hand, or turns the middleware off.",
			probe: probeCSRFHelper},
		{id: "TK-05", family: "testkit", title: "a test acts as a user: a session the application recognises",
			want: partial, note: "MintToken issues a bearer token for the JWT routes; there is no helper that signs a user in and opens the " +
				"session the cookie-based routes recognise, and no client that would carry the cookie (TK-03).",
			probe: probeActAsUser},
		{id: "TK-06", family: "testkit", title: "factories build persisted records with defaults",
			want: absent, note: "no factories in pkg/nucleustest: every test inserts its rows by hand through DB() or a route.",
			probe: probeFactories},
		{id: "TK-07", family: "testkit", title: "a transaction per test, rolled back on cleanup",
			want: absent, note: "no transaction per test: TempSQLite gives each test its own database file, which is isolation by copy — right " +
				"for SQLite, no answer for a test suite on the application's PostgreSQL.",
			probe: probeTxPerTest},
		{id: "TK-08", family: "testkit", title: "a mail double captures what the application sent",
			want: absent, note: "mail providers registered: noop and smtp. noop discards, smtp sends; nothing a test can read back, so a flow " +
				"that sends a verification mail cannot assert the mail.",
			probe: probeMailCapture},
		{id: "TK-09", family: "testkit", title: "a storage double the test can read back",
			want: partial, note: "the real store is reachable through Runtime().Storage() and readable (Get, List, Exists), so a test CAN look " +
				"at what the local provider wrote; nothing captures for it or asserts on it, and a test against another " +
				"provider talks to that provider.",
			probe: probeStorageCapture},
		{id: "TK-10", family: "testkit", title: "a tasks double: the test sees what was enqueued",
			want: partial, note: "when a module registers jobs the memory provider's inspector reads the runtime (queues, sizes, workers) " +
				"through TaskInspectorFrom — an operations view; nothing lists the enqueued payloads for a test to assert that " +
				"a handler enqueued the right task without running it.",
			probe: probeTasksCapture},
		{id: "TK-11", family: "testkit", title: "a double for the HTTP the application makes to other services",
			want: absent, note: "nothing in the kit intercepts the HTTP the application makes to other services: a test of a webhook or an " +
				"outgoing call needs its own httptest server wired through configuration.",
			probe: probeOutboundHTTPDouble},
		{id: "TK-12", family: "testkit", title: "a helper reads a server-sent event stream",
			want: present, probe: probeStreamHelper},
		{id: "TK-13", family: "testkit", title: "the runtime is reachable from the test: database, migrations, services",
			want: present, probe: probeRuntimeReachable},
		{id: "TK-14", family: "testkit", title: "the generated code ships a test that uses the kit",
			want: present, probe: probeStarterShipsTest},
		{id: "TK-15", family: "testkit", title: "contract tests for a module: a kit checks a ModuleSpec against what the framework expects",
			want: absent, note: "the kit boots an application; it does not check a ModuleSpec against the contract (name, prefix, requires, " +
				"migrations, hooks) on its own, so a module's mistakes surface as boot failures in an application test.",
			probe: probeModuleContractKit},

		// ---- openapi -----------------------------------------------------
		{id: "OA-01", family: "openapi", title: "an OpenAPI 3.1 document model with schemas, parameters and security",
			want: present, probe: probeDocumentModel},
		{id: "OA-02", family: "openapi", title: "the application serves its document at a route",
			want: present, probe: probeServedDocument},
		{id: "OA-03", family: "openapi", title: "the document derives from the registered routes",
			want: absent, note: "an application with routes and no document provider answers 404 at /openapi.json: the document is whatever the " +
				"author writes by hand in the scaffold's internal/contracts registrars, and nothing reads the router to produce " +
				"or check it.",
			probe: probeDerivedFromRoutes},
		{id: "OA-04", family: "openapi", title: "schemas derive from Go structs",
			want: absent, note: "pkg/openapi builds schemas by hand (ObjectSchema, ArraySchema, RefSchema); nothing reads a struct's fields and " +
				"tags, so every schema is written twice — once as the Go type, once as the document.",
			probe: probeSchemaFromStruct},
		{id: "OA-05", family: "openapi", title: "requests are validated against the document",
			want: absent, note: "no middleware validates a request against the document; validation is struct tags on whatever the handler " +
				"binds (HT-01), which the document knows nothing about.",
			probe: probeRequestValidation},
		{id: "OA-06", family: "openapi", title: "a test asserts a response conforms to the document",
			want: absent, note: "pkg/nucleustest never reads the document: a response that drifts from the contract passes every test.",
			probe: probeResponseConformance},
		{id: "OA-07", family: "openapi", title: "a client is generated from the document",
			want: partial, note: "nucleus openapi --out exports the document to a file; nothing generates a client from it. The gate of the arc " +
				"asks for a TypeScript client that consumes the starter's API in a test.",
			probe: probeClientGenerator},
		{id: "OA-08", family: "openapi", title: "the scaffold's document declares the application's security scheme",
			want: absent, note: "the contracts the scaffold writes declare paths and schemas and no security scheme, so the document says the " +
				"API is open while the application requires a bearer token.",
			probe: probeSecuritySchemeDeclared},
		{id: "OA-09", family: "openapi", title: "the document is under contract control: a breaking change turns a check red",
			want: absent, note: "contracts/baseline freezes exported symbols, CLI commands, config keys and the security posture; the OpenAPI " +
				"document is not among them, so a path or a field can disappear with every check green.",
			probe: probeSpecUnderContractControl},
		{id: "OA-10", family: "openapi", title: "the generated application publishes its document",
			want: partial, note: "the CLI exports the document to a file; the generated application does not serve it — WithOpenAPIHandler " +
				"exists in pkg/nucleus and no template calls it.",
			probe: probeStarterPublishesSpec},

		// ---- http --------------------------------------------------------
		{id: "HT-01", family: "http", title: "a JSON body binds into a struct and is validated by its tags",
			want: present, probe: probeJSONBinding},
		{id: "HT-02", family: "http", title: "query parameters bind into a struct, typed",
			want: absent, note: "Query(name) returns one string; a filter with a page, a size and a sort is parsed field by field in every " +
				"handler.",
			probe: probeQueryBinding},
		{id: "HT-03", family: "http", title: "path parameters bind typed",
			want: absent, note: "Param(name) returns a string; every numeric or UUID id is converted and checked by the handler.",
			probe: probePathBinding},
		{id: "HT-04", family: "http", title: "headers bind typed",
			want: absent, note: "no header binding; handlers read c.Request.Header by hand.",
			probe: probeHeaderBinding},
		{id: "HT-05", family: "http", title: "a validation failure names the field that failed",
			want: present, probe: probeStructuredValidationErrors},
		{id: "HT-06", family: "http", title: "errors are problem+json (RFC 9457)",
			want: absent, note: "errors answer as application/json in the framework's own envelope ({error: {code, message, details}}), not " +
				"application/problem+json (RFC 9457), so a generic client cannot read them by the standard's names.",
			probe: probeProblemJSON},
		{id: "HT-07", family: "http", title: "content negotiation by Accept: one handler, the representation asked for",
			want: absent, note: "JSON(), XML(), HTML() and String() each commit to one representation; nothing reads Accept and picks, so a " +
				"handler that serves two needs two.",
			probe: probeNegotiation},
		{id: "HT-08", family: "http", title: "declarative API versioning",
			want: absent, note: "neither Router nor Module declares a version; /v1 is a Prefix the author types, with no header, negotiation or " +
				"deprecation behind it.",
			probe: probeVersioning},
		{id: "HT-09", family: "http", title: "a timeout where the route says",
			want: partial, note: "one timeout for the whole router (WithTimeout) with exempt path prefixes (WithTimeoutExempt); a slow export " +
				"and a fast lookup share the same limit unless one is exempted entirely.",
			probe: probePerRouteTimeout},
		{id: "HT-10", family: "http", title: "an unknown route answers a JSON 404 in the framework's envelope",
			want: partial, note: "an unknown path under a module's prefix answers 404 with Go's plain-text \"404 page not found\", not the " +
				"framework's JSON envelope, even with Accept: application/json — a client reading errors by the envelope reads " +
				"nothing.",
			probe: probeUnknownRouteJSON},
		{id: "HT-11", family: "http", title: "the raw-HTML writer is named as such; HTML renders a template",
			want: absent, note: "nucleus.Context.HTML(code, html) writes a raw string while router.Context.HTML(status, template, data) renders " +
				"a template: same name, two meanings (NU-41). There is no RawHTML.",
			probe: probeRawHTMLNamed},
		{id: "HT-12", family: "http", title: "one error envelope: a domain error and the router's 404 share a shape",
			want: absent, note: "a domain error answers {error: {code, message}} and the router's own 404 answers plain text (HT-10): two " +
				"shapes for one client.",
			probe: probeErrorEnvelopeConsistent},

		// ---- di ----------------------------------------------------------
		{id: "DI-01", family: "di", title: "modules receive a typed runtime: services by method, not by key",
			want: present, probe: probeTypedRuntime},
		{id: "DI-02", family: "di", title: "a module provides a service another module consumes typed",
			want: absent, note: "ServiceRegistration is a background runner (Run, Health); nothing lets module A hand module B a typed value, " +
				"so B reaches for a package-level variable or a string key in the request context.",
			probe: probeModuleProvidesService},
		{id: "DI-03", family: "di", title: "request-scoped values are typed",
			want: partial, note: "Context.Set(key string, v interface{}) and Get(key) interface{}: a keyed store with a type assertion at every " +
				"read; no typed accessor.",
			probe: probeRequestScopedTyped},
		{id: "DI-04", family: "di", title: "start order follows declared dependencies between modules",
			want: absent, note: "Module.Requires names DATABASE aliases, not modules, and modules start in name order (sortedModuleSpecs): a " +
				"module that needs another's OnStart to have run renames itself or hopes.",
			probe: probeStartOrderDeclared},
		{id: "DI-05", family: "di", title: "a failed OnStart shuts down the modules already started",
			want: absent, note: "when module B's OnStart fails, module A — started before it — never sees its OnShutdown (NU-44, documented in " +
				"the source as a follow-up): whatever A opened stays open while the process reports the failure.",
			probe: probeShutdownOnStartFailure},
		{id: "DI-06", family: "di", title: "application-level hooks run around the modules' hooks, in order",
			want: present, probe: probeAppHooksAroundModules},
		{id: "DI-07", family: "di", title: "constructors report bad input as an error, never a panic",
			want: absent, note: "auth.NewJWTManager panics on a short secret while NewJWTManagerFromKeys returns an error: two constructors for " +
				"one type with two contracts, the case NU-41 names; the style guide it asks for is not written.",
			probe: probeConstructorsDontPanic},
		{id: "DI-08", family: "di", title: "a module declares a typed configuration and receives it typed",
			want: present, probe: probeTypedModuleConfig},
		{id: "DI-09", family: "di", title: "a module's configuration is bound from the config file under modules.<name>",
			want: present, probe: probeModuleConfigBound},
	}
}
