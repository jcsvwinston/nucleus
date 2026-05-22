# Iteration archive — 2026-05-22 pkg/observability lifecycle (experimental)

> Archived 2026-05-22 as part of the session-end `/handoff`. Landed as the
> feature commit `9227e7d` (`docs(contracts): classify
> pkg/observability(+hooks) as experimental`) on `main`, followed by the
> usual `chore(state): close` state commit. Candidate #1 from the prior
> queue (surfaced by architect-reviewer 2026-05-21 / 2026-05-22 during the
> shared-registry and nested-coverage iterations).

## Goal

Give `pkg/observability` and `pkg/observability/hooks` their first
`API_CONTRACT_INVENTORY.md` rows and a real lifecycle decision, replacing the
`lifecycleUninventoried` placeholder they carried in the contract registry.

## Scope

### In

- `docs/reference/API_CONTRACT_INVENTORY.md`: two `experimental` rows added
  (after `pkg/observe`):
  - `pkg/observability` — the in-process observability fan-out bus (`Bus`,
    `NewBus`, `Subscription`, `SubscribeOptions`, `Stats`, generic
    `RingBuffer[T]`, the `Event` interface and typed events with pooled
    `Acquire*` constructors, `EventKind`/`SessionChangeKind`, `Filter`,
    `DefaultSubscriberChannelSize`).
  - `pkg/observability/hooks` — the instrumentation bridges
    (`NewHTTPMiddleware`, `NewSQLObserver`, `NewSessionRecorder` + configs).
  Both documented as internal-facing plumbing for the admin observability
  agent, with no compatibility guarantee yet.
- `contracts/packages_test.go`: both rows flipped `lifecycleUninventoried` →
  `lifecycleExperimental` (still `frozen:false`, `firewalled:false`); header
  comment + notes updated. `lifecycleUninventoried` kept as the documented
  placeholder for future newly-discovered packages.

### Out

- No relocation of `pkg/observability` to `internal/` (a post-v1.0 backlog
  note — see follow-ups).
- No godoc lifecycle annotations (the inventory is authoritative).

## Decision

The owner chose `experimental` over inventing a new `internal-facing`
lifecycle tag. Rationale: a new tag would expand the inventory taxonomy and
the registry enum — a governance change, arguably an ADR — whereas
`experimental` already means "public, no compatibility guarantee, may change
before v1.0" and matches the `pkg/openapi` precedent. The "internal-facing"
nuance is captured in the row notes instead.

## Acceptance criteria — all met

- [x] Behaviour-preserving: `contracts/baseline/` untouched; freeze set still
  17; firewall set still 20.
- [x] `go test ./...` green; `gofmt`/`go vet` clean; contract-freeze script
  passes.
- [x] `TestPublicPackages_FrozenMatchesLifecycle` holds (experimental →
  frozen:false).
- [x] Loop: architect PASS, code-reviewer PASS (wording nits addressed),
  contract-guardian PASS, test-runner PASS.

## Outcome

Landed as feature commit `9227e7d`. After this, no package in the registry
carries `uninventoried` — every public `pkg/*` package (top-level and nested)
has a deliberate, inventory-backed lifecycle.

## Follow-ups opened / carried

- **Relocate `pkg/observability` to `internal/` post-v1.0** (architect-reviewer
  2026-05-22) rather than ever promoting it to `stable` — it is internal-facing
  plumbing; `experimental` buys time, relocation is the eventual right move.
  Parked under the Phase 4 modularization candidate.
- Two optional reviewer cleanups carried from the nested-coverage iteration
  (double `os.ReadDir`; dead `*ast.InterfaceType` skip branch).

## Notes / decisions log

- 2026-05-22 — `pkg/observability`'s importers are all framework-internal:
  the top-level `admin/agent/*` subsystem, `pkg/app` wiring, and
  `pkg/observability/hooks`. No examples or user app code import it; no
  forbidden third-party imports. That profile (substantial public API, but
  internal-facing and unproven) is what `experimental` is for.
