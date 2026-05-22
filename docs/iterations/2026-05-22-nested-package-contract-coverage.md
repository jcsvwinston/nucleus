# Iteration archive — 2026-05-22 nested-package contract coverage

> Archived 2026-05-22 as part of the session-end `/handoff`. Landed as the
> feature commit `1233bf4` (`test(contracts): extend contract scanners to
> nested pkg/* packages`) on `main`, followed by the usual
> `chore(state): close` state commit. Candidate #1 from the prior queue
> (surfaced by architect-reviewer 2026-05-22 during the shared-registry
> iteration).

## Goal

Extend the contract scanners and the `allPublicPackages()` registry / guard
from top-level `pkg/*` directories only to the 4 nested public packages that
neither scanner had ever covered, deciding a lifecycle posture for each.

## Scope

### In

- `contracts/packages_test.go`:
  - `discoverTopLevelPublicPackages` → `discoverPublicPackages`: now a
    recursive `filepath.WalkDir` over `pkg/`, with a `shouldSkipDir` helper
    that prunes non-Go subtrees (`node_modules`, `vendor`, `dist`,
    `testdata`, dot/underscore dirs). The filesystem-match guard now covers
    all 25 packages (21 top-level + 4 nested).
  - 4 nested rows added to `allPublicPackages()` (owner-confirmed postures):
    - `pkg/auth/secrets` — `transitional`, firewalled (AWS Secrets Manager
      resolver, slated for cloud-secrets plugin extraction).
    - `pkg/observability/hooks` — `uninventoried`, neither (internal-facing,
      same family as its `uninventoried` parent; no forbidden imports).
    - `pkg/tasks/providers/asynq` — `transitional`, firewalled (wraps asynq +
      otel in unexported fields).
    - `pkg/tasks/providers/memory` — `transitional`, neither (uuid + cron,
      not forbidden).
  - None frozen → the API freeze baseline is untouched.
- `contracts/firewall_test.go`:
  - Added `github.com/aws/aws-sdk-go-v2/config` and
    `.../service/secretsmanager` to the forbidden-type map so the
    AWS-confinement that `pkg/auth/secrets` documents (ADR-005, Accepted) is
    actually enforced.
  - Fixed `checkTypeSpecForLeaks`: it flagged forbidden types in ANY field of
    an exported struct, including unexported ones — contrary to the
    firewall's "public surface" spec. Now skips unexported, named struct
    fields / interface methods (via new `anyExported`) while keeping embedded
    fields checked (embedding promotes the embedded type's exported methods).
    This false positive fired the moment `asynqprovider` (which holds
    `*asynq.Client`, `*asynq.Scheduler`, etc. in unexported fields) joined
    the scan.

### Out

- **`pkg/observability` + `pkg/observability/hooks` lifecycle decision.** Both
  stay `uninventoried`; giving them an inventory row + posture is the new
  candidate #1.
- Two optional reviewer cleanups (low priority): the double `os.ReadDir` in
  `discoverPublicPackages`, and the effectively-dead `*ast.InterfaceType`
  unexported-skip branch (kept for symmetry).

## Acceptance criteria — all met

- [x] Behaviour-preserving for the freeze: `contracts/baseline/` untouched;
  freeze set still exactly 17; freeze passes without
  `NUCLEUS_UPDATE_CONTRACT_BASELINE=1`.
- [x] Firewall set expanded 18 → 20 (added secrets + asynq), zero violations.
- [x] `go test ./...` green; `gofmt`/`go vet` clean; contract-freeze script
  passes.
- [x] Loop: architect PASS, code-reviewer NITS (interface-branch comment
  added; two cleanups deferred), security-auditor PASS, contract-guardian
  PASS, test-runner PASS.

## Outcome

Landed as feature commit `1233bf4`. All public `pkg/*` packages — top-level
and nested — are now machine-visible in the registry, each with a deliberate
posture. The firewall now matches its own "public surface" spec and enforces
AWS-confinement per ADR-005.

## Follow-ups opened / carried

- **Candidate #1 (new): `pkg/observability` + `hooks` inventory entry +
  lifecycle.** Both `uninventoried`; needs an owner lifecycle decision and
  inventory rows. The frozen⟺lifecycle invariant will enforce consistency.
- Optional cleanups (double-ReadDir; dead interface branch) noted in the
  candidate list.

## Notes / decisions log

- 2026-05-22 — Owner confirmed the conservative posture set (3 transitional +
  1 uninventoried, none frozen) and "add AWS + enforce" for the firewall.
  Security-auditor verified the unexported-field skip opens no leak vector:
  unexported fields are importer-unreachable, and exported accessors/methods
  plus embedded fields remain covered by complementary scan paths. The
  asynq/otel types in `asynqprovider` and the AWS types in `pkg/auth/secrets`
  are all confined to unexported fields / internal interfaces, so the
  expanded firewall passes clean.
