# Migration Assistant: provider-typed OpenAPI mounts → `http.Handler`

- ID: `MA-2026-008`
- Pairs with: `docs/deprecations/DEP-2026-008-withopenapi-provider-signature.md`
- Severity: `low` — mechanical one-line rewrite per call site; the adapter
  (`openapi.Handler`) already exists and the endpoint output is
  byte-identical. Deprecated members keep working until removal.
- Status: `current`

---

## Scope

Applications that mount an OpenAPI document endpoint through any of the
three provider-typed members: `AppBuilder.WithOpenAPI`,
`nucleus.OpenAPISpec.Provider` (direct-struct surface), or
`app.App.MountOpenAPI`. At v0.12.0 the three are removed; the stdlib
`http.Handler` members replace them.

Out of scope: `pkg/openapi` itself — the document model and helpers stay
available (experimental, outside the v1.0 promise); only the stable
surface stops naming its types.

## Detection

**Go source — search for the deprecated members:**

```bash
# From the consumer repo root.
grep -rn "WithOpenAPI(\|MountOpenAPI(\|Provider:.*DocumentProvider\|OpenAPISpec{" --include="*.go" .
```

**Compiler tooling:** after upgrading to v0.11.0, `staticcheck` (SA1019)
and gopls flag every deprecated use.

## Rewrite

```go
// before (deprecated, removal at v0.12.0)
nucleus.New().WithOpenAPI("/openapi.json", contracts.NewDocument)

// after — wrap the same provider with the existing adapter
nucleus.New().WithOpenAPIHandler("/openapi.json", openapi.Handler(contracts.NewDocument))
```

Direct-struct surface:

```go
// before
app.OpenAPI = &nucleus.OpenAPISpec{Pattern: "/openapi.json", Provider: contracts.NewDocument}

// after
app.OpenAPI = &nucleus.OpenAPISpec{Pattern: "/openapi.json", Handler: openapi.Handler(contracts.NewDocument)}
```

Core container surface: `a.MountOpenAPI(p, provider)` →
`a.MountOpenAPIHandler(p, openapi.Handler(provider))`.

The stdlib signature also admits non-provider sources — pre-rendered JSON
(`http.ServeContent`), an embedded file, or a proxy — with no `pkg/openapi`
dependency at all.

## Rollback

Both surfaces coexist until v0.12.0; reverting the call-site rewrite
restores the prior state. No data or config involved.

## Validation

After the rewrite, boot the app and confirm:

1. `GET /openapi.json` (or the configured pattern) returns the same
   document as before (byte-identical for the same provider);
2. no deprecation diagnostics remain (`staticcheck ./...`).
