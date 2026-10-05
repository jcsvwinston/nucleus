---
sidebar_position: 4
title: Billing (Stripe)
covers:
  - pkg/nucleus.BillingFrom
  - pkg/nucleus.BillingWebhook
  - pkg/nucleus.BillingSource
---

# Billing (Stripe)

An application that sells subscriptions needs four things from its billing
provider: a customer, a page where the customer pays, the state of the
subscription afterwards, and to hear when that state changes. The `stripe`
catalog entry gives an application those four through
[Stripe](https://stripe.com), behind a provider-neutral package,
`pkg/billing`:

- **customers**, linked to the application's own users by a reference;
- a hosted **Checkout** for a subscription, and the hosted **Customer
  portal** where a customer updates a card or cancels;
- a subscription's **state**;
- the **events** of a Stripe webhook — a subscription created, updated or
  canceled, a payment that failed — verified, stored once, and delivered to
  the application's handlers as typed values.

Card data never reaches the application: Checkout and the portal are
Stripe's pages.

The Stripe provider ships as its own module,
`github.com/jcsvwinston/nucleus/providers/billing-stripe`. The Stripe SDK is
linked by the applications that add it and by no other.

## Install

```bash
nucleus add stripe
```

The command does four things: `go get` of the module at the version released
with the CLI, the blank import in `main.go`, `Mount(nucleus.BillingWebhook())`
in the `nucleus.New()` chain, and this block in `nucleus.yml` (written only
when neither `billing` nor `outbox` is set there; printed to merge by hand
otherwise):

```yaml
billing:
  provider: stripe
  stripe:
    secret_key: env:STRIPE_SECRET_KEY
    webhook_secret: env:STRIPE_WEBHOOK_SECRET
outbox:
  enabled: true
```

Then export the two keys:

```bash
export STRIPE_SECRET_KEY=rk_test_…       # Developers → API keys → a restricted key
export STRIPE_WEBHOOK_SECRET=whsec_…     # the webhook endpoint's Signing secret
```

Use a **restricted key** (`rk_…`) with write access to Customers, Checkout
Sessions and the Customer portal and read access to Subscriptions; a secret
key (`sk_…`) works too, with every permission it carries.

Point a Stripe webhook endpoint at
`<your public URL>/webhooks/billing/stripe` and subscribe it to
`customer.subscription.created`, `customer.subscription.updated`,
`customer.subscription.deleted` and `invoice.payment_failed`. Create the
endpoint with API version `2026-09-30.endive` (or another of the same
release line, the part after the dot): the module reads events at the
version of the Stripe SDK it pins and refuses others. To receive events on
a development machine, the Stripe CLI forwards them and prints the signing
secret to export:

```bash
stripe listen --forward-to localhost:8080/webhooks/billing/stripe
```

The application refuses to start while a key is missing, and names the
variable.

## Configuration

| key (`billing.*`) | default | what it does |
|---|---|---|
| `provider` | none | The billing provider. Empty: the application does not bill. `stripe` without the module in the build is refused at startup with `nucleus add stripe`. |
| `stripe.secret_key` | `env:STRIPE_SECRET_KEY` | A **reference** to the API key: `env:NAME` reads an environment variable; `aws-sm:<secret-id>[#json-key]` reads AWS Secrets Manager once `nucleus add aws-sm` is in the build. |
| `stripe.webhook_secret` | `env:STRIPE_WEBHOOK_SECRET` | A reference to the endpoint's signing secret (`whsec_…`). |
| `stripe.tolerance` | `5m` | How old a delivery's signature may be, between 1s and 15m. It bounds how long a captured delivery could be replayed; inside it, the event id is recognised (below). |
| `stripe.api_url` | Stripe's | Another address for the Stripe API — for `stripe-mock` or a test double. https only, except on the loopback interface. |

The keys are references, never the keys: a Stripe key written in
`nucleus.yml` (`secret_key: sk_live_…`) is refused, and the refusal says to
rotate it. So are a publishable key (`pk_…`), a value that is not a Stripe
key, and a signing secret that is not `whsec_…`. No message repeats a key.
A key under `billing.stripe` the module does not declare stops the boot,
naming it; with the module not in the build, each key is reported
`not installed: nucleus add stripe`.

The webhook route stores every event in the [outbox](events.md) before it
answers, which is why the block turns the outbox on; without it,
`BillingWebhook()` refuses to start and says so.

## Using it from a module

A module takes the application's billing in `OnStart`, registers the
handlers its events go to, and keeps it for its routes:

```go
import (
    "context"
    "errors"

    "github.com/jcsvwinston/nucleus/pkg/billing"
    "github.com/jcsvwinston/nucleus/pkg/nucleus"
)

type plans struct {
    billing *billing.Billing
    store   *Store // your table of users and their plans
}

func Module() nucleus.ModuleSpec {
    m := &plans{}
    return nucleus.Module[struct{}]{
        Name:   "plans",
        Prefix: "/plans",
        OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
            b, ok := nucleus.BillingFrom(rt)
            if !ok {
                return errors.New("plans: needs billing (nucleus add stripe)")
            }
            m.billing, m.store = b, NewStore(rt.DB())
            if err := b.On(billing.SubscriptionUpdated, m.sync); err != nil {
                return err
            }
            return b.On(billing.SubscriptionCanceled, m.sync)
        },
        Routes: func(r nucleus.Router, _ struct{}) {
            r.Post("/checkout", m.checkout)
        },
    }.Build()
}

// checkout sends the signed-in user to Stripe's Checkout.
func (m *plans) checkout(c *nucleus.Context) error {
    userID := currentUserID(c) // however your application identifies the user
    co, err := m.billing.CreateCheckout(c.Request.Context(), billing.CheckoutRequest{
        Price:      "price_…",                 // from the Stripe dashboard
        Reference:  userID,                    // comes back on every event
        SuccessURL: "https://app.example.com/plans/welcome",
        CancelURL:  "https://app.example.com/pricing",
    })
    if err != nil {
        return err
    }
    return c.Redirect(303, co.URL)
}

// sync keeps the user's plan in step with Stripe. It may run more than
// once for the same event (see below), so it writes the state, rather
// than applying a change.
func (m *plans) sync(ctx context.Context, ev billing.Event) error {
    s := ev.Subscription
    return m.store.SetPlan(ctx, s.Reference, s.ID, s.Status.Active(), s.CurrentPeriodEnd)
}
```

`CreatePortal` gives the URL of the Customer portal for a customer, and
`Subscription(ctx, id)` reads a subscription's current state from Stripe —
a call to Stripe; for the decision taken on every request, keep the state
the events bring.

| `Billing` method | what it does |
|---|---|
| `CreateCustomer(ctx, CustomerRequest)` | A customer, with `Email` (required), `Name`, `Reference` (your user id, stored on the customer) and `Metadata`. Returns its id: keep it on your user. |
| `CreateCheckout(ctx, CheckoutRequest)` | A Checkout Session in subscription mode for `Price` × `Quantity` (default 1), for an existing `Customer` or one Checkout creates (`CustomerEmail` prefilled), with `TrialDays`, returning to absolute `SuccessURL` / `CancelURL`. `Reference` and `Metadata` go on the subscription it creates. The payment methods are the ones the Stripe account enables. |
| `CreatePortal(ctx, PortalRequest)` | A Customer portal session for `Customer`, returning to `ReturnURL`. |
| `Subscription(ctx, id)` | The subscription's state; `billing.ErrNotFound` for one Stripe does not know. |
| `On(type, handler)` | Registers a handler for one of the four event types. Register in `OnStart`. |

A request missing what it needs — a price, an absolute return URL — is
refused with `billing.ErrInvalidRequest` before it reaches Stripe. Stripe's
own refusals come back as errors carrying its message, code and request id.

## The events

| type | when | carries |
|---|---|---|
| `billing.SubscriptionCreated` | `customer.subscription.created`: a checkout completed, typically. The status may still be `incomplete` while the first payment settles. | `Subscription` |
| `billing.SubscriptionUpdated` | `customer.subscription.updated`: the status changed (`active`, `past_due`, `unpaid`…), the price or quantity changed, a cancellation was scheduled for the period's end. | `Subscription` |
| `billing.SubscriptionCanceled` | `customer.subscription.deleted`: the subscription ended. | `Subscription` |
| `billing.PaymentFailed` | `invoice.payment_failed`: a payment failed; Stripe retries on its schedule (`NextAttemptAt`) and the subscription's status follows in an update. | `Payment` |

A `Subscription` has its `ID`, `Customer`, `Status`, the `Reference` given
to `CreateCheckout`, the first item's `Price` and `Quantity`,
`CurrentPeriodEnd`, `CancelAtPeriodEnd`, `CanceledAt` and `Metadata`.
`Status.Active()` is true for `active` and `trialing`; whether a `past_due`
customer keeps access while Stripe retries is the application's call. A
`Payment` has the `Invoice`, `Customer`, `Subscription`, `Reference`,
`AmountDue` in the currency's minor unit, `Currency`, `AttemptCount` and
`NextAttemptAt`. Every event has its Stripe `ID`, `OccurredAt`, and `Live`
(false in test mode).

## What the webhook route does with a delivery

`POST /webhooks/billing/stripe` (under `webhooks_prefix`) accepts POST only,
caps the body at 512 KiB, and is exempt from CSRF because it authenticates
by signature. Then:

1. **It verifies before it reads.** The Stripe SDK checks the
   `Stripe-Signature` header — an HMAC-SHA256 of the timestamp and the raw
   body under the signing secret, compared in constant time — and that the
   timestamp is within `tolerance`. Nothing in the body is looked at until
   that passes.
2. **It stores the event once.** The event goes into the outbox under its
   Stripe id before the route answers. A second delivery of the same event
   — Stripe's retry after a lost answer, or a captured delivery replayed
   inside the tolerance — finds it there and is not delivered again.
3. **It answers Stripe without waiting for your handlers.** The outbox
   delivers the event to the handlers right after, and retries a handler
   that fails, with backoff; when the attempts run out the event stays in
   the outbox's dead letter with the reason, where it can be requeued.

| delivery | answer | delivered |
|---|---|---|
| verified, one of the four events | `200 {"received":true}` | once, to every handler of the type |
| the same event again | `200 {"received":true,"duplicate":true}` | no |
| verified, another event type | `200 {"received":true,"delivered":false}` | no — Stripe stops retrying it |
| no signature, a wrong one, another body, a timestamp outside the tolerance | `400` | no |
| verified, at an API version of another release line | `400`, and an error line naming the version to use | no |
| verified and not stored (the database is down) | `500` — Stripe delivers it again | not yet |

Because a handler can be retried — and every handler of the type runs again
when one of them fails — write handlers that set state rather than apply a
change, or that key their work by `ev.ID`.

An outbox bridge of your own routed `billing.*` receives the same events:
a webhook to a data warehouse, an external plugin.

## Linking customers to users

The framework keeps no table of who is which customer. Give your user id as
`Reference` — to `CreateCustomer`, and to `CreateCheckout` — and it comes
back on every event about the subscription; keep the Stripe customer id on
your user if you create customers yourself. An application with
[accounts](auth/accounts.md) passes the account's id.

## Testing without Stripe

`billing.New(name, provider)` wraps a `billing.Provider` of your own, and
`Deliver(ctx, event)` runs the handlers registered for an event, so a
module's billing code is tested without a network. Against the real
module, `stripe.api_url` points the API at
[stripe-mock](https://github.com/stripe/stripe-mock) or an `httptest`
server, and a delivery is signed the way Stripe signs it:
`Stripe-Signature: t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>" under the signing secret>`.

## What this does not cover

- Invoicing, tax, proration, dunning, refunds and disputes: Stripe does
  them, configured in its dashboard.
- Prices and products: created in Stripe; the application passes a price
  id.
- One-time payments, usage-based billing and metering, and marketplaces
  (Stripe Connect).
- A table of customers or subscriptions in your database: the events bring
  the state; storing it is the application's.
- What a plan entitles a user to.
- More than one provider at a time.
- Other Stripe events (`checkout.session.completed`, `invoice.paid`, …):
  verified, acknowledged and dropped.
- Allowlisting Stripe's IP addresses in front of the route: do it at your
  load balancer for defence in depth.

## Writing a provider of your own

The Stripe module uses nothing private. A provider implements
`billing.Provider` — `CreateCustomer`, `CreateCheckout`, `CreatePortal`,
`Subscription` and `ParseWebhook` — and registers from an `init`:

```go
func init() {
    billing.MustRegister("paddle", func(cfg billing.Config) (billing.Provider, error) {
        var c Config
        if err := cfg.Bind(&c); err != nil { // billing.paddle.*, strictly
            return nil, err
        }
        return newProvider(c)
    })
}
```

`ParseWebhook` verifies first and returns `billing.ErrSignature` for
anything that fails verification, whatever the event's type;
`billing.ErrUnsupportedEvent` for a verified event of a type it does not
map; and otherwise an `Event` with the provider's event id — which is what
makes a second delivery a duplicate. The framework answers, stores and
delivers.
