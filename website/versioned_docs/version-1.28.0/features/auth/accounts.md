---
sidebar_position: 5
title: Accounts
covers:
  - pkg/accounts.Module
  - pkg/accounts.New
  - pkg/accounts.Config
  - pkg/accounts.Service
  - pkg/accounts.Service.Register
  - pkg/accounts.Service.VerifyEmail
  - pkg/accounts.Service.Login
  - pkg/accounts.Service.LoginFrom
  - pkg/accounts.ClientIP
  - pkg/accounts.Service.Logout
  - pkg/accounts.Service.StartSession
  - pkg/accounts.Service.RequestPasswordReset
  - pkg/accounts.Service.ResetPassword
  - pkg/accounts.Service.ChangePassword
  - pkg/accounts.Service.RequestMagicLink
  - pkg/accounts.Service.ConsumeMagicLink
  - pkg/accounts.Service.RevokeSessions
  - pkg/accounts.Service.UseSessions
  - pkg/accounts.Store
  - pkg/accounts.Account
  - pkg/accounts.Token
  - pkg/accounts.TokenPurpose
  - pkg/accounts.NewSQLStore
  - pkg/accounts.SQLStore
  - pkg/accounts.SQLStoreConfig
  - pkg/accounts.SQLStore.PurgeExpired
  - pkg/accounts.Mailer
  - pkg/accounts.MFAStore
  - pkg/accounts.Factor
  - pkg/accounts.FactorKind
  - pkg/accounts.Service.BeginTOTPEnrolment
  - pkg/accounts.Service.ConfirmTOTPEnrolment
  - pkg/accounts.Service.DisableTOTP
  - pkg/accounts.Service.VerifySecondFactor
  - pkg/accounts.Service.CompleteSecondFactor
  - pkg/accounts.Service.StartPendingSession
  - pkg/accounts.Service.HasConfirmedFactor
  - pkg/accounts.Service.RegenerateRecoveryCodes
  - pkg/accounts.Service.RemainingRecoveryCodes
  - pkg/accounts.Service.RequireFreshAuth
  - pkg/accounts.Service.MarkAuthenticated
  - pkg/accounts.NewTOTPSecret
  - pkg/accounts.TOTPURI
  - pkg/accounts.VerifyTOTP
  - pkg/accounts.TestingTOTPCode
  - pkg/auth.SessionManager.HasSession
  - pkg/accounts.DefaultConfig
---

# Accounts

The part of authentication an end user touches: registering, confirming an
address, signing in and out, recovering a password, and being locked out
after enough wrong guesses.

It is an **opt-in module**. Nothing is served until you mount it, and
nothing about an application that does not is changed.

```go
store, err := accounts.NewSQLStore(ctx, db, accounts.SQLStoreConfig{
    Flavor: accounts.FlavorPostgres,
})
svc, err := accounts.New(store, nil, mailer, templates, accounts.Config{
    BaseURL: "https://app.example.com",
    From:    "no-reply@example.com",
    RequireEmailVerification: true,
}, logger)

app := nucleus.New().Mount(accounts.Module(svc))
```

The module takes the application's **own** session manager at start-up, so a
service built with a different one cannot end up writing into a session the
request never carries.

## The routes

| Method | Path | What it does |
| --- | --- | --- |
| `POST` | `/auth/register` | Create an account and send a confirmation link |
| `GET`/`POST` | `/auth/verify-email` | Confirm an address with the link's token |
| `POST` | `/auth/login` | Sign in and start a session |
| `POST` | `/auth/logout` | End the session |
| `POST` | `/auth/password/reset` | Send a reset link |
| `PUT` | `/auth/password/reset` | Set a new password with the link's token |
| `POST` | `/auth/password/change` | Change the signed-in account's password |
| `POST` | `/auth/magic-link` | Send a one-time sign-in link |
| `GET` | `/auth/magic-link` | Sign in with it |

The handlers are thin on purpose. Every decision that matters is in
`Service`, tested once, instead of in each application's copy of the same
seven handlers.

## What the flows decide for you

**A registration form does not say whether an address is taken.** It answers
`202` with the same body either way, and mails the existing owner a
confirmation link instead. An endpoint that answers differently is an
account enumeration oracle — the most-used one on the web. The reset and
magic-link endpoints keep the same property.

**A wrong password and an unknown account are indistinguishable**, in the
error *and* in the time it takes: an unknown address is still verified
against a dummy hash, because answering faster is the same disclosure by
another channel.

**Tokens are single-use, stored hashed, and scoped to a purpose.** The row
holds SHA-256 of the token, so a leaked database does not hand over live
reset links; consuming one is a single conditional `UPDATE`, so two
concurrent uses of the same link cannot both win; and a token issued to
verify an address cannot reset a password.

**A reset ends the account's sessions** and drops every other outstanding
reset link. A recovery that leaves the attacker signed in has not recovered
the account.

**Signing in rotates the session token**, which is what closes session
fixation.

**A password change requires the current password** — a session left open on
a shared machine must not be enough to take the account over — and changes
the account in the SESSION, never one named in the body.

### One thing to know about magic links

`GET /auth/magic-link?token=…` **consumes** the token, because that is what
makes a link work in one click. Mail clients, security scanners and link
previewers fetch URLs they find in mail, and a fetch is indistinguishable
from a click: the token is spent and the person who received it arrives to
an expired link.

The framework cannot tell the two apart, so it does not pretend to. Two
things reduce the cost, and both are yours to choose:

- keep the TTL short (15 minutes by default) so a spent link is quickly
  replaced by asking again;
- if your users are behind a scanner that does this, serve your own page at
  the link and have it **POST** the token, so the fetch is harmless and the
  click is what spends it. The token travels in the JSON body as well as the
  query string, precisely for that.

## Lockout

Failures are counted per identity within a window; past the threshold the
endpoint answers `429` (not `401`: a client that retries on `401` would
hammer a locked account), and it answers that way for the **correct**
password too. A lockout that lets the right password through only slows down
an attacker who is already wrong.

```go
accounts.Config{
    LockoutThreshold: 10,              // default
    LockoutWindow:    15 * time.Minute, // default
}
```

A successful sign-in clears the counter.

**What the failures are counted against has no free answer.** Per *account*
is what ASVS asks for and what stops credential stuffing against one user —
and it lets anyone lock a known address out for the window by typing ten
wrong passwords, which is a denial of service against that person. Per
*client* stops that and lets a botnet spread its guesses.

So the framework does not choose silently. `Login` counts per account.
`LoginFrom` folds the caller in, and the mounted module uses it with the
client address: the attacker locks out themselves, the owner still signs in,
and a distributed attack still meets the per-identity rate limiter.

```go
svc.LoginFrom(ctx, email, password, accounts.ClientIP(r))
```

## Where accounts live

`Store` is the port. `NewSQLStore` is an implementation with its own three
tables (`nucleus_accounts`, `nucleus_account_tokens`,
`nucleus_account_failures`) for an application that does not have a users
table yet; an application that does implements the same interface against
it.

Whatever implements it must keep three properties, because the flows rely on
them: `Create` reports a duplicate rather than overwriting, `ConsumeToken` is
atomic, and `RecordFailure` returns the count including the failure it just
recorded.

`SQLStore.PurgeExpired` drops used and expired rows; call it from a
scheduled job. Nothing depends on it for correctness — every read already
filters on time.

## Mail

`Mailer` is one method, so you can hand it the direct sender or a
queue-backed one. Prefer queueing: a confirmation mail that goes out while
the transaction rolls back points at an account that does not exist. See
[Queueing mail with the write it announces](../storage-and-tasks.md).

Supply `mail.Templates` for the wording (`verify_email`, `reset_password`,
`magic_link`); without them the service sends a plain-text fallback, so the
flows work before anybody writes a template.

## Second factor

TOTP is implemented against RFC 6238 in about sixty lines of HMAC and a
counter, rather than pulled in as a dependency — the framework is
stdlib-first, and a dependency that small is a supply-chain entry for every
application that links it. The RFC's own test vectors are in the suite, so
any authenticator app agrees.

Second factors need a key:

```go
accounts.Config{
    Issuer:           "Example Co",   // what the app shows next to the code
    MFAEncryptionKey: key,            // exactly 32 bytes
}
```

Without it, enrolment is **refused**. A TOTP secret is a password
equivalent — anyone holding it mints codes forever — so storing it in the
clear would work perfectly and fail once, silently, in a database backup.
Secrets are sealed with AES-256-GCM and a per-secret nonce.

### The routes

| Method | Path | What it does |
| --- | --- | --- |
| `POST` | `/auth/mfa/totp` | Start enrolment: returns the secret and the `otpauth://` URI to show as a QR code |
| `PUT` | `/auth/mfa/totp` | Confirm with the first code; returns the recovery codes |
| `DELETE` | `/auth/mfa/totp` | Remove the factor and its recovery codes |
| `POST` | `/auth/mfa/verify` | Finish a sign-in that stopped at the password |
| `POST` | `/auth/mfa/recovery-codes` | Issue a fresh set, invalidating the old one |

### The sign-in becomes two steps

With a confirmed factor, `POST /auth/login` answers **401** with
`{"mfa_required": true}` and records a *pending* sign-in under a different
session key from a finished one — so nothing that reads the signed-in
identity mistakes one for the other. `POST /auth/mfa/verify` completes it.

401 rather than a 2xx is deliberate: a client that treats any 2xx as
"signed in" would act on a half-authenticated session.

### Properties worth knowing

- **Enrolment is not active until confirmed.** A secret nobody has proven
  they can read must not lock an account out of its own sign-in.
- **A one-time password is one-time.** The accepted counter step is
  recorded and a code at or below it is refused, so a code read over
  someone's shoulder does not work for the rest of its thirty seconds.
- **Recovery codes are single-use, hashed, and forgiving about typing** —
  case and dashes are normalised before hashing. They are shown once.
- **A wrong code counts towards the lockout.** A second factor that can be
  guessed without limit is a six-digit password.
- **TOTP is tried before recovery codes**, so a mistyped code does not burn
  one.

### Step-up

Changing a second factor requires a sign-in from the last 15 minutes, not
merely a session that still resolves: a session open for three weeks is
evidence somebody signed in three weeks ago, and nothing about who is at the
keyboard now. `Service.RequireFreshAuth(ctx, maxAge)` is the same guard for
your own sensitive operations, and `MarkAuthenticated` stamps a fresh proof.

`SessionManager.HasSession(ctx)` answers whether a context carries session
data at all — a handler mounted outside the session middleware gets an
error instead of a panic from deep inside the session library.
