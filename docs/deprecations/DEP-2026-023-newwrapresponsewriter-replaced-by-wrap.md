# Deprecation Notice: `router.NewWrapResponseWriter(w, protoMajor)` → `router.Wrap(w)`

- ID: `DEP-2026-023`
- Status: `active`
- Announced in: the next minor after `v1.32.0` (A12 `M0-N`; decision D4 of the owner, 2026-10-16)
- Earliest removal: `v2.0.0`, no earlier than 2027-01-17 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md` and the suite's deprecation policy.
- Scope: `api`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

`router.NewWrapResponseWriter(w http.ResponseWriter, protoMajor int)` wraps a
response writer to record the status code and the bytes written. Its second
argument was accepted for compatibility with chi's constructor of the same
name and has never been read (NU-41, audit of 2026-09-03). A parameter a
function ignores is a promise it does not keep; the replacement takes only
the writer.

This release adds `router.Wrap(w)` and marks the old constructor
`// Deprecated:`; the framework's own callers (the logging middleware, the
OpenTelemetry middleware and the HTTP observability hook) use `Wrap`.

## Affected Surfaces

- `github.com/jcsvwinston/nucleus/pkg/router.NewWrapResponseWriter`.
- Known external caller: Orbit (`internal/admin/audit.go`, `live.go`), which
  migrates in its 1.x minor (`M0-O` of the A12 arc).
- Not affected: `WrapResponseWriter` itself and its methods (`Status`,
  `BytesWritten`, `WroteHeader`, `Unwrap`), which do not change.

## Migration Path

- Replacement: `router.Wrap(w)`.

  ```go
  // before
  ww := router.NewWrapResponseWriter(w, r.ProtoMajor)
  // after
  ww := router.Wrap(w)
  ```

- Behavior differences: none — the old constructor calls the new one.
- Required app changes: the call site.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-023-wrap-response-writer.md`
- Detection rule: `grep -rn 'NewWrapResponseWriter(' --include='*.go'`;
  staticcheck SA1019 on the marker.
- Suggested rewrite: automatic —
  `sed -E 's/NewWrapResponseWriter\(([^,]+), [^)]+\)/Wrap(\1)/g'`.

## Validation

- Compatibility tests updated: `yes` — `TestWrap_RecordsStatusAndBytes`
  (`pkg/router/wrap_test.go`) pins that both constructors record the same
  status and byte count; the pre-existing `TestWrapResponseWriter_TracksWroteHeader`
  keeps calling the old form until it is removed. `Wrap` is listed in
  `contracts/baseline/api_exported_symbols.txt` (additions only).
- Release note updated: through the squash commit.
- Rollback plan documented: `yes` — the old constructor keeps working until
  v2.0.0.

## Timeline

- Announcement date: `2026-10-16`
- Review checkpoint: `2027-01-17`, when the window opens.
- Removal decision date: the `v2.0.0` major.

## Notes

The umbrella guard `scripts/check_deprecations.sh` reads the
`// Deprecated:` marker on the constructor (it cites this notice, the
version and the date) and the suite's list of the major
(`quantum/docs/planes/quantum-2.0.md` §3.0, row 25). `authz.New` was named
with this constructor in the audit; the owner decided (D4) that its signature
stays.
