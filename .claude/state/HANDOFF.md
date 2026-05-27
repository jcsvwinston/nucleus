# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    ADR-010 §2 layer 4 — referential validation — COMPLETE (committed + archived). No active iteration.
BRANCH:       main (ahead of origin/main by the layer-4 commits until pushed). NOT clean — see NOTES: separate uncommitted ADR-012 work remains in the tree, deliberately left for the owner.
LAST COMMIT:  a8cf810 feat(nucleus): config referential validation (ADR-010 §2 layer 4)  (+ the chore(state) close commit carrying this file).
STATUS:       Layer 4 shipped: validateReferential (smtp_host/smtp_port↔mail_driver; session_cookie_samesite=none↔session_cookie_secure) wired after validateSemantics at both FromConfigFile + Run; validateModuleRequires fulfils the long-unimplemented ADR-010 §6 boot guarantee (a module's Requires() aliases must be configured databases) — runs at Run only. New exported sentinel ErrInvalidConfigReference (freeze +1, additive). Architect caught + I fixed a real bug: Run now NormalizeRuntimeConfigs before validating so the synthesised "default" alias resolves in the direct-struct path (regression test added). Loop all green (architect PASS after fix, code-reviewer NITS addressed, security-auditor PASS, contract-guardian PASS, test-runner PASS). Docs: ADR-010 §2 layer-4 amendment + status line, API_CONTRACT_INVENTORY, CHANGELOG. Layer 5 (module-specific config binding/validation) is the last remaining validator layer.
NEXT STEP:    Push the layer-4 commits (git push). Then owner decisions, in priority order:
  1. **Deal with the uncommitted ADR-012 work** (see NOTES) — commit it or discard; it's been sitting in the tree.
  2. **Fix the 2 pre-existing red cmd/nucleus tests** (a spawned-task chip was raised) — main is RED on those.
  3. ADR-010 §2 layer 5 (module-specific config binding/validation) — completes the five-layer validator.
  4. P1 WithoutDefaults() admin-bootstrap leak (pkg/app/app.go:~272); P2 Resource("") panic (pkg/nucleus/router.go).
  (NOTE: the prior handoff's "tag v0.6.0" recommendation was STALE — v0.6.0 AND v0.7.0 are already tagged. A /release-prep + tag v0.8.0 pass is the real release option, given the large arc shipped since v0.7.0.)
BLOCKERS:     none.
FILES OF INTEREST:
  - pkg/nucleus/validate_referential.go — the layer-4 validators.
  - pkg/nucleus/nucleus.go — wiring + the NormalizeRuntimeConfig-before-validate fix in Run.
  - docs/iterations/2026-05-26-adr010-layer4-referential-validation.md — this iteration's archive.

NOTES:
  - **UNCOMMITTED, NOT MINE — ADR-012 (Prometheus metrics exporter) work** sits in the working tree and was deliberately excluded from the layer-4 commits (staged by explicit path, never `git add -A`): `contracts/firewall_test.go` (prometheus forbidden-import entries), `docs/adrs/ADR-012-prometheus-metrics-exporter.md` (new/untracked), `docs/adrs/ADR-001-stdlib-first.md`, `docs/governance/CI_MATRIX.md`. It appears complete and passing (`go test ./contracts/...` green with it present). Owner: commit it as its own change or discard — do not let it ride into an unrelated commit.
  - **PRE-EXISTING red tests (NOT caused by layer-4):** `cmd/nucleus` `TestRun_NewProjectSupportsTemplateFlag` and `TestRun_OpenAPIExport` fail on main — leftover from the 2026-05-25 skeleton scaffolder (no more `cmd/server/main.go`; openapi title differs). Verified they fail with the layer-4 change stashed. A spawn-task chip was raised to fix them.
  - Layer-4 deferred (forward-compat, no closed member set today): the §2 "auth chain references valid providers" and "observability exporters resolve" referential checks.

Updated: 2026-05-26
