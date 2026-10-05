# ADR-037: Billing is a provider-neutral seam in the framework; Stripe is a module behind it

- Status: Accepted
- Date: 2026-10-05
- Deciders: jcsvwinston
- Related: [ADR-023](ADR-023-provider-registries.md) (registries and the
  per-name configuration subtree this follows),
  [ADR-025](ADR-025-plugin-contract-leaf-package.md) and
  [ADR-026](ADR-026-storage-contract-leaf-package.md) (a contract a third
  party implements lives in a leaf package),
  [ADR-030](ADR-030-cloud-backends-as-modules.md) and
  [ADR-031](ADR-031-drivers-and-exporters-as-modules.md) (a vendor SDK lives
  in a module of its own), [ADR-024](ADR-024-ldap-provider-module.md) (a
  module that needs API its pinned release lacks pins a pseudo-version),
  [ADR-029](ADR-029-request-interceptors.md) (the other way out of that,
  and why it does not fit here), [ADR-034](ADR-034-catalog-embedded-pinned.md)
  and [ADR-035](ADR-035-catalog-entries-carry-their-wiring.md) (the catalog
  entry and its recipe), the A11 bench (`docs/catalog-bench.md`, `EN-08`,
  `CAT-01`)

## Context

The A11 catalog names `stripe` as an entry, and the owner decided on
2026-10-04 that it is built. The bench recorded it absent (`EN-08`): no
module, no dependency on stripe-go, and the only mention of billing in the
framework was a "stretch" line in the plugin SDK reference —
`subscription.create` and `subscription.cancel` as capabilities with no
schema.

What an application needs from its framework to bill is not the vendor's
API; the vendor's SDK already is that. It is the part every integration
rewrites and gets wrong in the same places:

- **a webhook route that verifies before it reads.** The provider tells the
  application what happened to a subscription only through webhooks; a
  route that parses the body before checking the signature, or checks the
  signature and not its age, is a route anybody can drive;
- **a second delivery that changes nothing.** Providers deliver an event
  again when they did not see the first answer, and a replayed capture
  inside the signature's tolerance looks the same;
- **an answer that does not wait for the application.** The provider wants
  a 2xx quickly and retries otherwise; the application's handler may be
  slow, or failing, and must be retried on the application's side without
  the provider sending the event again;
- **values the application can program against** that do not change when
  the provider does.

And the SDK must not become a dependency of applications that do not bill,
which is the rule every optional module of this repository follows.

## Decision

**1. `pkg/billing` is the seam, a leaf package.** It holds the values an
application exchanges with a provider (`CustomerRequest`, `Customer`,
`CheckoutRequest`, `Checkout`, `PortalRequest`, `Portal`, `Subscription`
with a `Status`, `Payment`, `Event`), the contract a provider implements —
`Provider`, five methods: `CreateCustomer`, `CreateCheckout`,
`CreatePortal`, `Subscription`, `ParseWebhook` — four sentinel errors
(`ErrSignature`, `ErrUnsupportedEvent`, `ErrNotFound`,
`ErrInvalidRequest`), and the registry (`Register`, `MustRegister`,
`RegisteredProviders`, `Open`). It imports the standard library, the
catalog and the provider-configuration binder — two third-party packages,
the floor `TestPluginContract_StaysLight` holds the other contracts to,
and it is held to it too. It is experimental until a second provider has
implemented it.

The seam carries four events, which are also the outbox topics they travel
under: `billing.subscription.created`, `billing.subscription.updated`,
`billing.subscription.canceled` and `billing.payment.failed`. A
subscription event carries the subscription's state as of the event; a
payment failure carries the invoice, the amount in minor units, the attempt
count and the next attempt.

`Billing` is what an application holds: the provider behind requests
validated the same way whichever provider it is (a price, absolute http(s)
return URLs, an email), a `ParseWebhook` that checks what the provider
returned (an id, a carried type, the part the type promises) and stamps
the provider's name, and the handlers its events are delivered to (`On`,
`Deliver` — every handler of the type, in order, a panic recovered and
counted as a failure, failures joined).

**2. Configuration selects a provider and the provider reads its own
subtree.** `billing.provider` names it; `billing.<name>.*` is the
provider's, exempt from the unknown-key check only for a registered name
(`internal/providerns`, both configuration paths), bound strictly by the
provider. A name this project publishes and the binary does not link is
refused naming `nucleus add <name>`, at load for its keys and at boot for
the selection.

**3. The framework builds it; a module takes it.** `app.New` opens the
provider on every stack, `WithoutDefaults()` included, after the outbox,
into `App.Billing`. A module reaches it with `nucleus.BillingFrom(rt)`, an
optional interface beside the published `Runtime` (which does not grow
before the major), and registers its handlers in `OnStart`, before the
outbox starts delivering.

**4. The webhook route is a module the framework ships,
`nucleus.BillingWebhook()`.** It mounts at
`<webhooks_prefix>/billing/<provider>` through the module webhook registry,
so it inherits its checks — POST only, the body capped (512 KiB here), the
prefix exempt from CSRF because it authenticates by signature — and the
boot log says what verifies it instead of warning that nothing does. For
each delivery:

- the provider verifies it (`ParseWebhook`): signature and timestamp
  first, the body read only after. A failure is `ErrSignature`, answered
  400 and delivered nowhere; a forgery of a type the seam does not carry
  is a forgery, not an unsupported event;
- a verified event of a type the seam does not carry is answered 200 —
  the provider stops retrying it — and stored nowhere;
- a verified event the module cannot read (an endpoint at an API version
  of another release line) is answered 400 and logged at ERROR: an
  operator has to act, another delivery will not help;
- a carried event is stored in the transactional outbox under the id
  `billing:<provider>:<event id>` before the answer. The outbox's primary
  key makes a second delivery of the same event — the provider's retry or
  a replay inside the tolerance — a unique violation, answered 200
  `duplicate` and not delivered again. Not stored, not acknowledged: 500,
  and the provider delivers it again.

The outbox delivers the stored event to the application's handlers through
a bridge, `billing-handlers`, routed `billing.*`. A handler's error is a
retry with the outbox's backoff and, when the attempts run out, the dead
letter with the reason; a payload that does not decode fails at once. An
operator's own bridge routed `billing.*` receives the events too. The
route therefore needs `outbox.enabled: true`, and refuses to start without
it, or without a provider.

**5. Stripe is `providers/billing-stripe`, over the official SDK.** The
module pins `github.com/stripe/stripe-go/v87` (API version
`2026-09-30.endive`) and registers `stripe` from its `init`. Checkout is a
hosted Checkout Session in subscription mode with no
`payment_method_types` (the account's own, from the Dashboard); the
application's reference goes on the subscription's metadata
(`nucleus_reference`) and as `client_reference_id`, so every event about
the subscription says whose it is without a lookup. The two keys are
references — `env:NAME` or `aws-sm:<secret-id>[#key]` — and a Stripe key
written in clear in the configuration is refused, as are a publishable key,
a value that is not a key, a signing secret that is not `whsec_…`, a
tolerance outside 1s–15m and an `api_url` that is not https outside the
loopback interface; no message repeats a key. Verification is the SDK's
(`webhook.ConstructEventWithOptions`: HMAC-SHA256 of the timestamp and the
raw body, compared in constant time, the timestamp within
`billing.stripe.tolerance`, 5m by default), and it refuses an event
rendered at an API version of another release line. The four Stripe events
mapped are `customer.subscription.created`, `.updated`, `.deleted` and
`invoice.payment_failed`. Stripe's errors reach the application as text —
message, status, code, request id — and none of the SDK's types; a missing
resource is `billing.ErrNotFound`.

**6. `nucleus add stripe` installs and wires it.** The entry ships as a
module and carries a recipe (ADR-035): the blank import,
`Mount(nucleus.BillingWebhook())` spliced into the `nucleus.New()` chain —
the first module entry whose recipe has a chain call — and the block:
`billing.provider: stripe`, the two keys as `env:` references, and
`outbox.enabled: true`.

### Why not a method satisfied structurally, as ADR-029 did

ADR-029's amendment kept `providers/errors-sentry` building against the
release it pins by making the new seam one method whose signature uses
standard-library types only. A billing provider's contract is a set of
typed values; the same trick would make every method take and return JSON
bytes, and every provider written after this one would implement that
forever to save one release train. The module instead follows ADR-024's
rule: until the release that carries `pkg/billing` is tagged, it pins a
pseudo-version of the framework commit that introduced it. That commit
resolves from the module proxy, so the module still builds, vets and tidies
with the workspace off (`check_modules_standalone.sh`); the train's floor
script leaves a floor ahead of the last tag as it is; the module's first
tag carries it; the next cut raises it to the release, as it does for
every module.

## What this does not cover

- Invoicing, tax, proration, dunning, refunds and disputes: the provider
  does them, configured in its dashboard.
- Prices and products: created in the provider; the application passes a
  price id.
- One-time payments, usage-based billing and metering, marketplaces
  (Connect).
- Storing who is which customer: the application keeps the customer id on
  its own user (`CreateCustomer` returns it; the reference travels on every
  event), and no table of the framework mirrors the provider's
  subscriptions.
- Entitlements: `Status.Active()` answers "active or trialing"; what a plan
  grants is the application's.
- More than one provider at a time, and the plugin SDK's `subscription.*`
  capabilities for an external process: the reference keeps them as a
  stretch line.
- Other Stripe events (`checkout.session.completed`, `invoice.paid`…):
  verified, acknowledged and dropped; the seam grows by adding a type.
- Network controls in front of the route (allowlisting Stripe's addresses):
  the operator's.

## Consequences

- Additive (QADR-0010): a new package, an `App` field and two `Config`
  fields (in the extension-surface and API baselines), `BillingFrom`,
  `BillingSource`, `BillingWebhook`, one exported constant in `pkg/app`;
  an unexported field on `WebhookSpec`; a new configuration namespace.
  Nothing that boots today changes.
- An application that does not bill links `pkg/billing` (it is in
  `pkg/app`'s graph) and not stripe-go; the provider-modules CI lane
  asserts the SDK is not reachable from `pkg/app`.
- Release mechanics: a release-please package for `providers/billing-stripe`
  at `0.0.0` so the first cut is v0.1.0, its key in `modules.json`, the
  dependabot entry, the provider-modules lane. The umbrella's
  `versions.yaml` lists this repository's modules by hand; the set that
  first certifies the module adds its row.
- The bench records `EN-08` present: its wiring check drives deliveries
  signed the way Stripe signs them through the starter `nucleus add stripe`
  leaves and reads the typed event the application's handler received, once.
