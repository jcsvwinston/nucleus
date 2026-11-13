---
sidebar_position: 1
title: Your first login
covers:
  - pkg/auth.UserProvider
  - pkg/auth.User
  - pkg/auth.Chain.Authenticate
  - pkg/auth.HashPassword
  - pkg/auth.CheckPassword
  - pkg/auth.ErrInvalidCredentials
  - pkg/auth.SessionManager.RenewToken
  - pkg/auth.SessionManager.Destroy
  - pkg/nucleus.AppBuilder.WithUserProvider
  - pkg/nucleus.PolicyRule
config_keys:
  - session_cookie_secure
  - csrf_enabled
---

# Your first login

The scaffold pre-authorizes `GET /login` in the bootstrap allow-list, but it
does not serve one — the login endpoint, the user table and the code between
them are yours. `nucleus createuser` does not help here either: it manages
**orbit admin accounts**, not your application's users.

This page is the complete slice, in one place. Every code block below
compiles as shown. The pieces:

1. a `users` table (one migration),
2. a `UserProvider` — the four-method interface that tells the framework
   how to reach that table,
3. registering it with `WithUserProvider`, which puts your table in the
   authentication chain,
4. a module serving `POST /login` and `POST /logout`, with the policy rows
   that let an anonymous browser reach them,
5. seeding the first user.

## 1 — The users table

```sql title="migrations/002_create_users.up.sql"
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    email         TEXT NOT NULL UNIQUE,
    role          TEXT NOT NULL DEFAULT 'user',
    password_hash TEXT NOT NULL
);
```

```sql title="migrations/002_create_users.down.sql"
DROP TABLE IF EXISTS users;
```

Apply it with `nucleus migrate up`. (Placeholders in the Go code below use
SQLite's `?`; adjust to `$1` for PostgreSQL.)

## 2 — The UserProvider

`auth.UserProvider` is how your user model plugs into the framework's
authentication system: four lookups, nothing more. The framework wraps it as
a backend in the authentication chain.

```go title="internal/accounts/store.go"
package accounts

import (
	"context"
	"database/sql"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// Store implements auth.UserProvider over the users table this module
// owns. The *sql.DB is the framework-managed handle, captured in OnStart —
// before the first request can reach the login route.
type Store struct {
	db *sql.DB
}

func (s *Store) FindByID(ctx context.Context, id string) (*auth.User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, email, role FROM users WHERE id = ?`, id))
}

func (s *Store) FindByUsername(ctx context.Context, username string) (*auth.User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, email, role FROM users WHERE username = ?`, username))
}

func (s *Store) FindByEmail(ctx context.Context, email string) (*auth.User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, email, role FROM users WHERE email = ?`, email))
}

func (s *Store) scanUser(row *sql.Row) (*auth.User, error) {
	var u auth.User
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.Role); err != nil {
		return nil, err
	}
	return &u, nil
}

// dummyHash burns the same bcrypt work for an unknown user as for a wrong
// password, so response timing does not enumerate usernames.
var dummyHash, _ = auth.HashPassword("nucleus-timing-equalizer")

func (s *Store) ValidateCredentials(ctx context.Context, username, password string) (*auth.User, error) {
	var (
		u    auth.User
		hash string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, role, password_hash FROM users WHERE username = ?`,
		username).Scan(&u.ID, &u.Username, &u.Email, &u.Role, &hash)
	if err != nil {
		auth.CheckPassword(password, dummyHash)
		return nil, auth.ErrInvalidCredentials
	}
	if !auth.CheckPassword(password, hash) {
		return nil, auth.ErrInvalidCredentials
	}
	return &u, nil
}
```

Two details are deliberate:

- **Every rejection is `auth.ErrInvalidCredentials`.** "No such user" and
  "wrong password" must be indistinguishable to the caller, and the
  framework's adapter collapses them anyway — but returning something else
  would leak through your own logs and tests.
- **The dummy hash.** Without it, an unknown username answers milliseconds
  faster than a wrong password, and that difference enumerates your users.
  The framework cannot equalize work it does not perform; this is the
  provider's job.

## 3 — The login module

```go title="internal/accounts/module.go"
package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// Module carries the framework handles the login routes need.
type Module struct {
	Store *Store

	sm    *auth.SessionManager
	chain *auth.Chain
}

func New() *Module { return &Module{Store: &Store{}} }

func (m *Module) Spec() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name:   "accounts",
		Prefix: "/accounts",
		// The global default-deny gate evaluates before any module
		// middleware, with no claims in the context: without these rows
		// an anonymous browser gets 403 before the handler runs.
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/login", Action: "create"},
			{Subject: "anonymous", Object: "/logout", Action: "create"},
		},
		// The JSON variant of the endpoints skips the double-submit
		// token. An HTML form flow should NOT be exempted — embed the
		// token instead (see the CSRF note below).
		CSRFExempt: []string{"/login", "/logout"},
		OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
			m.Store.db = rt.DB()
			m.sm = rt.Session()
			m.chain = rt.AuthChain()
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Post("/login", m.login)
			r.Post("/logout", m.logout)
		},
	}.Build()
}

func (m *Module) login(c *nucleus.Context) error {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
	}

	// Authenticate through the chain the operator declared
	// (auth_backends), not against the user table directly — a directory
	// configured in front of the local table applies here too.
	user, err := m.chain.Authenticate(c.Request.Context(), in.Username, in.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		}
		return err // a backend was unreachable — a 5xx, not a rejection
	}

	// Rotate the session ID on privilege change (session-fixation
	// defence), then store the identity the two-layer middleware pattern
	// reads back.
	if err := m.sm.RenewToken(c.Request.Context()); err != nil {
		return err
	}
	if err := c.SessionPutString("user_id", user.ID); err != nil {
		return err
	}
	if err := c.SessionPutString("role", user.Role); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "signed in", "user": user.Username})
}

func (m *Module) logout(c *nucleus.Context) error {
	if err := m.sm.Destroy(c.Request.Context()); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "signed out"})
}
```

## 4 — Wire it in main

```go title="main.go"
package main

import (
	"log"

	"github.com/acme/myapp/internal/accounts"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func main() {
	acc := accounts.New()
	if err := nucleus.New().
		FromConfigFile("nucleus.yml").
		WithUserProvider(acc.Store).
		Mount(acc.Spec()).
		Start(); err != nil {
		log.Fatal(err)
	}
}
```

`WithUserProvider` registers the store as the `local` backend of the
authentication chain (use `WithUserProviderNamed` for another name). The
provider is registered at build time but its `db` field is only set in
`OnStart` — that is fine: backends are consulted per request, and every
`OnStart` runs before the server accepts one.

## 5 — Seed the first user

There is no CLI for your application's users (that is `createuser`'s job
only for orbit admins), so seed the first one with ten lines:

```go title="cmd/createappuser/main.go"
// Seeds the first application user.
package main

import (
	"database/sql"
	"log"
	"os"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	_ "modernc.org/sqlite"
)

func main() {
	username, email, password := os.Args[1], os.Args[2], os.Args[3]
	hash, err := auth.HashPassword(password)
	if err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("sqlite", "app.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(
		`INSERT INTO users (id, username, email, role, password_hash) VALUES (?, ?, ?, 'user', ?)`,
		username, username, email, hash)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("user %s created", username)
}
```

```bash
go run ./cmd/createappuser alice alice@example.com 'a-long-password'
```

## 6 — Try it

```bash
go run .   # in another terminal

curl -s -X POST localhost:8080/accounts/login \
  -H 'Content-Type: application/json' \
  -c cookies.txt \
  -d '{"username":"alice","password":"a-long-password"}'
# {"status":"signed in","user":"alice"}

curl -s -X POST localhost:8080/accounts/logout -b cookies.txt
# {"status":"signed out"}
```

Local development over `http://localhost` needs
`session_cookie_secure: false` in `nucleus.yml` — the cookie defaults to
`Secure` and browsers (and curl's cookie jar) drop it on plain HTTP.

## The two notes that save you an afternoon

**CSRF.** The mvc scaffold ships `csrf_enabled: true`. The module above
exempts its JSON endpoints from the double-submit token, which is the
usual choice for an API — but exemption removes a protection, and login
CSRF is a real (if niche) attack. For an HTML form flow, drop the
`CSRFExempt` line and embed the token in the form as a hidden
`_csrf_token` field instead. The
[quickstart's CSRF note](../../getting-started/quickstart.md#a-note-on-csrf)
has the details.

**Protecting routes after login.** The session now stores `user_id` and
`role`, but the global RBAC gate does not read sessions — your protected
modules need the session-to-claims bridge described in
[RBAC & the middleware chain](./rbac-and-middleware.md#your-first-403).
That page is the other half of this one.
