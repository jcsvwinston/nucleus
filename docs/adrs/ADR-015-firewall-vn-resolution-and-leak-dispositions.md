# ADR-015: Dependency-Firewall `/vN` Resolution + Per-Leak Dispositions (F-4)

Reference date: 2026-06-08.
Status: Accepted.
Related: [ADR-004](ADR-004-casbin-default-deny-mount.md) (casbin),
[ADR-005](ADR-005-es256-and-aws-secrets-manager.md) (JWT/ES256),
[ADR-012](ADR-012-prometheus-metrics-exporter.md) (the wrap-don't-leak pattern).

## Context

The dependency firewall (`contracts/firewall_test.go`,
`TestFirewall_NoThirdPartyTypesInStableAPIs`) enforces `SPEC.md` §2 principle
3 — third-party concrete types must not leak onto stable public surfaces
(`pkg/*`). It parses each firewalled package's AST and flags any exported
signature, field, or embed whose type comes from a package on the
`forbiddenThirdParty` list.

The 2026-06-07 exhaustive audit (`docs/audits/2026-06-07-exhaustive-audit-v2.md`,
finding **F-4**, P1, `[verified]`) found the firewall was **structurally blind
to Go Semantic Import Versioning**. `extractImports` only resolved an import's
local identifier when an explicit alias was present; for a normal
`github.com/casbin/casbin/v2` import it left the name empty, and the fallback
`strings.HasSuffix(impPath, "/"+ident.Name)` compared `…/v2` against `/casbin`
and failed. Since almost the entire forbidden list uses `/vN` paths, the
firewall was passing **green while blind** — a hollow guarantee.

Fixing the resolver revealed **seven** leaks on stable surfaces, not the two
the audit had verified by hand:

| # | Package | Symbol | Third-party type |
|---|---------|--------|------------------|
| 1 | `pkg/authz` | `Enforcer` (anonymous embed) | `casbin.Enforcer` |
| 2 | `pkg/auth` | `Claims` (anonymous embed) | `jwt.RegisteredClaims` |
| 3 | `pkg/auth` | `SessionManager.SCS()` | `*scs.SessionManager` |
| 4 | `pkg/auth` | `SessionManager.SetStore()` | `scs.Store` |
| 5 | `pkg/auth` | `NewRedisSessionStore()` | `redis.UniversalClient` |
| 6 | `pkg/auth` | `NewRedisSessionStoreFromURL()` | `*redis.Client` |
| 7 | `pkg/validate` | `RegisterRule()` | `validator.Func` |

## Decision

### §1 — Fix the resolver

In `extractImports`, when no alias is given, derive the local package
identifier from the import path's **last segment that is not a major-version
element** (`^v\d+$`). A small `pkgNameOverrides` table covers modules whose
package name diverges from that segment (today: `github.com/redis/go-redis/v9`
→ `redis`, `github.com/minio/minio-go/v7` → `minio`). Every forbidden package
with a divergent name **must** appear in that table or the firewall goes blind
to it again. The brittle `HasSuffix` fallback is removed — with names now
resolved exactly, a single name-match per import is correct and no longer
double-counts.

### §2 — Wrap `casbin` (leak #1)

`authz.Enforcer` changes from embedding `*casbin.Enforcer` anonymously to
holding it in an unexported field `enforcer *casbin.Enforcer`. The six internal
call sites that used promoted Casbin methods are rewritten to `e.enforcer.*`.
Three Casbin read methods that an external consumer (the admin RBAC inspector,
`pkg/admin/rbac.go`) relied on via promotion are re-exposed as explicit,
Casbin-type-free forwarders: `GetPolicy() ([][]string, error)`,
`GetGroupingPolicy() ([][]string, error)`, `GetAllRoles() ([]string, error)`.

This is the only revealed leak that is wrapped rather than blessed: Casbin
access was an *accidental* consequence of the embed, every capability Nucleus
intends to expose is already covered by a hand-written method, and no caller
needs raw Casbin. If a future power-user genuinely needs the underlying
enforcer, the correct extension point is a deliberate `Inner() *casbin.Enforcer`
escape method added under its own review — intentionally not in this change.

### §3 — Bless the structural / escape-hatch exposures (leaks #2–#7)

The remaining six are recorded as narrow, ADR-cited entries in the
`blessedLeaks` allow-list in `firewall_test.go`, keyed by
`<pkgImportPath> <ownerSymbol> <thirdPartyImportPath>` so an exception can
never silently widen to cover a new leak:

- **§3a — `auth.Claims` ⊃ `jwt.RegisteredClaims` (structural).**
  `jwt.ParseWithClaims` (`pkg/auth/jwt.go`) requires the target value to
  implement the `jwt.Claims` interface. Embedding `RegisteredClaims` is the
  idiomatic, minimal way to satisfy it; implementing the interface by hand
  would still return `*jwt.NumericDate` / `jwt.ClaimStrings`, merely relocating
  the leak to method signatures. The embed is the structurally minimal form of
  a mandatory dependency.

- **§3b — `auth.SessionManager.SCS()` / `SetStore()` (escape hatches).** These
  exist precisely to hand the caller the underlying SCS manager for advanced
  configuration and to inject a pluggable `scs.Store` (Redis/SQL/custom).
  Wrapping them would defeat their purpose.

- **§3c — `auth.NewRedisSessionStore()` / `NewRedisSessionStoreFromURL()`
  (integration constructors).** Redis is an optional, plugin-style session
  backend; these constructors let callers share a configured client and own
  its lifecycle (`*redis.Client.Close`).

- **§3d — `validate.RegisterRule()` (extension point).** The documented way to
  register a custom validation rule. `validator.Func`'s parameter
  (`validator.FieldLevel`) is a fat interface; re-exposing it under a Nucleus
  name would only move the dependency, not remove it.

### §4 — One ADR

The resolver fix is the mechanism that surfaces the leaks; the per-leak
dispositions are the architectural response. They are causally coupled and
recorded together here.

## Alternatives considered

- **Wrap all seven.** Rejected for #2–#7: jwt is structurally required;
  `SCS`/`SetStore`/`NewRedisSessionStore*` are deliberate integration escape
  hatches whose entire value is exposing the third-party type; `validator.Func`
  wraps a fat interface. Wrapping these would relocate or destroy functionality
  rather than tighten the contract. Casbin (#1) was the only accidental embed,
  so it alone is wrapped.
- **Leave the resolver and hand-maintain a leak list.** Rejected: the whole
  finding is that a *code* contract silently rotted. The resolver must actually
  work; exceptions must be explicit and ADR-cited, not implicit gaps.
- **Parse each dependency's `package` clause for its real name.** Rejected as
  too heavy for a static contract test (needs the module graph). The
  last-non-`vN`-segment heuristic plus a tiny override table is exact for the
  forbidden set and cheap.

## Consequences

- **`pkg/authz`: semantic narrowing, pre-v1.0 clean break.** Casbin's promoted
  methods are no longer callable directly on `authz.Enforcer`. No code outside
  `pkg/authz` relied on them except three read methods now re-exposed as
  Casbin-free forwarders (grep-verified across `pkg/`, `examples/`, tests).
  Recorded as a `CHANGELOG` note; no `docs/deprecations/` entry or
  `migration-assistant` spec (single-maintainer, no external users —
  `contract-guardian` confirmed, consistent with ADR-006/ADR-008/ADR-014
  precedent).
- **Freeze baseline grows by exactly three additive lines**
  (`method:Enforcer.GetPolicy|GetGroupingPolicy|GetAllRoles`); no removals. The
  embed and promoted methods were never in the baseline (`go/doc` does not
  capture promoted foreign methods, and `exportedMembersFromTypeDecl` skips
  anonymous fields), so the wrap is otherwise byte-neutral.
- **The firewall is now green *and honest*.** Six exposures remain on the
  public surface but are explicitly adjudicated and ADR-cited, not hidden. Each
  is recorded in `docs/reference/API_CONTRACT_INVENTORY.md`.
- **`blessedLeaks` is now a contract surface of its own.** Adding an entry
  widens the blessed third-party surface of a frozen package and requires an
  ADR; removing one (by wrapping the leak) is always safe.
