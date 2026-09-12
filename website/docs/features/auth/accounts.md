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
