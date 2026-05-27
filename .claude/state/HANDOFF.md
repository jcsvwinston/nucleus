# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Get main CI green, then cut v0.8.0 (in progress — parked at clean checkpoint).
BRANCH:       main. TWO LOCAL UNPUSHED COMMITS — do NOT push until go test ./... is green.
LAST COMMIT:  21a4762 (pushed HEAD). Local unpushed: b829855 (ADR-012 + CI_MATRIX fix), then 217fed5 (Error-1064 AutoMigrate fix for MySQL/SQLite).
STATUS:       in progress — 2 of 3 red-CI causes addressed locally; test fixes not yet done.
NEXT STEP:    Fix the 4 stale tests in cmd/nucleus/main_test.go. Requires owner decision: accept module-path-derived OpenAPI title "Contractapp API" (fix the test) OR fix defaultOpenAPITitle in internal/cli/contracts_scaffold.go (~line 88) to preserve the project name (fix production code). Then run go test ./... locally; if green, push b829855 + 217fed5, confirm CI green, resume v0.8.0 release (CHANGELOG promotion, docs/reports/, annotated tag).
BLOCKERS:     MSSQL live-smoke lane not yet root-caused (CI-only, needs container). Cannot push or tag until go test ./... is green.
FILES OF INTEREST: cmd/nucleus/main_test.go, internal/cli/contracts_scaffold.go (~L88), internal/cli/scaffold/templates/, pkg/db/exec_script.go, docs/adrs/ADR-012-prometheus-metrics-exporter.md
NOTES:
  - v0.7.0 (ed5689b) is the latest published release (nucleus module path). Real next release is v0.8.0 (main is 71 commits past v0.7.0). The stale "tag v0.6.0" recommendation in older handoffs is superseded.
  - main has been RED since ~2026-05-24 because branch protection / required gate is NOT enforced on direct pushes. Governance follow-up: enable it so this cannot recur.
  - The 4 failing tests: TestRun_NewProjectSupportsTemplateFlag (and 2 sibling scaffold-layout tests) assert the REMOVED demo-app layout (cmd/server/main.go, articles migrations, seeds/) broken by f073953 (2026-05-25); TestRun_OpenAPIExport expects title "ContractApp API" but gets "Contractapp API" (skeleton removed the baked-in contracts aggregator; title now derived from module path via defaultOpenAPITitle).
  - Error-1064 fix (217fed5): pkg/db/exec_script.go splits multi-statement AutoMigrate scripts for MySQL and SQLite. code-reviewer PASS; regression-tested on in-memory SQLite.
  - ADR-012 (b829855): prometheus exporter rationale + firewall forbidden-import entries + CI_MATRIX truth-fix. architect-reviewer + contract-guardian PASS.
  - Once CI is green and v0.8.0 is tagged, carry-forward priority: P1 WithoutDefaults() admin-bootstrap leak (pkg/app/app.go:~272), then ADR-010 §2 layer 5.

Updated: 2026-05-27
