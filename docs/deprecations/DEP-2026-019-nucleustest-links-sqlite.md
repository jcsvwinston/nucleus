# Behaviour-Change Notice: `nucleustest` stops linking SQLite into every test binary at v2.0.0

- ID: `DEP-2026-019`
- Status: `active`
- Announced in: the next minor after `v1.32.0` (A12 `M0-N`, decided 2026-10-16)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-17 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc (QADR-0010). This is a
  **packaging change of a test helper**, not a symbol removal: no
  `// Deprecated:` marker carries it.
- Scope: `api` (test support)
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

`pkg/nucleustest` imports `modernc.org/sqlite` for its side effect
(`runtime.go`), so `TempSQLite` and `CheckModule` work in a test binary that
imports no database driver of its own. The price is paid by every test
binary that imports `nucleustest`: it links the pure-Go SQLite engine whether
the application under test uses SQLite or not — measured in A12 `N1` as the
one dependency `nucleustest` carries into production code (NU-121). The
framework moved its own tests off that import in `v1.32.0`
(`internal/testsqlite`); the public helper could not follow without
breaking the tests that rely on it, so it waits for the major.

What changes and when:

- **This release:** nothing. `nucleustest` keeps linking SQLite; this notice
  and the pinned test say so.
- **v2.0.0:** `nucleustest` imports no driver. `TempSQLite` keeps existing and
  returns the same configuration; opening it fails with a message that names
  the import to add when no `sqlite` driver is registered. A test that uses
  another engine imports that engine's driver module instead and links no
  SQLite.

## Affected Surfaces

- Test binaries that import `github.com/jcsvwinston/nucleus/pkg/nucleustest`
  and call `TempSQLite`, `CheckModule` or `CheckModuleIn` without importing
  `github.com/jcsvwinston/nucleus/drivers/sqlite` (or registering a `sqlite`
  driver some other way).
- Not affected: tests that already import a driver module; applications
  (`nucleustest` is test support).
- No Go API is removed or changes shape.

## Migration Path

- Replacement: add the blank import in one `_test.go` file of each package
  that uses the helper:

  ```go
  import _ "github.com/jcsvwinston/nucleus/drivers/sqlite"
  ```

  A test written this way behaves the same before and after 2.0.
- Behavior differences: none in v1. At v2.0.0 a test binary without the
  import fails at the first `TempSQLite`-backed call with the import named.
- Required app changes: one import line per test package that relies on the
  helper's implicit driver.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-019-nucleustest-import-your-driver.md`
- Detection rule: a test package that imports `pkg/nucleustest` and no
  `drivers/*` module (`grep -L 'nucleus/drivers/' $(grep -l 'pkg/nucleustest' -r --include='*_test.go' .)`).
- Suggested rewrite: manual — the blank import above.

## Validation

- Compatibility tests updated: `yes` —
  `TestImportingNucleustestLinksSQLiteInV1`
  (`pkg/nucleustest/sqlite_linked_v1_test.go`) pins that importing
  `nucleustest` alone registers the `sqlite` driver and that `TempSQLite`
  works without any other import. The 2.0 PR changes it in the same commit
  as the import.
- Release note updated: through the squash commit, which feeds the
  release-please notes.
- Rollback plan documented: `yes` — before v2.0.0 nothing to roll back;
  after it, adding the import restores a passing test binary.

## Timeline

- Announcement date: `2026-10-16`
- Review checkpoint: `2027-01-17`, when the window opens.
- Behaviour flip decision date: the `v2.0.0` major.

## Notes

The umbrella guard `scripts/check_deprecations.sh` reads this notice through
its `Status` and «no earlier than» lines and the suite's list of the major
(`quantum/docs/planes/quantum-2.0.md` §3.0, row 21).
