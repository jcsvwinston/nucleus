# Migration Assistant: `router.NewWrapResponseWriter(w, protoMajor)` → `router.Wrap(w)`

- ID: `MA-2026-023`
- Pairs with: `docs/deprecations/DEP-2026-023-newwrapresponsewriter-replaced-by-wrap.md`
- Severity: `low` (mechanical rename; the old constructor calls the new one).
- Status: `current`

---

## Scope

Any Go code that calls `router.NewWrapResponseWriter`. Known: Orbit's
`internal/admin/audit.go` and `live.go`.

## Detection

```bash
grep -rn 'NewWrapResponseWriter(' --include='*.go' .
staticcheck ./...   # SA1019 on the marker
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| `router.NewWrapResponseWriter(w, r.ProtoMajor)` | `router.Wrap(w)` | automatic |

```bash
sed -i '' -E 's/NewWrapResponseWriter\(([^,]+), [^)]+\)/Wrap(\1)/g' $(grep -rl 'NewWrapResponseWriter(' --include='*.go' .)
gofmt -l .
```

## Verification

```bash
go build ./... && go vet ./... && go test ./...
```

## Rollback

The old constructor keeps working until v2.0.0; revert the `sed`.
