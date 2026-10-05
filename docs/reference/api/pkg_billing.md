# pkg/billing — contract

Lifecycle: `experimental` until a second provider has implemented it
(ADR-037). One file per package, like the rest of this directory.

## Contract scope

Values: `CustomerRequest`, `Customer`, `CheckoutRequest`, `Checkout`,
`PortalRequest`, `Portal`, `Subscription`, `Status` (with `Active`) and its
eight constants, `Payment`, `Event`, `EventType` (with `Known`) and its four
constants (`SubscriptionCreated`, `SubscriptionUpdated`,
`SubscriptionCanceled`, `PaymentFailed`), `EventTypes`.

The provider contract: `Provider` (`CreateCustomer`, `CreateCheckout`,
`CreatePortal`, `Subscription`, `ParseWebhook`) and the sentinels
`ErrSignature`, `ErrUnsupportedEvent`, `ErrNotFound`, `ErrInvalidRequest`.

The registry: `Register`, `MustRegister`, `RegisteredProviders`,
`Unregister`, `Open`, `Config` (`Name`, `ProviderConfig`, `Logger`, `Bind`),
`Factory`.

The application's handle: `Billing` (`Name`, `CreateCustomer`,
`CreateCheckout`, `CreatePortal`, `Subscription`, `ParseWebhook`, `On`,
`Deliver`), `New`, `Handler`.

In `pkg/nucleus`: `BillingFrom`, `BillingSource`, `BillingWebhook`. In
`pkg/app`: `App.Billing`, `Config.Billing` (`BillingConfig.Provider`),
`Config.BillingProviderConfig`, `BillingBridgeName`.

## Notes

The package is a leaf: the standard library, the catalog and the
provider-configuration binder, two third-party packages in all, the floor
`TestPluginContract_StaysLight` holds the other contracts to.

`Billing` validates a request before the provider sees it (a price, absolute
http(s) return URLs, an email) and refuses with `ErrInvalidRequest`;
`ParseWebhook` passes the provider's verdict through and checks what a
verified event promises (an id, a carried type, the subscription or the
payment). `Deliver` runs every handler of the type in registration order,
recovers a panic as an error and joins the failures.

The framework opens the provider `billing.provider` names on every stack
(`App.Billing`); `Open` refuses an unregistered name, and for a name this
project publishes and the binary does not link (`stripe`) names
`nucleus add stripe`. `nucleus.BillingWebhook()` serves
`<webhooks_prefix>/billing/<provider>`: the provider verifies, the event is
stored in the outbox under `billing:<provider>:<event id>` (the primary key
makes a second delivery a duplicate, answered 200 and not delivered again),
and the outbox bridge `billing-handlers`, routed `billing.*`, delivers it to
the handlers with the outbox's retry and dead letter. It requires
`outbox.enabled: true`.
