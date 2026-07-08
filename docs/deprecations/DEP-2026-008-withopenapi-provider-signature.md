# Deprecation Notice: provider-typed OpenAPI mounts → stdlib `http.Handler`

- ID: `DEP-2026-008`
- Status: `active`
- Announced in: `Unreleased` (v1 gate A-1a decision, 2026-07-08)
- Earliest removal: `v0.12.0` (pre-`v1.0` removal, exception-only per
  `docs/governance/DEPRECATION_TEMPLATE.md` — maintainer approval recorded in
  `docs/V1_GATE.md` §A-1; same train as DEP-2026-004/005/006)
- Scope: `api`
- Affected lifecycle tag: `stable` (the deprecated members) referencing
  `experimental` (`pkg/openapi`)
- Owner: `@jcsvwinston`

## Summary

Three stable members name the experimental `openapi.DocumentProvider` type:
`AppBuilder.WithOpenAPI`, `nucleus.OpenAPISpec.Provider`, and
`app.App.MountOpenAPI`. A stable surface referencing an experimental type is
not a tenable v1.0 shape (v1 gate A-1a): promoting `DocumentProvider` would
drag the entire `*openapi.Document` model (~40 exported symbols, 2 files,
explicitly experimental) into the freeze, and `pkg/openapi` is not mature
enough to freeze.

The maintainer decision (2026-07-08) is to **re-sign the surface to stdlib**:

- New, stable, stdlib-only members shipped in `v0.11.0`:
  `AppBuilder.WithOpenAPIHandler(pattern string, handler http.Handler)`,
  `nucleus.OpenAPISpec.Handler http.Handler`, and
  `app.App.MountOpenAPIHandler(pattern string, handler http.Handler)`.
  The adapter already existed: `openapi.Handler(provider)` converts a
  document factory into an `http.Handler`, so the DX cost is one call.
- The provider-typed members are deprecated in `v0.11.0` and removed in
  `v0.12.0` with a deliberate freeze rebaseline. `pkg/openapi` itself stays
  `experimental` and is documented as **outside the v1.0 promise**
  (inventory + release notes), free to evolve behind the stdlib boundary.

When both `OpenAPISpec.Handler` and the deprecated `Provider` are set,
`Handler` wins (documented on the struct).

## Affected Surfaces

- `nucleus.AppBuilder.WithOpenAPI` → use
  `WithOpenAPIHandler(pattern, openapi.Handler(provider))`.
- `nucleus.OpenAPISpec.Provider` → use `OpenAPISpec.Handler`.
- `app.App.MountOpenAPI` → use
  `MountOpenAPIHandler(pattern, openapi.Handler(provider))`.
  (`MountOpenAPI` already delegates to the handler path since this notice.)
- `pkg/openapi` is NOT deprecated — it remains the convenient experimental
  toolkit for building documents; only the stable surface stops naming its
  types.

## Migration Path

- Replacement: mechanical one-line rewrite at each call site —
  `WithOpenAPI(p, provider)` → `WithOpenAPIHandler(p, openapi.Handler(provider))`,
  same for the mount; direct-struct users move
  `OpenAPI: &OpenAPISpec{Pattern: p, Provider: f}` to
  `OpenAPI: &OpenAPISpec{Pattern: p, Handler: openapi.Handler(f)}`.
- Behavior differences: none — the deprecated paths already route through
  the same handler mount; the endpoint output is byte-identical.
- Beyond parity, the stdlib signature admits non-provider sources
  (pre-rendered JSON, embedded files, proxies) without touching
  `pkg/openapi`.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-008-withopenapi-to-handler.md`
- Detection rule: use of `WithOpenAPI(`, `MountOpenAPI(`, or
  `OpenAPISpec{...Provider:` in Go source; `staticcheck`/gopls deprecation
  diagnostics after upgrading to v0.11.0.
- Suggested rewrite: wrap the existing provider with `openapi.Handler`.

## Validation

- Compatibility tests updated: `yes` — the three new stdlib members were
  added to `contracts/baseline/api_exported_symbols.txt` via the intentional
  refresh workflow (additions only; removals wait for v0.12.0's deliberate
  rebaseline). Builder semantics covered by new tests (record/nil/last-wins
  across both setters).
- Release note updated: pending merge (conventional commit feeds
  release-please notes).
- Rollback plan documented: `yes` — both surfaces coexist until v0.12.0;
  reverting a call site restores the prior state.

## Timeline

- Announcement date: `2026-07-08`
- Review checkpoint: at `v0.11.0` release prep — confirm both surfaces ship
  and the deprecation diagnostics fire.
- Removal decision date: `v0.12.0` release prep — remove the three
  provider-typed members, rebaseline deliberately, close v1 gate A-1a's
  removal step.
