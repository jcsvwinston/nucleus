# Auth bench — what Nucleus authentication can and cannot do today

This is the numerator of the A5 gate ("auth of a product"). It exists because
that gate needs a number, and a number needs something that produces it.

**Measured on 2026-09-12, and kept current as the arc closed its gaps: the
numbers below are what the suite produced on its last run.**
Run it with:

```bash
go test ./internal/authbench/ -run TestAuthBench -v
```

The bench is not prose. Every control is a Go probe in
`internal/authbench/` that boots an application and asks the route, calls the
API and checks the answer, or reads the configuration struct by koanf tag.
`TestAuthBench` asserts the **recorded verdict** rather than success, so
closing a gap turns the suite red with "this one is present now, update the
verdict" — which is what keeps this page honest about a surface that is
mostly absent today.

## The verdicts

| verdict | meaning |
|---|---|
| **present** | the control exists and its probe exercised it end to end |
| **partial** | a piece exists; the case records exactly what is missing |
| **absent** | no surface at all — the probe measures the absence (a 404 from a booted application, an empty registry, a missing knob) |

A control that cannot be probed does not belong in the bench. That rule comes
from the A4 query bench, where two of five findings were wrong because the
measurement trusted a comment that had outlived what it described. Here the
same failure showed up in its other form: the first draft of KEY-05 recorded
"the limiter keys on the address" after reading the name of a configuration
knob. The probe that exhausts one user's budget and finds another user, same
role and same address, still served, says otherwise — the limiter keys on the
authenticated user id, with the tenant as a prefix. A name is not a
measurement either.

## The result

**40 of 43 controls present. 1 partial. 2 absent.**

| family | present | partial | absent |
|---|---|---|---|
| sessions | 5 | 1 | 0 |
| credentials | 8 | 0 | 0 |
| multi-factor | 3 | 0 | 1 |
| api-keys | 6 | 0 | 0 |
| federated | 3 | 0 | 1 |
| authorization | 5 | 0 | 0 |
| mail | 5 | 0 | 0 |
| tokens | 3 | 0 | 0 |
| posture | 2 | 0 | 0 |

It started at **14 of 43**, the day the bench was written.

## What is not present, and why

Three rows are not `present`, and each carries its reason rather than sitting
in a gap nobody explains.

- **MFA-02, WebAuthn — absent.** Passkeys need CBOR and COSE, so the right
  packaging is a sibling module. A sibling module pins the LAST published
  release of the framework, which will not contain `accounts.MFAStore` until
  the release this arc produces — so it cannot be delivered in the same cut.
  Writing attestation and assertion verification by hand in the core would
  put a security-critical parser where a mistake is silent.
- **FED-03, SAML — absent.** The same seam the OIDC provider proves works,
  and a body of work of its own: XML signatures, metadata exchange, per-IdP
  quirks. Scheduling it here would have meant doing it badly.
- **SES-06, inactivity timeout — partial.** The knob is honoured and ships
  disabled. Turning it on by default would start expiring sessions in every
  deployment that upgrades, which this suite groups into its next major. It
  is no longer silent: `nucleus doctor security` names it in production, and
  the ASVS baseline records it as V3.3.2.

## What the shape of the result said at the start

The framework had the **substrate** and not the **product**. Sessions,
tokens, password hashing, the policy engine and the two extension seams were
present and exercised; nothing an end user touches existed — no route logged
anybody in, no account to verify or recover, no second factor, no key to call
an API with.

Three of those gaps were worth naming, and all three are closed:

- **the federated seam had no implementation** — the contract was written,
  documented and registrable, and the registry was empty;
- **an identity carried one role**, so a provider returning three groups had
  one field to land in;
- **the policy model was `sub, obj, act`**, so "ana may edit the posts she
  owns" could not be expressed and every application re-implemented ownership
  inside its handlers, where nothing audits it.

## Controls

Ids are stable; the arc plan and the audit registry reference them.

| id | control | verdict |
|---|---|---|
| SES-01 | server-side session survives a request cycle | present |
| SES-02 | session token rotates on demand (fixation) | present |
| SES-03 | active sessions can be enumerated | present |
| SES-04 | revoke another session by token | present |
| SES-05 | session records the device it belongs to | present |
| SES-06 | inactivity timeout out of the box | partial |
| CRED-01 | passwords hashed with a modern KDF | present |
| CRED-02 | a long passphrase is not silently truncated | present |
| CRED-03 | progressive lockout after repeated failures | present |
| CRED-04 | password reset by single-use token | present |
| CRED-05 | password change with re-authentication | present |
| CRED-06 | registration with email verification | present |
| CRED-07 | magic-link sign-in | present |
| CRED-08 | a login route the framework serves | present |
| MFA-01 | TOTP enrolment and verification | present |
| MFA-02 | WebAuthn / passkeys | absent |
| MFA-03 | recovery codes | present |
| MFA-04 | step-up re-authentication | present |
| KEY-01 | issue a key, stored hashed, shown once | present |
| KEY-02 | scopes on a key, projected onto the policy | present |
| KEY-03 | a key-bearing request is authenticated | present |
| KEY-04 | rotation and revocation | present |
| KEY-05 | rate limit per identity, not per address | present |
| KEY-06 | CLI to create, list and revoke keys | present |
| FED-01 | browser-redirect contract with framework-owned state | present |
| FED-02 | an OIDC provider ships with the framework | present |
| FED-03 | a SAML provider ships with the framework | absent |
| FED-04 | claims map onto roles | present |
| AZ-01 | role-based access control over routes | present |
| AZ-02 | explicit deny beats a grant | present |
| AZ-03 | permission on an object, not just a path | present |
| AZ-04 | authorization helper on the handler context | present |
| AZ-05 | a denial says why | present |
| MAIL-01 | send a plain-text message | present |
| MAIL-02 | send an HTML / multipart message | present |
| MAIL-03 | attachments | present |
| MAIL-04 | render a message from a template | present |
| MAIL-05 | queued mail survives a crash | present |
| TOK-01 | signed tokens with a keyring and rotation | present |
| TOK-02 | a token from another issuer is rejected | present |
| TOK-03 | revoke an issued token before it expires | present |
| POS-01 | default security posture frozen as observed values | present |
| POS-02 | posture mapped to a named standard, control by control | present |

Each case in `authbench_cases_test.go` carries a note saying what exactly is
missing; the note is required for anything that is not `present`.
