# Auth bench — what Nucleus authentication can and cannot do today

This is the numerator of the A5 gate ("auth of a product"). It exists because
that gate needs a number, and a number needs something that produces it.

**Measured on 2026-09-12 against nucleus v1.27.0; updated as A5 closes gaps.**
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

**14 of 43 controls present. 3 partial. 26 absent.**

| family | present | partial | absent |
|---|---|---|---|
| sessions | 3 | 2 | 1 |
| credentials | 2 | 0 | 6 |
| multi-factor | 0 | 0 | 4 |
| api-keys | 1 | 0 | 5 |
| federated | 1 | 0 | 3 |
| authorization | 3 | 0 | 2 |
| mail | 1 | 1 | 3 |
| tokens | 2 | 0 | 1 |
| posture | 1 | 0 | 1 |

## What the shape of the result says

The framework has the **substrate** and not the **product**. Sessions, tokens,
password hashing, the policy engine and the two extension seams — credential
backends and browser-redirect identity providers — are present and exercised.
What is absent is everything an end user touches: there is no route that logs
anybody in, no account to verify or recover, no second factor, no key to call
an API with.

Three gaps are worth naming because they are not "a feature is missing":

- **The federated seam has no implementation.** The contract is written,
  documented and registrable, and the registry is empty: every deployment
  that wants OIDC writes discovery, PKCE and JWKS itself.
- **An identity carries one role.** `backend.User.Role` is a single string,
  so an identity provider that returns three groups has one field to land in.
  Whatever maps claims onto roles in A5 needs somewhere to put them.
- **The policy model is `sub, obj, act`.** "Ana may edit the posts she owns"
  cannot be expressed, so every application re-implements ownership inside
  its handlers — where nothing audits it.

## Controls

Ids are stable; the arc plan and the audit registry reference them.

| id | control | verdict |
|---|---|---|
| SES-01 | server-side session survives a request cycle | present |
| SES-02 | session token rotates on demand (fixation) | present |
| SES-03 | active sessions can be enumerated | present |
| SES-04 | revoke another session by token | absent |
| SES-05 | session records the device it belongs to | partial |
| SES-06 | inactivity timeout out of the box | partial |
| CRED-01 | passwords hashed with a modern KDF | present |
| CRED-02 | a long passphrase is not silently truncated | present |
| CRED-03 | progressive lockout after repeated failures | absent |
| CRED-04 | password reset by single-use token | absent |
| CRED-05 | password change with re-authentication | absent |
| CRED-06 | registration with email verification | absent |
| CRED-07 | magic-link sign-in | absent |
| CRED-08 | a login route the framework serves | absent |
| MFA-01 | TOTP enrolment and verification | absent |
| MFA-02 | WebAuthn / passkeys | absent |
| MFA-03 | recovery codes | absent |
| MFA-04 | step-up re-authentication | absent |
| KEY-01 | issue a key, stored hashed, shown once | absent |
| KEY-02 | scopes on a key, projected onto the policy | absent |
| KEY-03 | a key-bearing request is authenticated | absent |
| KEY-04 | rotation and revocation | absent |
| KEY-05 | rate limit per identity, not per address | present |
| KEY-06 | CLI to create, list and revoke keys | absent |
| FED-01 | browser-redirect contract with framework-owned state | present |
| FED-02 | an OIDC provider ships with the framework | absent |
| FED-03 | a SAML provider ships with the framework | absent |
| FED-04 | claims map onto roles | absent |
| AZ-01 | role-based access control over routes | present |
| AZ-02 | explicit deny beats a grant | present |
| AZ-03 | permission on an object, not just a path | absent |
| AZ-04 | authorization helper on the handler context | absent |
| AZ-05 | a denial says why | present |
| MAIL-01 | send a plain-text message | present |
| MAIL-02 | send an HTML / multipart message | absent |
| MAIL-03 | attachments | absent |
| MAIL-04 | render a message from a template | absent |
| MAIL-05 | queued mail survives a crash | partial |
| TOK-01 | signed tokens with a keyring and rotation | present |
| TOK-02 | a token from another issuer is rejected | present |
| TOK-03 | revoke an issued token before it expires | absent |
| POS-01 | default security posture frozen as observed values | present |
| POS-02 | posture mapped to a named standard, control by control | absent |

Each case in `authbench_cases_test.go` carries a note saying what exactly is
missing; the note is required for anything that is not `present`.
