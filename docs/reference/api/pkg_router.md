# pkg/router — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Router construction, middleware hooks, unified request context helpers (`Context`, `ContextHandler`), rendering/binding/pagination helpers; rate-limit middleware keys per-tenant when a tenant is resolved in context

## Notes

Request/response helper behavior is contract surface.

`CSRFMiddleware`, which panics on a misconfiguration, is deprecated toward `NewCSRFMiddleware`, which returns it as an error (DEP-2026-012, removal in v2.0.0); the default stack (`csrf_enabled`) builds its middleware without the panicking wrapper.

Typed binding (A10 S8): `BindQuery`, `BindPath`, `BindHeaders` and
`BindRequest` bind by the `query:"name"`, `path:"name"`, `header:"X-Name"`
and (for `BindRequest`'s body) `json` tags, convert with `BindForm`'s
converter plus `encoding.TextUnmarshaler` and slices, and validate once; a
conversion failure is a 400 `BAD_REQUEST` naming the parameter, a validation
failure the 422 `VALIDATION_FAILED` naming it as the client sent it. The tag
names are contract: the OpenAPI derivation reads the same tags.

One error shape: handler errors, binding failures, the router's own 404/405
(for a client that prefers JSON; a browser keeps Go's plain text), the
request timeout (503 `TIMEOUT`), the CSRF and rate-limit refusals and
`errors.WriteError` answer the envelope
`{"error": {"code", "message", "details"}}` by default and RFC 9457 problem
details (`errors.Problem`) when the client prefers
`application/problem+json` or the router was built `WithProblemDetails(true)`.
`HTTPError` and unclassified 500s keep their `{"error": "<message>"}` shape
in the envelope mode until the next major. `Negotiate` picks JSON, XML or
plain text by `Accept` (406 `NOT_ACCEPTABLE` when none fits). `Timeout(d)`
moves the request deadline for a route, longer or shorter, and the
connection's write deadline with it. `APIVersion` / `Mux.Version` mount a
version under `/<Name>` and send `Deprecation` (RFC 9745), `Sunset` (RFC 8594)
and `Link` rel="successor-version"/"deprecation".
