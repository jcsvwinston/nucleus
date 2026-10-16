# Migration Assistant: `nucleustest` without an implicit SQLite → import the driver your test uses

- ID: `MA-2026-019`
- Pairs with: `docs/deprecations/DEP-2026-019-nucleustest-links-sqlite.md`
- Severity: `low` before v2.0.0 (nothing changes); `low` at v2.0.0 (a test
  binary without a driver fails at its first `TempSQLite`-backed call with
  the import named, and one import line fixes it).
- Status: `current`

---

## Scope

Test packages that import `github.com/jcsvwinston/nucleus/pkg/nucleustest`
and call `TempSQLite`, `CheckModule` or `CheckModuleIn` without importing a
database driver module. Today `nucleustest` links `modernc.org/sqlite`
itself; at v2.0.0 it does not.

Out of scope: tests that already import `drivers/sqlite` or another engine's
driver; application code.

## Detection

```bash
# Test packages that use nucleustest …
grep -rl 'nucleus/pkg/nucleustest"' --include='*_test.go' . | xargs -n1 dirname | sort -u > /tmp/uses
# … and import no driver module anywhere in the package.
while read -r d; do grep -q 'nucleus/drivers/' "$d"/*_test.go "$d"/*.go 2>/dev/null || echo "$d"; done < /tmp/uses
```

At v2.0.0 the failure itself detects it: `nucleustest: no "sqlite" driver is
registered — import _ "github.com/jcsvwinston/nucleus/drivers/sqlite" in a
test file of this package`.

## Rewrite

| Before | After | Kind |
|---|---|---|
| a `_test.go` package with `nucleustest` and no driver import | the same package with `import _ "github.com/jcsvwinston/nucleus/drivers/sqlite"` in one of its test files | manual (one line) |
| a test that wants another engine | `import _ "github.com/jcsvwinston/nucleus/drivers/<engine>"` and a `Databases` map pointing at it instead of `TempSQLite` | manual |

## Verification

```bash
go test ./...                       # the package's tests pass with the import in place
go list -deps -test ./<pkg> | grep -c modernc.org/sqlite   # 1 where the test asked for SQLite; 0 elsewhere at v2.0.0
```

## Rollback

Remove the import line. Before v2.0.0 the helper links SQLite anyway.
