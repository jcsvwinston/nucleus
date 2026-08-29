# nucleus/providers/ldap

Authenticate a Nucleus application against an LDAP directory.

It is a **separate Go module inside the framework's own repository**: it
ships on the framework's release train, in the framework's documentation
and under the framework's CI, but it is not in the framework's `go.mod`.
Whoever does not authenticate against a directory does not carry an LDAP
client (ADR-023, decision 5).

## Install

```bash
go get github.com/jcsvwinston/nucleus/providers/ldap
```

Import it for the side effect — the same shape `database/sql` drivers use —
and then name it in the chain:

```go
import _ "github.com/jcsvwinston/nucleus/providers/ldap"
```

```yaml
# nucleus.yml
auth_backends: [ldap, local]

auth:
  ldap:
    url: "ldaps://dc.corp.local:636"
    base_dn: "ou=people,dc=corp,dc=local"
    bind_dn: "cn=svc-nucleus,ou=services,dc=corp,dc=local"
    bind_password: "${LDAP_BIND_PASSWORD}"
```

The order in `auth_backends` is the feature: `[ldap, local]` consults the
directory first and still lets a local account in on the morning the
directory does not answer. A backend that cannot reach its source is
skipped; one that rejects ends the attempt.

## Settings

Everything lives under `auth.ldap.*`. A key not listed here is an error at
startup, not a silently ignored line.

| Key | Default | Meaning |
|---|---|---|
| `url` | — (required) | `ldaps://host:636`, or `ldap://host:389` with `start_tls`. |
| `base_dn` | — (required) | Subtree the user search starts from. |
| `bind_dn` | `""` | Service account used to SEARCH. Empty means anonymous search. Never used to authenticate the person logging in. |
| `bind_password` | `""` | Its password. |
| `user_filter` | `(uid=%s)` | `%s` is the username, filter-escaped before substitution. |
| `attr_username` | `uid` | Attribute mapped onto `auth.User.Username`. |
| `attr_email` | `mail` | Attribute mapped onto `auth.User.Email`. |
| `attr_name` | `cn` | Display-name attribute. |
| `timeout` | `5s` | Bounds dial, search and bind. |
| `start_tls` | `false` | Upgrade a plaintext `ldap://` connection before any credential crosses it. A failed upgrade is an error, never a fallback to plaintext. |
| `insecure_skip_verify` | `false` | Disables certificate verification. For a lab, and nothing else — it logs a warning at startup. |

`auth.User.ID` is the entry's DN.

## What this backend refuses to do

- **Authenticate an empty password.** RFC 4513 §5.1.2 makes a bind with a
  DN and an empty password an *unauthenticated* bind, which a directory may
  answer with success. It is rejected before a connection is opened.
- **Substitute an unescaped username into the filter.** `*)(uid=*` would
  otherwise match every entry in the subtree and the first one found would
  become the account logged into.
- **Pick one entry when the search matches several.** Ambiguity is a
  rejection; otherwise the account somebody authenticates into depends on
  directory ordering.
- **Report a directory problem as a wrong password.** Only result code 49
  is a rejection. Everything else — an unreachable host, a service account
  whose password was rotated, a broken `base_dn` — is *unavailable*, which
  is what lets the local account behind this backend still work.

**Timing.** An absent user still costs a bind, so this code does not add a
difference an attacker could measure from the login form to enumerate
users. It does not make the exchange constant-time and does not claim to:
the directory's own timing still varies.

## What this module compiles

Implementing this backend imports `pkg/auth/backend`, the leaf package that
holds the contract and nothing else. That is the difference between a
provider you can audit and one you cannot:

| | third-party packages linked |
|---|---|
| against `pkg/auth` (until v1.16.1) | 235 |
| against `pkg/auth/backend` | **11** |

The eleven are the LDAP client and its ASN.1 and NTLM support, the
configuration decoder, and `golang.org/x/crypto` — what an LDAP backend
needs, and nothing that belongs to somebody else's deployment. The 235
included the AWS, Azure and Google Cloud SDKs, Prometheus, OpenTelemetry,
Redis and a SQL driver, none of which this backend can reach.

The module's `go.mod` still lists those modules as indirect requirements,
because the live test starts the whole framework through `pkg/app` to prove
the wire end to end. They are not compiled by anyone who imports this
package; see ADR-026 for what is and is not measured.

## Running the tests

The unit tests need nothing. The live tests run against a real directory
and skip without one:

```bash
docker run -d --name nucleus-ldap-test -p 3890:389 \
  -e LDAP_ORGANISATION="Nucleus" -e LDAP_DOMAIN="example.org" \
  -e LDAP_ADMIN_PASSWORD="adminpassword" osixia/openldap:1.5.0
docker cp testdata/seed.ldif nucleus-ldap-test:/tmp/seed.ldif
docker exec nucleus-ldap-test ldapadd -x -H ldap://localhost \
  -D "cn=admin,dc=example,dc=org" -w adminpassword -f /tmp/seed.ldif

NUCLEUS_LDAP_URL=ldap://127.0.0.1:3890 go test ./...
```

Those are the same commands the `LDAP (real OpenLDAP)` lane runs in CI.

### Conformance

The backend is graded by the contract's own suite, the same four lines a
third party writes:

```go
backendtest.Run(t, backendtest.Suite{
    New:           func() (backend.Backend, error) { return conformanceBackend() },
    ValidUser:     "ana",
    ValidPassword: "correcta",
    UnknownUser:   "nadie",
    Unavailable:   unreachableBackend,
})
```

The fixture it runs against answers an empty-password bind with SUCCESS,
the way RFC 4513 §5.1.2 entitles a real directory to. That detail is the
point: the first draft of the fixture rejected it, and the suite passed
with the backend's own empty-password guard deleted. A fixture that is
kinder than a real directory grades nothing.
