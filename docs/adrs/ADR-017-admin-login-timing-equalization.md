# ADR-017: Admin Login Timing Equalization (Username-Enumeration Oracle)

Status: Accepted
Date: 2026-06-11
Related: ADR-006 (CSRF constant-time compare), ADR-016 (admin authn at router edge)

## Context

The unknown-username branch of `pkg/admin.(*DatabaseAdminAuth).handleLoginPOST`
returned immediately without a bcrypt comparison, while the wrong-password branch
ran a full cost-12 `bcrypt.CompareHashAndPassword`. The latency gap (~100–300 ms)
allowed unauthenticated callers to enumerate valid admin usernames from timing
alone, despite identical HTTP status codes and response bodies (fleetdesk
finding #17).

## Decision

Introduce a package-const `dummyPasswordHash` — a genuine cost-12 bcrypt hash of
a throwaway string whose pre-image is not recorded anywhere — and unconditionally
call `auth.CheckPassword(password, dummyPasswordHash)` in the unknown-username
branch. The result is discarded. Both rejection paths now incur exactly one
bcrypt verification before returning 401 with the same body.

## Alternatives considered

- `time.Sleep` to a fixed floor: rejected — bcrypt latency varies with CPU and
  load; a fixed sleep overshoots on fast hardware and undershoots under load,
  reopening the oracle at the margins.
- `crypto/subtle.ConstantTimeCompare` on a dummy value: not applicable — bcrypt
  verification is not constant-time by design; equalization must happen at the
  full-compare level, not the byte-compare level.

## Consequences

- Username enumeration via login timing is closed.
- Latency of the unknown-username 401 path rises from microseconds to the
  wrong-password path's ~50–300 ms. No status or body change on any path.
- The dummy hash constant in source is not a secret: bcrypt preimage resistance
  protects the throwaway string, and the comparison result is always discarded.
- Regression pinned by `TestHandleLoginPOST_UnknownUserBurnsBcryptCompare`
  (median-of-3, 4x-margin ratio assertion — three orders of magnitude looser
  than the pre-fix ratio, so scheduler noise cannot flake it).
- Residual sub-millisecond signal from the user-table linear scan (early exit on
  match vs full iteration) is dominated by bcrypt by four to five orders of
  magnitude and is not considered a practical oracle at admin-table sizes.
