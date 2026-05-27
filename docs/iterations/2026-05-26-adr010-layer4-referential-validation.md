# Iteration archive — 2026-05-26 ADR-010 §2 layer 4 (referential validation)

> Archived 2026-05-26 as part of the session-end `/handoff`. Landed as the
> feature commit `a8cf810` (`feat(nucleus): config referential validation
> (ADR-010 §2 layer 4)`) on `main`, followed by the usual `chore(state): close`
> state commit. The penultimate validator layer (layer 5 = module-specific
> remains).

## Goal

Implement ADR-010 §2 layer 4 (referential validation): cross-key consistency
checks where one config key's validity depends on another, plus the
long-documented-but-never-implemented §6 guarantee that a module's `Requires()`
aliases must be configured databases.

## Scope

### In
- `pkg/nucleus/validate_referential.go` (new):
  - `validateReferential(cfg *app.Config) error` — config cross-field checks:
    `mail_driver=smtp` requires `smtp_host` set + `smtp_port>0`;
    `session_cookie_samesite=none` requires `session_cookie_secure=true`.
    Wired immediately after `validateSemantics` in BOTH `FromConfigFile` (load)
    and `Run` (direct-struct), mirroring layer 3.
  - `validateModuleRequires(cfg, map[string]ModuleSpec) error` — every alias a
    module declares in `Requires()` must be a key in `cfg.Databases`, failing
    with the ADR-§6 message `module "<name>" requires database "<alias>" which
    is not configured`. Runs ONLY in `Run` (modules are Mount-ed on the
    builder, not present in the loaded config file).
  - New exported sentinel `ErrInvalidConfigReference` (mirrors layer-3's
    `ErrInvalidConfigValue`).
- `pkg/nucleus/nucleus.go`: wired both checks; `Run` now calls
  `app.NormalizeRuntimeConfig(&cfg)` before validating so the synthesised
  `default` database alias is visible to `validateModuleRequires` (the
  `FromConfigFile` path was already normalised by `loadFromFiles`).
- Freeze baseline rebaselined additively (+1: `var:ErrInvalidConfigReference`).
- Docs: ADR-010 §2 layer-4 amendment + status line; API_CONTRACT_INVENTORY
  `pkg/nucleus` row; CHANGELOG.

### Out (deliberate, deferred)
- The §2 "auth chain references valid providers" and "observability exporters
  resolve" checks — auth providers and exporters are plugin-extensible
  collections with no closed, config-resolvable member set today.

## Key design decision — split timing
Layer 4 runs across two timings, a deliberate deviation from §2's "all five
layers in `FromConfigFile`" framing (recorded in the ADR amendment): the config
cross-field half runs at load + Run; the module-`Requires` half runs only at
`Run`, because modules are registered programmatically on the builder, not in
the config file.

## Acceptance criteria — all met
- [x] Config rejects the inconsistent combinations; §6 Requires guarantee
  enforced with the documented message.
- [x] `go test ./pkg/nucleus/... ./contracts/...` green; gofmt/vet clean;
  freeze additive (+1).
- [x] Loop: architect PASS (after the normalize-timing fix), code-reviewer
  NITS (addressed), security-auditor PASS, contract-guardian PASS, test-runner
  PASS.

## Loop finding — architect WARN, fixed
The architect caught a real bug: in the direct-struct `Run(App{})` path,
`validateModuleRequires` originally ran before `app.New` normalised
`cfg.Databases`, so a module requiring `"default"` with an empty `Databases`
map would falsely fail (the `FromConfigFile` path was fine — `loadFromFiles`
normalises first). Fixed by calling `app.NormalizeRuntimeConfig(&cfg)` at the
top of `Run`, with a regression test
(`default alias resolves after NormalizeRuntimeConfig`).

## Outcome
Landed as `a8cf810`. Config misconfigurations that previously booted into a
broken state (silently-dropped session cookie, mail that never sends, a module
nil-derefing on a missing DB) now fail loud at startup.

## Notes
- The security-auditor endorsed the hard-fail on `SameSite=None + Secure=false`
  (no legitimate deployment needs that pair; browsers drop the cookie). One LOW
  (no warning for `Secure=false` alone) left out of scope.
- PRE-EXISTING and unrelated: 2 `cmd/nucleus` tests are red on main
  (`TestRun_NewProjectSupportsTemplateFlag`, `TestRun_OpenAPIExport`) from the
  2026-05-25 skeleton scaffolder (no more `cmd/server/main.go`); verified they
  fail with this change stashed. Flagged as a separate spawned task.
- ALSO unrelated and left untouched: uncommitted ADR-012 (Prometheus exporter)
  work was sitting in the working tree this session — `contracts/firewall_test.go`,
  `docs/adrs/ADR-012-prometheus-metrics-exporter.md` (new), `docs/adrs/ADR-001-stdlib-first.md`,
  `docs/governance/CI_MATRIX.md`. Deliberately NOT included in the layer-4
  commits; left for the owner to commit separately.
