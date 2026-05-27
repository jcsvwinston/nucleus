# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Get main CI green, then cut v0.8.0 (in progress — local tree green + pushed; awaiting CI confirmation before the release).
BRANCH:       main (clean, in sync with origin/main).
LAST COMMIT:  bf7b881 test(cli): update stale scaffold/openapi tests to the skeleton layout (preceded by d5c6203 fix(app): don't panic on empty/malformed templates dir). Both pushed.
STATUS:       `go test ./...` is GREEN locally and pushed. The 3 red-CI causes are all addressed: ADR-012 (b829855) + Error-1064 (217fed5) landed earlier; this session fixed the 4 stale cmd/nucleus tests (bf7b881) and a REAL app.New startup panic they surfaced — template.Must(ParseGlob) crashed when TemplatesDir existed but had no .html (the skeleton/generated-project case); now parses only when ≥1 file matches and returns parse errors via wrapOp instead of panicking (d5c6203, code-reviewer NITS addressed, new pkg/app/app_templates_test.go). Owner decision on the OpenAPI title: accepted the module-path-derived "Contractapp API" (test change, no production change).
NEXT STEP:    1. Confirm CI is green on bf7b881 — the `Test And Smoke` and `DB Matrix Required (mysql)` lanes especially (the Error-1064 fix). 2. If green, run scope #4 — the parked v0.8.0 release: promote CHANGELOG `[Unreleased] → [0.8.0] - <date>` + a `### Compatibility statement`, regenerate `docs/reports/`, then an annotated `v0.8.0` tag (matching the v0.7.0 convention) + push (triggers `release.yml`). Owner go-ahead needed before tagging.
BLOCKERS:     MSSQL live-smoke lane is CI-only (needs an Oracle/MSSQL container) — not root-caused locally; confirm green or note as a flake during the CI check.
FILES OF INTEREST:
  - pkg/app/app.go (the template-init fix in New) + pkg/app/app_templates_test.go.
  - cmd/nucleus/main_test.go (the 4 updated scaffold/openapi tests).
  - .claude/state/CURRENT_ITERATION.md (full scope/acceptance + carry-forward backlog: P1 WithoutDefaults admin-bootstrap leak, P2 Resource("") panic, ADR-010 §2 layer 5).
NOTES:
  - The 4 stale tests were leftover from the 2026-05-25 skeleton scaffolder (no more cmd/server/main.go; demo removed from core). The fix also revealed + fixed the app.New empty-templates-dir panic — another framework gap surfaced by the skeleton reality (same class as P1/P2).
  - Governance follow-up still open: enable branch protection / required gate on main so red CI cannot be pushed (main has been red since ~2026-05-24 because direct pushes bypass the gate).
  - After v0.8.0 ships, carry-forward priority: P1 WithoutDefaults() admin-bootstrap leak (pkg/app/app.go:~272), then ADR-010 §2 layer 5 (module-specific config validation — last of the five validator layers).

Updated: 2026-05-27
