# ADR-035: A catalog entry carries its wiring — a chain call, a configuration block, the routes it serves

- Status: Accepted
- Date: 2026-10-05
- Amended: 2026-10-05 (A11 N7) — a module entry may carry a recipe too:
  `sentry` is fetched and imported like any module, and its recipe writes
  the `http_interceptors` / `interceptors.sentry` block, because an
  interceptor that is imported and not listed is not in the request path
  (ADR-029)
- Amended: 2026-10-05 (A11 N8) — `saml` is the second module entry with a
  recipe: fetched and imported like any module, it mounts
  `nucleus.FederatedSignIn()` and writes the `auth_federated` block, because
  a federated provider that is imported and not declared serves nothing
- Deciders: jcsvwinston
- Related: [ADR-034](ADR-034-catalog-embedded-pinned.md) (the catalog this
  extends), [ADR-028](ADR-028-federated-authentication-seam.md) (the
  federated seam whose two routes the framework now also offers), the A11
  bench (`docs/catalog-bench.md`, `CAT-05`, `EN-01`, `EN-03`, `EN-05`)

## Context

After ADR-034 an entry was a module path and a blank import. That is the
whole wiring of a driver, an exporter or a storage provider, and nothing of
the capabilities the framework already has and no import registers: API
keys needed a store opened on the default database and a middleware placed
in a stack the application does not assemble; the durable job queue needed
`jobs_provider: sql`; OIDC registered its provider by import and left the
two sign-in routes to the application, so the callback URL the startup log
tells an operator to register answered 404 until somebody wrote them.
ADR-034 kept accounts, apikeys, sql-queue and websockets out of the table
for that reason — a name the table carries is a name `nucleus add` can
honour.

The bench measured it (`CAT-05` absent; `EN-01`, `EN-03`, `EN-05` partial:
"works by hand, and the catalog does not do it for you").

## Decision

1. **A catalog row may carry a recipe** (`knownproviders.Recipe`): the calls
   spliced into the `nucleus.New()` builder chain of `main.go`, written as
   they read in the chain (`Mount(nucleus.FederatedSignIn())`,
   `WithAPIKeys()`); the packages those calls name besides `pkg/nucleus`;
   the YAML block the entry reads; the routes it serves; and what is left to
   the person (the values only they know, the command that issues the first
   key). Every part is optional.

2. **`nucleus add` and `nucleus new --with` apply it, idempotently.** The
   chain call goes in with the editor `generate module --mount` uses — text
   spliced before the terminal call, the file's own name for `pkg/nucleus`
   followed, nothing reformatted — and a chain that already makes the call is
   left alone. A `main.go` without a builder chain is not guessed at: the
   call is printed and the command fails, as `--mount` does. The
   configuration block is appended to the project's configuration file
   (`--config`, `nucleus.yml` by default) only when **none** of its
   top-level keys is set there; when one is, nothing is written and the block
   is printed to merge by hand. A key the person has set is theirs. A second
   run changes no byte and says "already". `--dry-run` prints the plan.

3. **The capabilities get the entry point a recipe can name.**
   - `WithAPIKeys()` — an `app.Option`, a `nucleus` re-export and a builder
     method: the key store is opened on the default database at boot (the
     table `nucleus apikey create` issues into; SQLite, PostgreSQL, MySQL,
     another engine refused by name), and the middleware is mounted right
     after the bearer decode — before the rate limiter, so a key's owner is
     the identity the limiter keys on, and before the request interceptors.
   - `nucleus.FederatedSignIn()` — a module serving
     `auth.FederatedStartPath`/`FederatedCallbackPath` for every instance
     `auth_federated` declares. The framework keeps the custody it already
     had (ADR-028); the module adds the two handlers every application was
     writing: the anti-forgery token in an HttpOnly, Lax cookie scoped to the
     instance, the session token rotated on success, the identity recorded
     under `nucleus.SessionKeyFederated*`, and either the identity as JSON
     or a redirect to `modules.federated.redirect` (a path of the
     application; an absolute address is refused at boot). On the default
     stack it grants the anonymous subject its two routes and nothing else.
     An application whose sign-in ends differently keeps writing its own pair.
   - The durable queue needs no entry point: `jobs_provider: sql` builds it
     with or without jobs (the NF-13 rule for a non-default provider).

4. **Three core entries carry recipes:** `oidc` (its import, the Mount and
   the `public_base_url` / `auth_federated` / `auth.corp` block, with a
   placeholder issuer and client id the person replaces), `apikeys` (the
   option), `sql-queue` (the block). They are listed under a new group,
   "framework capabilities", except oidc, which stays under federated
   sign-in.

5. **accounts and websockets stay out.** accounts needs a service built on a
   database handle before the application exists and a mailer the api
   starter does not have; websockets needs a hub the application owns and a
   route that serves it. Neither is a chain call and a block today; each
   needs an entry point of its own first (A11 N5). *They got one in
   [ADR-036](ADR-036-accounts-and-realtime-from-the-runtime.md), and with it
   a recipe.*

## Consequences

- `CAT-05` present, and `EN-01`, `EN-03`, `EN-05` present on the api
  starter: 17 → 21 of 38 (after N2 and N3). The bench boots the
  configuration the command wrote, and each of these entries has a wiring
  check that exercises the capability (a sign-in end to end against a
  stand-in identity provider, a key issued with the CLI, a job run on the
  durable table).
- Additive only (QADR-0010): new exported `app.WithAPIKeys`,
  `nucleus.WithAPIKeys`, `AppBuilder.WithAPIKeys`, `nucleus.FederatedSignIn`,
  `FederatedSignInConfig`, `FederatedSignInModuleName` and the four session
  keys; a new `--config` flag on `nucleus add`. Nothing accepted before is
  refused, nothing changes behaviour unless the new call is made.
- The default-deny RBAC layer resolves its subject from bearer claims and
  does not read an API key's owner. A route a program calls with a key is
  authorised for the anonymous subject and gated by `apikeys.Require`. Mapping
  a key to a policy subject is left as its own decision. *Taken in
  [ADR-036](ADR-036-accounts-and-realtime-from-the-runtime.md): the subject is
  the key's owner, with a subject per scope.*
- A recipe is product text in somebody's repository, so the catalog tests
  check it once: every chain call reads as a method call, every block is
  YAML the strict configuration loader accepts on the api starter, every
  route is `METHOD /path`.
