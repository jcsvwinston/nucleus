# ADR-036: Accounts and realtime are built from the runtime, and a request's subject is its identity whatever the credential

- Status: Accepted
- Date: 2026-10-05
- Deciders: jcsvwinston
- Related: [ADR-035](ADR-035-catalog-entries-carry-their-wiring.md) (the
  recipes; its point 5 left accounts and websockets out until this one),
  [ADR-004](ADR-004-casbin-default-deny-mount.md) (the default-deny layer whose
  subjects this extends), the A11 bench (`docs/catalog-bench.md`, `EN-04`,
  `EN-07`), NU-112

## Context

ADR-035 gave a catalog entry a recipe — a chain call, a configuration block —
and kept two core entries out because neither had an entry point a recipe
could name:

- **accounts.** `accounts.Module` takes a finished `*Service`, and the service
  needs a database handle and a mailer before the application exists, which
  is when the framework has neither: it opens the database and builds the
  mail sender inside `nucleus.New()…Start()`. The author opened a second
  handle of their own and supplied a mailer of their own; on the api starter,
  which is built `WithoutDefaults()` and builds no mail sender, registration
  answered 500 at the first request (the bench measured it at `S0`).
- **websockets.** The hub and the route were the application's: `realtime.New`
  in `main`, a handle threaded to every module that publishes, and a route
  calling `realtime.ServeWS`.

And `N4` found that the default-deny layer saw `anonymous` for every request
that presented an API key (NU-112): a policy could not grant a route to a
key's owner or to a scope, and `apikeys.Require` was the only gate. The fix
needed a decision about what a key's owner *is* — which is the identity
question the account flows raise too.

`pkg/app` and `pkg/nucleus` cannot import `pkg/accounts`: it imports
`pkg/nucleus` (its module is a `nucleus.ModuleSpec`), which imports
`pkg/app`.

## Decision

1. **The account flows are a module built from the runtime:
   `accounts.FromRuntime()`.** At start-up it opens the account tables on the
   application's default database (SQLite, PostgreSQL, MySQL; another engine
   fails with its name), takes the application's mail sender and session
   manager, and serves the routes `Module` serves. Its configuration is
   `modules.accounts.*`: `base_url` and `from` are required (a link built from
   the request's `Host` is a link an attacker chooses), email verification is
   required unless `allow_unverified_login`, and `mfa_key_env` names the
   variable holding the second-factor key. On the default stack it grants the
   anonymous subject its routes, as `FederatedSignIn` does. It lives in
   `pkg/accounts` because of the import cycle; it is mounted, not an option.

2. **Mail: a refusal at boot, and a development driver the recipe writes.**
   `FromRuntime` refuses to start when the application has no mail sender
   (built `WithoutDefaults()` without the new `WithMail()`) or when the sender
   delivers nothing (`mail_driver: noop`, the default; `mail.Discards`), and
   the refusal names the keys. The alternative — a fallback that logs when
   nothing is configured — would put reset links in production logs whenever
   somebody forgot a key. Instead `mail_driver: log` is a driver like the
   others: it writes each message to the application log at WARN, and it is
   refused outside `env: development` at configuration load and again at
   boot. The recipe writes it, so `nucleus add accounts` works on the starter
   at once and the person reads the confirmation link in the terminal;
   deploying with it fails. Registration therefore never answers 500 for a
   missing mailer: the application does not start.

3. **The realtime hub is the runtime's: `WithRealtime()`.** An `app.Option`
   (with its `nucleus` re-export and builder method) builds a hub with the
   application, closes it at shutdown, exposes it as `App.Realtime` and, to a
   module, through `nucleus.RealtimeFrom(rt)` — an optional interface
   (`RealtimeSource`), because `Runtime` is published and does not grow before
   the major (QADR-0010). It serves `GET /realtime/{topic}`: a WebSocket for an
   upgrade, server-sent events for `Accept: text/event-stream`, 406 otherwise;
   one-way. A channel is a route, so the default-deny layer authorises it by
   path (`p, member, /realtime/*, read, allow`) — no second mechanism. The hub
   reaches this process; replicas build their own hub with a relay.

4. **A request's subject is the identity behind it, whatever the
   credential.** The default-deny layer tries, in order: a bearer token's user
   id and role; an API key's owner and `scope:<name>` for each scope the key
   carries (`apikeys.ScopeSubject`); the account a session was signed in as,
   recorded under the new `auth.SessionKeySubject` (the account flows write
   the account's id there); then `anonymous`. Identities share one namespace —
   `nucleus apikey create --owner <account id>` issues a key that acts as the
   account — so a policy row or a `g` role for an id reaches the person's
   token, keys and session alike. A request is allowed when any subject is.
   Scopes are subjects rather than attributes of a request because the model
   is `r = sub, obj, act`: growing the request tuple would break every custom
   model and `Enforcer.Can`. A key with no scopes reaches what its owner and
   anonymous reach; a route that must have an identity *and* a scope keeps
   `apikeys.Require(scope)`.

5. **Two catalog entries carry recipes:** `accounts` (`WithMail()`,
   `Mount(accounts.FromRuntime())` and its import; `mail_driver: log` and the
   `modules.accounts` block) and `websockets` (`WithRealtime()`), both under
   "framework capabilities".

## Consequences

- `EN-04` and `EN-07` present: 33 → 35 of 38. Each has a wiring check against
  the starter the command left: sign-up, the confirmation link read from the
  application's log, sign-in refused before confirming and accepted after;
  a WebSocket on `/realtime/bench` receiving what a handler of the application
  publishes.
- Additive only (QADR-0010). New exported: `accounts.FromRuntime`,
  `RuntimeConfig`, `RuntimeModuleName`; `app.WithMail`, `app.WithRealtime`,
  `App.Realtime`, `app.RealtimeRoute`, `app.RealtimeChannelPath` and their
  `nucleus` re-exports and builder methods; `nucleus.RealtimeFrom`,
  `RealtimeSource`; `auth.SessionKeySubject`; `apikeys.ScopeSubject`,
  `ScopeSubjectPrefix`; `mail.LogDriver`, `NoopDriver`, `Discards`.
- The new subjects only add: before this change no released configuration put
  an API key in the context ahead of the default-deny layer (only
  `WithAPIKeys`, unreleased until now, mounts it there), and no session
  carried `auth.SessionKeySubject`. `anonymous` is still tried last, so a
  route an application granted to anonymous and guarded with
  `apikeys.Require` keeps working. `StartSession` writes one more session key.
- A `deny` row applies to the subject it names: denying an owner does not stop
  their keys on a route granted to a scope. Revoking the key does.
- `mail_driver: log` is a new value of an existing key. A custom provider
  registered under the name `log` would now collide with the built-in one, as
  `memory` did when it shipped.
- `pkg/realtime` stays transitional and outside the freeze; the frozen
  `WithRealtime` and `RealtimeFrom` hand out its `*Hub`, as `Runtime.Outbox`
  hands out the transitional outbox.
- Not done: the hub has no relay from configuration (replicas wire their own),
  `FromRuntime` sends plain-text mail (it has no access to the application's
  mail templates; `Module` with `mail.Templates` does), and the account's
  `Role` is not a session subject — a role reaches a signed-in account through
  `g, <account id>, <role>`.
