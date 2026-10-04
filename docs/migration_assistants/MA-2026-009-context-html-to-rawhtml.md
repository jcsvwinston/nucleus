# Migration Assistant: `nucleus.Context.HTML(code, html)` → `RawHTML(code, html)`

- ID: `MA-2026-009`
- Pairs with: `docs/deprecations/DEP-2026-009-context-html-raw-string.md`
- Severity: `low` — a mechanical rename; the deprecated method forwards to
  the new one and the response is byte-identical. The old name keeps working
  until v2.0.0.
- Status: `current`

## Scope

Handlers written against `*nucleus.Context` (module handlers, fluent-mode
handlers) that call `HTML` with two arguments — a status code and an HTML
string:

```go
return c.HTML(http.StatusOK, "<p>hello</p>")
```

Out of scope:

- `router.Context.HTML(status, templateName, data)` — the template render on
  the lower-level router. It is not deprecated. Code that reaches it from a
  `nucleus.Context` as `c.Context.HTML(status, name, data)` keeps working;
  `c.Render(status, name, data)` is the shorter spelling of the same call.
- `html/template.HTML`, the type, which is unrelated.

## Detection

A consumer is impacted when a `*nucleus.Context` value calls `HTML` with two
arguments.

```bash
# Candidates: two-argument HTML calls (review each — a three-argument call
# is the router's template render and is not affected).
rg -n '\.HTML\(\s*[^,()]+,\s*[^,()]+\)' --type go .

# After upgrading, the compiler tooling names every use:
staticcheck ./...        # SA1019: nucleus.Context.HTML is deprecated
```

## Rewrite Plan

| Surface (before) | Surface (after) | Note |
|------------------|-----------------|------|
| `c.HTML(code, html)` on a `*nucleus.Context` | `c.RawHTML(code, html)` | identical behaviour |
| `c.Context.HTML(code, name, data)` from a `*nucleus.Context` | `c.Render(code, name, data)` (optional) | same call; not deprecated |

Automatic rewrite candidates:

- `gofmt -r 'x.HTML(a, b) -> x.RawHTML(a, b)' -w .` — single-letter
  identifiers in a gofmt rewrite rule are wildcards, so it matches every
  two-argument `HTML` call whatever the receiver is called. A receiver that
  is not a `*nucleus.Context` has no `RawHTML`, and the compiler rejects the
  rewrite there, which makes a wrong match visible immediately; run
  `go build ./...` right after.

Manual steps:

- None.

## Verification

```bash
go build ./...
go vet ./...
staticcheck ./...   # no SA1019 for nucleus.Context.HTML remains
go test ./...
```

## Rollback

Both methods coexist until v2.0.0: reverting the rename restores the prior
state. No data or configuration is involved.

## Compatibility Notes

Additive-first: `RawHTML` is new; `HTML` keeps its signature and behaviour
(it forwards to `RawHTML`) and is removed no earlier than 2027-01-04, in
v2.0.0.
