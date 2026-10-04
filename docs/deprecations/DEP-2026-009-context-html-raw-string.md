# Deprecation Notice: `nucleus.Context.HTML(code, html)` → `RawHTML` (raw string) and `Render` (template)

- ID: `DEP-2026-009`
- Status: `active`
- Announced in: `Unreleased` (the first release after 2026-10-04)
- Earliest removal: `v2.0.0` — major-only, no earlier than `2027-01-04`
  (stable surfaces are not removed in `v1.x`; the suite groups its breaking
  changes in one major)
- Scope: `api`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

`nucleus.Context` embeds `*router.Context`, and both have a method called
`HTML` with different signatures and different jobs:

- `router.Context.HTML(status int, templateName string, data map[string]interface{})`
  renders a named template through the application's template engine;
- `nucleus.Context.HTML(code int, html string)` writes a raw string as the
  response body, unescaped, and shadows the template render.

One name, two meanings. A handler author who reads the router's
documentation writes `c.HTML(200, "page.html", data)` in a module handler and
gets a compile error; one who reads the module documentation and writes
`c.HTML(200, userInput)` is writing unescaped markup with a method whose
name, everywhere else in the framework, means "render a template". The
generated code had to spell `c.Context.HTML(...)` to reach the template
render (the maturity audit of 2026-09-03 recorded it as NU-41; the API bench
of arc A10 measured it as `HT-11`).

The two jobs now have two names on `nucleus.Context`:

- `RawHTML(code int, html string) error` — the raw writer, unchanged in
  behaviour;
- `Render(code int, templateName string, data map[string]interface{}) error`
  — the template render (it already existed).

`nucleus.Context.HTML(code, html)` is deprecated and now forwards to
`RawHTML`. It keeps working, byte for byte, until the next major.

`router.Context.HTML` (the template render on the lower-level router) is
**not** deprecated: on `router.Context` the name has one meaning.

## Affected Surfaces

- `github.com/jcsvwinston/nucleus/pkg/nucleus.Context.HTML` (method).

## Migration Path

- Replacement: `c.RawHTML(code, html)` where the argument is an HTML string;
  `c.Render(code, name, data)` where a template was meant.
- Behavior differences: none — `HTML` forwards to `RawHTML`; same status, same
  `Content-Type: text/html; charset=utf-8`, same body.
- Required app changes: rename the call. No configuration or operational
  change.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-009-context-html-to-rawhtml.md`
- Detection rule: a call `c.HTML(<code>, <string>)` on a `*nucleus.Context`
  (two arguments); `staticcheck` SA1019 and gopls flag every use once the
  marker ships.
- Suggested rewrite: automatic — `HTML(` → `RawHTML(` on two-argument calls
  on a `*nucleus.Context`.

## Validation

- Compatibility tests updated: `yes` — `RawHTML` and the deprecated `HTML`
  are both exercised through a booted application
  (`pkg/nucleus/http_surface_e2e_test.go`); the API bench's `HT-11` calls
  `RawHTML` over HTTP and reads the deprecation marker on `HTML`; the frozen
  symbol baseline lists `Context.RawHTML` as an addition and keeps
  `Context.HTML`.
- Release note updated: pending merge (the conventional commit feeds the
  release notes).
- Rollback plan documented: `yes` — both methods coexist until v2.0.0;
  reverting a call site restores the prior state.

## Timeline

- Announcement date: `2026-10-04`
- Review checkpoint: the first release that ships `RawHTML` — confirm the
  marker reaches `go doc` and the deprecation diagnostics fire.
- Removal decision date: at the preparation of `v2.0.0`, no earlier than
  `2027-01-04`.

## Notes

The other half of NU-41 — constructors that panic on bad input
(`auth.NewJWTManager`) and loggers in inconsistent positions — is a separate
change with its own notice; this one covers the method name only.
