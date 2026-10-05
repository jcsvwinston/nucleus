---
sidebar_position: 6
title: SAML sign-in
covers:
  - pkg/auth.FederatedMetadataPath
  - pkg/auth.FederatedSet.ServiceMetadata
  - pkg/auth.FederatedSet.PublishesServiceMetadata
  - pkg/auth.FederatedSet.CallbackIsCrossSiteFormPost
  - pkg/nucleus.FederatedSignIn
config_keys: []
---

# SAML sign-in

`providers/auth-saml` is a SAML 2.0 **service provider** for the
[federated sign-in seam](./backends-and-federation.md#federated-sign-in-oidc-saml):
a person signs in at an identity provider — Okta, Microsoft Entra ID, Google
Workspace, Keycloak, Shibboleth, ADFS — and comes back to the application
with a signed assertion. It plugs into the same `auth_federated` declaration,
the same `nucleus.FederatedSignIn()` routes and the same session keys as
[OIDC](./backends-and-federation.md#oidc).

It is a module of its own, unlike OIDC, because SAML needs an XML-signature
implementation. The module uses a maintained one instead of its own:
[`github.com/crewjam/saml`](https://github.com/crewjam/saml) for the
protocol, which verifies signatures with
[`github.com/russellhaering/goxmldsig`](https://github.com/russellhaering/goxmldsig).
An application that does not speak SAML links neither.

## Install

```bash
nucleus add saml
```

The command fetches `github.com/jcsvwinston/nucleus/providers/auth-saml` at
the version released with your CLI, writes its blank import, adds
`Mount(nucleus.FederatedSignIn())` to the `nucleus.New()` chain of `main.go`,
and appends an instance named `corp` to `nucleus.yml` — only when none of
`public_base_url`, `auth_federated` and `auth` is set there yet; otherwise it
prints the block for you to merge:

```yaml
public_base_url: http://localhost:8080
auth_federated:
  - name: corp
    provider: saml
auth:
  corp:
    sp_entity_id: http://localhost:8080/auth/corp/metadata
    idp_metadata_url: https://idp.example.com/saml/metadata
```

Replace `idp_metadata_url` with your identity provider's metadata, and
`public_base_url` and `sp_entity_id` with the address the browser uses. The
application starts with the placeholder in place; `/auth/corp/start` answers
502 until the metadata loads.

By hand, it is the same three things:

```go
import _ "github.com/jcsvwinston/nucleus/providers/auth-saml"
```

```go
nucleus.New().
    FromConfigFile("nucleus.yml").
    Mount(nucleus.FederatedSignIn()).
    Start()
```

## Register the application with the identity provider

`FederatedSignIn` serves, for each SAML instance:

| route | what it is |
|---|---|
| `GET /auth/<name>/metadata` | this application's SAML metadata: its entity ID, one AssertionConsumerService with the HTTP-POST binding at the callback, `WantAssertionsSigned="true"`, and the signing certificate when one is configured |
| `GET /auth/<name>/start` | sends the browser to the identity provider with an AuthnRequest (HTTP-Redirect binding) |
| `POST /auth/<name>/callback` | receives the Response (HTTP-POST binding), verifies it, rotates the session and records the identity |

Most identity providers import the metadata URL. Those that are configured by
hand need three values:

- **Entity ID** (audience): the value of `sp_entity_id`.
- **ACS URL**: `<public_base_url>/auth/<name>/callback`, binding HTTP-POST.
- **Assertions signed**: on. A Response whose assertion is not itself signed
  is refused, even when the Response around it is signed.

Leave assertion encryption off: encrypted assertions are refused (see
[What this does not cover](#what-this-does-not-cover)).

## Settings

Everything lives under `auth.<name>.*`. A key not listed here stops the boot.

| Key | Default | Meaning |
|---|---|---|
| `sp_entity_id` | — (required) | This application's entity ID: the audience every assertion must carry. Conventionally `<public_base_url>/auth/<name>/metadata`. |
| `idp_metadata_url` | — | The identity provider's metadata. Fetched on the first sign-in, not at boot, and again every `metadata_refresh`. `https` only; plain `http` is accepted for a loopback address. |
| `idp_metadata_file` | — | The same document from a file, read at boot. Exactly one of the two is set. |
| `idp_entity_id` | `""` | Picks the identity provider in a metadata document that describes several (an `EntitiesDescriptor`); when set, the document's entity ID must match. |
| `sp_certificate_file`, `sp_key_file` | `""` | This application's key pair, PEM, both or neither. With them every AuthnRequest is signed (RSA-SHA256, or ECDSA-SHA256), and the certificate is published in the metadata. RSA keys under 2048 bits and a key that is not the certificate's are refused at boot. |
| `name_id_format` | `""` | The NameID format the AuthnRequest asks for: `persistent`, `email`, `transient`, `unspecified` or a full `urn:oasis:names:tc:SAML` URI. Empty lets the identity provider choose. |
| `email_attribute`, `username_attribute` | `""` | The attribute (by `Name` or `FriendlyName`) mapped onto the identity's email and username. Empty looks for the common names: `mail`, `email`, `uid`, `username`, their OIDs and the ADFS/Entra claim URIs. |
| `role_attribute` | `""` | The attribute whose values become the identity's roles. Empty maps no roles. |
| `metadata_refresh` | `24h` | How long fetched metadata is used before it is fetched again (minimum `1m`). A refresh that fails keeps the metadata that loaded last and is retried a minute later. |
| `timeout` | `10s` | Bounds a metadata fetch. |

## The identity

After a successful callback the session carries the identity under the same
keys OIDC writes — `nucleus.SessionKeyFederatedInstance`,
`SessionKeyFederatedUserID`, `SessionKeyFederatedUsername` and
`SessionKeyFederatedEmail`:

- **ID** is the assertion's NameID. Ask for `name_id_format: persistent` when
  the application keeps anything keyed by it: a `transient` NameID changes on
  every sign-in.
- **Email** is the email attribute, or the NameID when its format is
  `emailAddress` and no attribute carries one.
- **Username** is the username attribute, then the email, then the NameID.
- **Roles** are the values of `role_attribute`.

## What it verifies

Only a Response that answers a sign-in this application started, signed by
the identity provider its metadata names, signs anybody in. Each of these is
a test in the module, against a stand-in identity provider that signs with a
key of its own:

- **The assertion is signed** by a certificate in the identity provider's
  metadata. Unsigned, signed by another key, or unsigned inside a signed
  Response: refused. A signature on the Response, when there is one, must
  verify too.
- **It is for this application, now**: the `AudienceRestriction` names
  `sp_entity_id` (an assertion with none is refused — the library alone
  would accept it), the bearer `SubjectConfirmation`'s `Recipient` — and
  the Response's `Destination`, when it carries one — is the callback URL,
  and `NotBefore` /
  `NotOnOrAfter` include now with three minutes of clock skew. A Response
  issued more than 90 seconds ago is refused.
- **It answers the request the start route sent**: `InResponseTo` is that
  AuthnRequest's ID, and `RelayState` is the sign-in's nonce.
  Identity-provider-initiated sign-in is off and is not a setting.
- **It is used once**: the framework spends the sign-in's state on the first
  callback, a Response posted into another sign-in does not answer its
  request, and the provider remembers each assertion it accepted until it
  expires.
- **The document has one reading**: exactly one plaintext assertion, no two
  elements with the same ID, no `EncryptedAssertion`. These are the shapes
  signature-wrapping attacks need — a second assertion, or the signed one
  moved where the verifier and the reader disagree — and the tests include
  them. An XML comment inside a signed NameID does not shorten it.
- **Only the POST binding**: a Response in the query string and the artifact
  binding are refused, and a SAMLResponse over 512 KiB is not read.

Signature verification itself is the library's: this module never computes
a digest or checks a signature on its own.

## HTTPS, and the state cookie

The identity provider returns the browser with a form POST from its own
site. Browsers do not send a `SameSite=Lax` cookie with a cross-site POST,
so for a SAML instance `FederatedSignIn` sets the sign-in's state cookie
`SameSite=None; Secure` when `public_base_url` is `https`. Over plain `http`
the cookie stays `Lax` (browsers refuse `SameSite=None` without `Secure`),
and the sign-in works only with an identity provider on the same site — a
local one for development. Serve it over `https` everywhere else.

With `csrf_enabled`, `FederatedSignIn` exempts each instance's callback path
from the CSRF check: the identity provider's form cannot carry the
application's token, and the state cookie and the Response's verification
are what bind the callback to the sign-in. The exemption is the callback
path, never the `/auth/` prefix.

## Test your sign-in

The module ships the identity provider its own tests use,
`github.com/jcsvwinston/nucleus/providers/auth-saml/samltest`: it generates
a key and a certificate, serves metadata and an `/sso` endpoint that answers
with the auto-posting form, and can be told to answer wrongly — an unsigned
assertion, another audience, an expired window — so an application can test
that it refuses what it must:

```go
idp, _ := samltest.New()
idp.Start()
defer idp.Close()
// auth.corp.idp_metadata_url: idp.MetadataURL()
```

The catalog bench drives the same identity provider through `nucleus add
saml` on the api starter (`EN-02`).

## What this does not cover

- **Single logout (SLO).** Signing out of the application does not sign the
  person out of the identity provider, and a logout request from the identity
  provider is not served.
- **Encrypted assertions.** Refused, and no encryption key is published in
  the metadata. The assertion travels over the browser; serve it over `https`.
- **Identity-provider-initiated sign-in.** Refused, by design.
- **The artifact binding** and **the HTTP-POST binding for the
  AuthnRequest**: the request goes by redirect, the Response comes back by
  POST.
- **Signed metadata.** The identity provider's metadata is trusted for the
  channel it arrives on (a file you placed, or `https`); its own XML
  signature is not checked.
- **Several replicas.** A pending sign-in and the memory of used assertions
  live in the process, as the framework's pending flows do: behind a load
  balancer, a callback must reach the replica that started its sign-in
  (sticky sessions).
- **Signed SP metadata, contact and organisation elements, attribute
  consuming services** in the metadata this application publishes.
- **Per-identity-provider quirks** beyond the defaults above (attribute names
  are configurable; claim transformations are not).
