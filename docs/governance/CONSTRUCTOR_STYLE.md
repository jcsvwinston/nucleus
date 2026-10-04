# Constructor Style

Reference date: 2026-10-04.
Status: Current.

How an exported constructor in this repository reports what it cannot work
with. It applies to every new constructor under `pkg/`, and to the ones that
predate it through the deprecation path at the end.

## The rules

1. **Bad input is an error.** A constructor whose input can be wrong returns
   `(T, error)`. Input is anything that reaches it from outside the line that
   calls it: configuration, the environment, a file, a flag, a request, the
   result of another call. A secret that is too short, a name with a
   character the store cannot hold, a required field left empty, two options
   that exclude each other — each one is an error the caller reports, wraps
   or retries.

2. **A constructor does not panic on input.** A panic takes the process down
   at a point the caller did not choose and turns a configuration mistake into
   a stack trace. "It should crash at startup" is the caller's decision: a
   caller that wants a crash checks the error and exits.

3. **A panic is for a programmer error that no input can produce.** What
   counts:
   - a `Must…` wrapper beside a function of the same name without the prefix
     that returns the error — `regexp.MustCompile` beside `regexp.Compile`.
     It is for values the program spells as constants, and for registration
     in an `init()`, where there is no caller to hand an error to
     (`driver.MustRegisterUniqueViolation`, `exporter.MustRegister`);
   - a contract the type system cannot express broken by the calling code
     itself, at a point with no error return — releasing a pooled event twice,
     registering a resource controller that lacks the method its route needs.
     Where the framework can turn it into a boot error, it does (two modules
     claiming one route is a boot error, not a crash);
   - the platform failing in a way the program cannot continue from —
     `crypto/rand` returning an error.

   An "unreachable" panic inside an exported function is not on this list:
   to a reader it looks like a panic on input. Make the state impossible by
   construction instead (the default middleware stack builds its CSRF
   middleware from options it controls, without a panicking wrapper).

4. **One shape for new constructors**:

   ```go
   func NewThing(cfg ThingConfig, logger *slog.Logger) (*Thing, error)
   ```

   Options and required dependencies go in the config struct, so a new option
   is a new field and not a new parameter. The logger is the last parameter;
   the doc comment says what nil means. No parameter is left unused.

5. **Two constructors for one type keep one contract.** If one of them returns
   an error for an input, the other does not panic on the same input.

## What checks it

`DI-07` in the API bench (`internal/apibench/probes_di_test.go`,
`probeConstructorsDontPanic`) reads every non-test Go file under `pkg/`: each
exported top-level function that calls `panic` must either be a `Must…`
wrapper with its error-returning sibling, or carry a `Deprecated:` paragraph
that names an exported function of its package whose last result is an
error. It then calls the error forms with the input the deprecated forms
panic on and expects an error, not a panic. A new constructor that panics on
input turns the bench red.

## When an existing constructor breaks a rule

Its signature does not change: exported signatures are frozen until the next
major (`contracts/baseline/`). Instead, in one change:

1. add the error-returning form beside it;
2. mark the old one `Deprecated:`, naming the new form, a `DEP-YYYY-NNN`
   notice and the removal version (the template is
   [`DEPRECATION_TEMPLATE.md`](DEPRECATION_TEMPLATE.md));
3. move the framework's own callers to the new form.

## Inventory

Measured on 2026-10-04 by the `DI-07` scan, over `pkg/` and the sibling
modules under `providers/`, `drivers/` and `exporters/` (which have none).

| Function | Panicked on | Error form | Status |
|---|---|---|---|
| `auth.NewJWTManager` | a secret shorter than 32 bytes | `auth.NewJWTManagerFromSecret` | deprecated, `DEP-2026-012` |
| `db.NewModuleMigrator` | an empty module name; `/` or NUL in it | `db.NewMigratorFromConfig` | deprecated, `DEP-2026-012` |
| `db.NewModuleFSMigrator` | a nil `fs.FS`; an empty module name; `/` or NUL in it | `db.NewMigratorFromConfig` | deprecated, `DEP-2026-012` |
| `router.CSRFMiddleware` | `EnableXSRFCookie` without a 32-byte key; a `__Host-`/`__Secure-` cookie name with `InsecureCookie` | `router.NewCSRFMiddleware` (already present) | deprecated, `DEP-2026-012` |
| `driver.MustRegisterUniqueViolation` | a duplicate engine or a nil classifier | `driver.RegisterUniqueViolation` | kept: `Must…` for `init()` |
| `exporter.MustRegister` | a duplicate name | `exporter.Register` | kept: `Must…` for `init()` |

Two departures from rule 4 predate it and stay until the major, since
changing them changes a signature:

- the logger's position: `authz.New(logger, policyPath...)` and
  `router.New(logger, opts...)` take it first, `db.New(cfg, logger)` last;
- `router.NewWrapResponseWriter(w, _ int)` keeps an unused parameter.
