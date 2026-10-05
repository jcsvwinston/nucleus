# nucleus/providers/auth-saml

A SAML 2.0 service provider for Nucleus's federated sign-in: a person signs
in at an identity provider (Okta, Microsoft Entra ID, Google Workspace,
Keycloak, Shibboleth, ADFS) and comes back with a signed assertion.

It is a **separate Go module inside the framework's own repository**: it
ships on the framework's release train and under its CI, but it is not in
the framework's `go.mod`. SAML needs an XML-signature implementation, and
this module uses a maintained one —
[`github.com/crewjam/saml`](https://github.com/crewjam/saml) over
[`github.com/russellhaering/goxmldsig`](https://github.com/russellhaering/goxmldsig)
— so an application that does not speak SAML links neither (ADR-030/031).

## Install

```bash
nucleus add saml
```

That fetches this module at the version released with the CLI, writes the
blank import, mounts `nucleus.FederatedSignIn()` and writes an instance into
`nucleus.yml`. By hand:

```go
import _ "github.com/jcsvwinston/nucleus/providers/auth-saml"
```

```yaml
public_base_url: https://app.example.com
auth_federated:
  - name: corp
    provider: saml
auth:
  corp:
    sp_entity_id: https://app.example.com/auth/corp/metadata
    idp_metadata_url: https://idp.example.com/saml/metadata
```

The identity provider is configured from
`https://app.example.com/auth/corp/metadata`, or by hand with the entity ID
above and the ACS URL `https://app.example.com/auth/corp/callback`
(HTTP-POST). It must sign the assertion.

## What it accepts

Service-provider-initiated sign-in only: the AuthnRequest goes out by
HTTP-Redirect, the Response comes back by HTTP-POST, and it must carry
exactly one assertion that is itself signed by a certificate in the
identity provider's metadata, addressed to this application's entity ID and
callback, answering the request the start route sent, inside its validity
window (three minutes of skew), and not seen before. Signature verification
is the library's; this module adds the policy around it and the tests for
each refusal, including signature-wrapping shapes.

Settings, the identity mapping, and what is not covered (single logout,
encrypted assertions, IdP-initiated sign-in, the artifact binding, signed
metadata, several replicas without sticky sessions) are documented on the
site: **Features → Auth & sessions → SAML sign-in**.

## Testing

`samltest` is the identity provider this module's tests use: it signs with
its own key and can be told to answer wrongly. An application can use it to
test its own sign-in.

```bash
go test ./...
```
