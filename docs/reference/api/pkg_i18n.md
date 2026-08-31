# pkg/i18n — contract

Lifecycle: `experimental`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`Load(dir)`, `Catalog` (`Locales`, `Lookup`), `New(catalog, defaultLocale)`,
`Translator` (`T`, `Negotiate`, `Middleware`, `DefaultLocale`, `Locales`),
context helpers `WithLocale`, `Locale`, `WithTranslator`,
`TranslatorFromContext`, and package-level `T(ctx, key, args...)`.

## Notes

Runtime counterpart of the `makemessages`/`compilemessages` CLI pair
(audit PR-GAP-03/NF-4): `Load` reads the compiled JSON bundles the CLI
writes under `<locales_path>/<locale>/LC_MESSAGES/`, `Middleware`
negotiates the request locale from `Accept-Language` (RFC 9110 q-values,
base-language fallback, `default_locale` as the terminal fallback) and the
`T` helpers resolve keys with the chain requested locale → base language →
default locale → the key itself. `app.New` mounts the middleware
automatically when at least one compiled catalog exists and exposes the
translator as `App.I18n`; `router.Context.T` is the handler-side helper.
Experimental: plural forms and per-domain lookup may still land before the
surface freezes. Pure stdlib.
