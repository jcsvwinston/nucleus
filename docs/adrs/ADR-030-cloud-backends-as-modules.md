# ADR-030: The cloud backends ship as their own modules

Reference date: 2026-08-31.
Status: Accepted.
Related: [ADR-023](ADR-023-provider-registries.md) (the registries this uses),
[ADR-024](ADR-024-ldap-provider-module.md) (the sibling-module shape, first
used for LDAP), [ADR-025](ADR-025-plugin-contract-leaf-package.md) (the leaf
package the backends import).

## Context

A hello-world built on this framework — one call to `app.New`, no storage
configured, no secret manager, nothing — linked **1008 packages across 346
modules and produced a 75.6 MB binary**. Measured, not estimated.

Where it came from was not where the plan assumed. Four cloud SDKs accounted
for **42.6 MB of that binary on their own**:

| dependency | packages | entered through |
|---|---|---|
| AWS SDK | 57 | `pkg/auth/secrets` — the Secrets Manager resolver |
| Azure SDK | 37 | `pkg/storage` |
| Google Cloud SDK | 34 | `pkg/storage` |
| MinIO client | 16 | `pkg/storage` — the S3 implementation |

Two things worth stating plainly, because the arc had been planned around
guesses that measurement did not support. The task runtime (`asynq`) was
*already* absent from the graph — there was nothing to extract there. And the
single heaviest entry was **not storage at all** but the AWS Secrets Manager
resolver, which the framework linked so that a deployment could write
`aws-sm:` in front of a JWT key it almost never does.

This is the cost every application paid to have four backends available. It
also contradicted, in the most direct way available, a project that describes
itself as standard-library-first.

## Decision

**S3, GCS, Azure Blob and AWS Secrets Manager leave the framework and become
sibling modules**, in the shape ADR-024 established for LDAP:

- `providers/storage-s3`, `providers/storage-gcs`, `providers/storage-azure`
- `providers/secrets-aws`

Each registers itself from an `init()` through the public door its subsystem
already offered — `provider.Register` for storage, and a new
`secrets.RegisterResolver` for managed secret stores, which had no registry
at all: the chain named the AWS resolver in its own struct, so the framework
had to link the SDK to offer a scheme.

The core keeps the backends that need no client library: **local storage** and
**environment-variable secrets**. Those are what a fresh project writes to.

### What a user sees

Configuration does not change. `storage.provider: s3` and `aws-sm:` references
mean exactly what they meant. What changes is that the module has to be in the
build:

```go
import _ "github.com/jcsvwinston/nucleus/providers/storage-s3"
```

A name this project publishes gets the recipe rather than a rejection. Asked
for `s3` without the import, the framework answers with the `go get` line and
the import line, and says what *is* registered — the same treatment ADR-024
gave to auth backends. It fails at construction; it never falls back to local
storage or to the environment, because a silent fallback to a different
backend is how data ends up somewhere nobody intended.

### Compatibility

This lands in a **minor, without a deprecation window**, deliberately:

- Configuration-driven use — the overwhelming majority — keeps working after
  one import line, and the error states that line.
- Code that calls `storage.NewS3Store` directly stops compiling. That is a
  loud, immediate failure with an obvious fix (import the module, call
  `s3.NewS3Store`), not a silent behaviour change.
- It is a packaging change, not a redesign: the same code, in a module of its
  own. A deprecation window would keep every application paying 42.6 MB for
  another release to postpone a one-line edit.

## Consequences

Measured on the same hello-world, after the extraction:

| | before | after |
|---|---|---|
| binary | 75.6 MB | **42.0 MB** |
| modules | 346 | **176** |
| packages | 1008 | **576** |

The framework no longer links a single cloud SDK.

**Two shared helpers surfaced during the split and moved to the contract.**
Key hygiene (`NormalizeKey`, `ValidateKey`, `ValidateKeyPrefix`) lived inside
the S3 implementation although local, GCS, Azure and the cleanup sweeper all
called it; `EscapeURLPath` lived inside GCS and Azure used it. Neither could
stay inside a module the others no longer import. They are exported from
`pkg/storage/provider` now, which is where behaviour every backend depends on
belongs — and their previous home was an accident of who wrote them first.

**Two cross-provider tests split by provider.** One asserted that every
backend validates keys before touching its client; another asserted the
public-URL contract. Both verified a property that is true *per backend*, so
each module now asserts it about itself. The property is unchanged; what
changed is that no module has to import another to state it.

**What this does not close.** The target of the arc is a hello-world under
30 MB and under 150 modules, and 42.0 MB is not there. What remains is
measured and belongs to different subsystems: the OTLP exporter pulls
**gRPC and protobuf (106 packages)** through `pkg/observe`, and the database
drivers registered by `pkg/db` account for **42 more**. Both are the same
shape of problem this ADR solves and neither is decided here.
