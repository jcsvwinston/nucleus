# Storage Guide

A comprehensive guide to the Nucleus storage layer (`pkg/storage`) --- a provider-agnostic file storage abstraction with a durable interface designed to last through v1.x.

## Table of Contents

1. [Overview](#overview)
2. [Supported Providers](#supported-providers)
3. [Configuration](#configuration)
4. [Basic Usage](#basic-usage)
5. [Public vs Private Objects](#public-vs-private-objects)
6. [Multi-Tenant Storage](#multi-tenant-storage)
7. [Signed URLs](#signed-urls)
8. [Circuit Breaker](#circuit-breaker)
9. [Temporary File Cleanup](#temporary-file-cleanup)
10. [Production Checklist](#production-checklist)
11. [Migration from Legacy Config](#migration-from-legacy-config)

---

## Overview

The `pkg/storage` package provides a single, stable `Store` interface that abstracts over multiple cloud storage backends. Application code never changes when switching from local development to S3 in production.

Key design principles:

- **Provider-agnostic**: The same Go code works with S3, GCS, Azure Blob, and local filesystem.
- **Streaming-native**: All reads/writes use `io.Reader`/`io.ReadCloser` so large files never need to be held in memory.
- **Configuration-driven**: Provider selection is entirely controlled by YAML configuration.
- **Tenant-isolated**: Multi-tenant applications automatically receive key prefix isolation.
- **Durable interface**: The `Store` interface is designed to remain stable through v1.x.

Core types:

| Type | Purpose |
|------|---------|
| `storage.Store` | The main interface --- `Put`, `Get`, `Delete`, `Exists`, `List`, `PublicURL`, `SignedURL`, `Copy` |
| `storage.PutOptions` | Configures uploads: `Visibility`, `ContentType`, `Metadata`, `TenantPrefix` |
| `storage.ObjectInfo` | Metadata returned after upload or stat: `Key`, `Size`, `ContentType`, `Visibility`, `UpdatedAt` |
| `storage.Visibility` | `Private` (default) or `Public` --- controls object accessibility |
| `storage.CredentialSource` | Credential resolution: `value`, `env_var`, `file`, `secret_manager` with `env:` references only |
| `storage.ListOptions` | Configures listing: `Prefix`, `Delimiter`, `Limit`, `Marker` |
| `storage.ListResult` | Response from `List()`: `Objects`, `CommonPrefixes`, `NextMarker`, `Truncated` |

---

## Supported Providers

| Provider | Type constant | Use cases |
|----------|--------------|-----------|
| **AWS S3** | `s3` | Production on AWS |
| **MinIO** | `s3` (with `endpoint` + `use_path_style`) | Self-hosted, edge deployments |
| **Cloudflare R2** | `s3` (with R2 endpoint) | Zero egress-fee alternative to S3 |
| **DigitalOcean Spaces** | `s3` (with DO endpoint) | Simple S3-compatible option |
| **Google Cloud Storage** | `gcs` | GCP-native applications, Cloud Run, GKE |
| **Azure Blob Storage** | `azure` | Azure-native applications |
| **Local filesystem** | `local` | **Development only** --- never production |

All providers implement the same `Store` interface. Switching providers requires only a config change.

---

## Configuration

Storage is configured via YAML under the `storage` key.

> **Note on credential sources:** The `storage.CredentialSource` type in `pkg/storage` supports `value`, `env_var`, `file`, and `secret_manager`, but `secret_manager` currently accepts only `env:VAR_NAME` references. Cloud Secret Manager SDK lookups are not implemented; inject cloud-managed secrets into environment variables or mounted files before starting the process.

### S3 (AWS)

```yaml
storage:
  provider: s3
  default: private
  public_url_base: "https://cdn.example.com"
  public_paths:
    "/media": "storage/public/media/"
  s3:
    bucket: "myapp-private-bucket"
    public_bucket: "myapp-public-bucket"
    region: "us-east-1"
    access_key_id: "${AWS_ACCESS_KEY_ID}"
    secret_access_key: "${AWS_SECRET_ACCESS_KEY}"
    # For temporary STS credentials (optional):
    # session_token: "${AWS_SESSION_TOKEN}"
```

### S3 (MinIO)

```yaml
storage:
  provider: s3
  default: private
  s3:
    endpoint: "http://minio:9000"
    bucket: "myapp"
    region: "us-east-1"         # MinIO ignores region but the field is required
    use_path_style: true        # Required for MinIO
    access_key_id: "${MINIO_ACCESS_KEY}"
    secret_access_key: "${MINIO_SECRET_KEY}"
```

### S3 (Cloudflare R2)

```yaml
storage:
  provider: s3
  default: private
  s3:
    endpoint: "https://<account-id>.r2.cloudflarestorage.com"
    bucket: "myapp"
    region: "auto"
    use_path_style: false
    access_key_id: "${R2_ACCESS_KEY_ID}"
    secret_access_key: "${R2_SECRET_ACCESS_KEY}"
```

### GCS

```yaml
storage:
  provider: gcs
  default: private
  public_url_base: "https://cdn.example.com"
  public_paths:
    "/media": "storage/public/media/"
  gcs:
    bucket: "myapp-private"
    public_bucket: "myapp-public"
    # GCS uses Application Default Credentials (ADC) on GKE/Cloud Run
    # For local dev, set GOOGLE_APPLICATION_CREDENTIALS as an env var
    # pointing to your service account JSON file path.
    # No credential fields are needed in YAML.
```

### Azure Blob Storage

```yaml
storage:
  provider: azure
  default: private
  public_url_base: "https://cdn.example.com"
  public_paths:
    "/media": "storage/public/media/"
  azure:
    account_name: "${AZURE_ACCOUNT_NAME}"
    account_key: "${AZURE_STORAGE_KEY}"
    container: "myapp-private"
    public_container: "myapp-public"
```

### Local (Development)

```yaml
storage:
  provider: local
  default: private
  local:
    path: "storage/"   # Relative to working directory
```

### Temporary File Cleanup

```yaml
storage:
  # ... provider config above ...
  cleanup:
    enabled: true
    interval: "1h"     # How often to run cleanup
    prefix: "_tmp/"    # Key prefix for temporary objects
    max_age: "24h"     # Delete objects older than this
```

---

## Basic Usage

### Creating a Store

```go
import (
    "log/slog"

    "github.com/go-frame/nucleus/pkg/storage"
)

// From YAML config (parsed into storage.Config):
store, err := storage.New(cfg, logger)
if err != nil {
    log.Fatal(err)
}
defer store.Close()
```

### Put (Upload)

```go
import "github.com/go-frame/nucleus/pkg/storage"

// Upload a private PDF:
info, err := store.Put(ctx, "uploads/report-2025.pdf", fileReader, storage.PutOptions{
    Visibility:  storage.Private, // default, can omit
    ContentType: "application/pdf",
    Metadata: map[string]string{
        "user_id":    "12345",
        "department": "finance",
    },
})
if err != nil {
    // handle err
}
fmt.Printf("Stored: %s (%d bytes)\n", info.Key, info.Size)

// Upload a public image:
info, err = store.Put(ctx, "storage/public/media/blog/hero.png", imageReader, storage.PutOptions{
    Visibility:  storage.Public,
    ContentType: "image/png",
})
```

### Get (Download)

```go
reader, info, err := store.Get(ctx, "uploads/report-2025.pdf")
if err != nil {
    if _, ok := err.(storage.ErrNotFound); ok {
        // file does not exist
    }
}
defer reader.Close()

// Stream to HTTP response:
w.Header().Set("Content-Type", info.ContentType)
w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size))
io.Copy(w, reader)
```

### Delete

```go
err := store.Delete(ctx, "uploads/report-2025.pdf")
// Idempotent: no error if key does not exist
```

### Exists

```go
exists, err := store.Exists(ctx, "uploads/report-2025.pdf")
if err != nil {
    // handle err
}
if exists {
    // object is present
}
```

### List

```go
// List all objects under a prefix:
result, err := store.List(ctx, storage.ListOptions{
    Prefix: "uploads/",
    Limit:  100,
})
if err != nil {
    // handle err
}

for _, obj := range result.Objects {
    fmt.Printf("%s: %d bytes, updated %s\n", obj.Key, obj.Size, obj.UpdatedAt)
}

// Paginate:
if result.Truncated {
    nextResult, err := store.List(ctx, storage.ListOptions{
        Prefix: "uploads/",
        Limit:  100,
        Marker: result.NextMarker,
    })
    // ...
}

// Directory-like listing with common prefixes:
result, err = store.List(ctx, storage.ListOptions{
    Prefix:    "uploads/",
    Delimiter: "/",
})
// result.CommonPrefixes contains subdirectories like "uploads/reports/", "uploads/invoices/"
```

### Copy

```go
info, err := store.Copy(ctx, "uploads/report-2025.pdf", "archives/report-2025.pdf")
```

---

## Public vs Private Objects

Visibility is controlled via `storage.PutOptions.Visibility`:

| Visibility | Behavior | Access |
|------------|----------|--------|
| `storage.Private` (default) | Object stored in private bucket/container | Requires `SignedURL()` or app-layer serving |
| `storage.Public` | Object stored in public bucket/container (if configured) | Direct unauthenticated URL via `PublicURL()` |

### How it works

When `Visibility` is `Public` and a `public_bucket` (S3/GCS) or `public_container` (Azure) is configured, the object is uploaded to the separate public bucket. Otherwise it goes to the private bucket.

### CDN Integration

Set `public_url_base` in config to your CDN domain:

```yaml
storage:
  public_url_base: "https://cdn.example.com"
  public_paths:
    "/media": "storage/public/media/"
```

Then use the `PublicMapper` to generate URLs:

```go
mapper := storage.NewPublicMapperForConfig(store, cfg)

url, err := mapper.PublicURL(ctx, "storage/public/media/blog/hero.png", storage.URLConfig{})
// Returns: "https://cdn.example.com/media/blog/hero.png"
```

### Serving Public Files Directly

Register HTTP handlers to serve public files without signed URLs:

```go
mux := chi.NewMux()
mapper.MountAll(mux)
// GET /media/blog/hero.png serves storage/public/media/blog/hero.png from storage
// Sets Cache-Control: public, max-age=31536000, immutable
```

---

## Multi-Tenant Storage

The `TenantStore` wrapper provides automatic key prefixing for multi-tenant isolation.

### How it works

```
// App code (tenant-agnostic):
store.Put(ctx, "uploads/invoice.pdf", ...)

// Actual S3 key when tenant "acme" is in context:
"acme/uploads/invoice.pdf"

// Actual S3 key when tenant "globex" is in context:
"globex/uploads/invoice.pdf"
```

### Setup

```go
// Wrap the store with tenant prefixing:
tenantGetter := func(ctx context.Context) string {
    // Extract tenant from request context, JWT, session, etc.
    if t, ok := ctx.Value(myapp.TenantContextKey).(string); ok {
        return t
    }
    return "" // No prefix if no tenant
}

store = storage.NewWithTenant(store, tenantGetter)
```

### Using the Context Key

The package provides `storage.TenantKey` for consistent context usage:

```go
ctx = context.WithValue(ctx, storage.TenantKey{}, "acme")
info, err := store.Put(ctx, "uploads/invoice.pdf", reader, storage.PutOptions{})
// Key becomes: "acme/uploads/invoice.pdf"
```

### Overriding the Tenant Prefix

```go
info, err := store.Put(ctx, "shared/docs/readme.md", reader, storage.PutOptions{
    TenantPrefix: "global", // Explicitly set prefix instead of auto-detecting
})
// Key becomes: "global/shared/docs/readme.md"
```

All operations (`Get`, `Delete`, `Exists`, `List`, `PublicURL`, `SignedURL`, `Copy`) automatically apply the tenant prefix. `List()` scopes to the tenant automatically.

---

## Signed URLs

Signed URLs provide time-limited, direct access to private objects without authentication.

### Generating a Signed URL

```go
// Download URL (24 hours):
url, err := store.SignedURL(ctx, "uploads/report-2025.pdf", 24*time.Hour, storage.URLConfig{})

// Download URL with content type override:
url, err = store.SignedURL(ctx, "uploads/report-2025.pdf", 1*time.Hour, storage.URLConfig{
    ContentType: "application/octet-stream", // Force download instead of browser preview
    Disposition: "attachment",
})

// Short-lived URL (5 minutes for a one-time download):
url, err = store.SignedURL(ctx, "uploads/report-2025.pdf", 5*time.Minute, storage.URLConfig{})
```

### Use Cases

| Scenario | Recommended expiry |
|----------|-------------------|
| File download link in email | 1--24 hours |
| Image preview in app | 5--30 minutes |
| One-time secure transfer | 5 minutes |
| Batch export download | 1 hour |

### URLConfig Options

| Field | Purpose |
|-------|---------|
| `Expires` | Duration the URL remains valid (passed as second arg to `SignedURL`) |
| `ContentType` | Overrides `Content-Type` in the response (forces download vs preview) |
| `Disposition` | Sets `Content-Disposition`: `"inline"` (browser preview) or `"attachment"` (download) |

---

## Circuit Breaker

`App.New` automatically wraps all remote storage provider operations with a `pkg/circuit.Breaker` (ADR-004). This protects the application from cascading failures when object storage becomes unavailable or slow.

### What is wrapped

The following `Store` interface methods are wrapped:

| Method | Wrapped |
|--------|---------|
| `Put` | yes |
| `Get` | yes |
| `Delete` | yes |
| `Exists` | yes |
| `List` | yes |
| `Copy` | yes |
| `SignedURL` | yes |
| `PublicURL` | **no** — pure string composition, no network call |

### What is excluded

- **`local` provider**: the local filesystem store is never wrapped. Circuit breakers are only meaningful for remote dependencies.
- **`ErrNotFound`**: a missing object returned by `Get` or `Exists` is a normal outcome, not an infrastructure failure. The breaker does not count it as a failure towards its threshold.

### Defaults

The breaker is enabled by default with conservative thresholds:

```yaml
storage:
  circuit_breaker:
    enabled: true
    failure_threshold: 5         # consecutive op failures required to trip the breaker
    cooldown: 30s                # time the breaker stays open before probing
    half_open_max_concurrent: 1  # in-flight probe budget while half-open
```

### When the breaker is open

When the failure threshold is reached, the breaker transitions to the `open` state and all wrapped operations return `circuit.ErrOpen` immediately without making a network call. After the `cooldown` elapses, the breaker moves to `half-open` and admits up to `half_open_max_concurrent` probe calls. A successful probe closes the breaker; any failure resets the cooldown and re-opens it.

Application code should handle `circuit.ErrOpen` as a retriable or degraded-mode condition:

```go
import "github.com/jcsvwinston/nucleus/pkg/circuit"

_, _, err := store.Get(ctx, "uploads/report.pdf")
if errors.Is(err, circuit.ErrOpen) {
    // storage is temporarily unavailable; degrade gracefully or enqueue for retry
}
```

### Opting out or tuning

To disable the breaker entirely (for example, in a self-hosted environment where object storage is always local):

```yaml
storage:
  circuit_breaker:
    enabled: false
```

To reduce sensitivity for high-throughput workloads:

```yaml
storage:
  circuit_breaker:
    enabled: true
    failure_threshold: 10
    cooldown: 60s
    half_open_max_concurrent: 3
```

---

## Temporary File Cleanup

The storage layer supports automatic cleanup of temporary objects using the `_tmp/` prefix.

### Generating Temporary Keys

```go
// Create a temp key for an import job:
key := storage.CleanupTempKey("import_users")
// Returns: "_tmp/import_users_20250412143022"

// Upload to the temp location:
info, err := store.Put(ctx, key, csvReader, storage.PutOptions{})
```

### Checking if a Key is Temporary

```go
if storage.IsTempKey(key) {
    // This key will be automatically cleaned up
}
```

### Configuring the Cleaner

```yaml
storage:
  cleanup:
    enabled: true
    interval: "1h"      # Run every hour
    prefix: "_tmp/"     # Only clean keys with this prefix
    max_age: "24h"      # Delete keys older than 24 hours
```

### Starting the Cleaner

```go
cleaner, err := storage.NewCleaner(store, cfg.Cleanup, logger)
if err != nil {
    log.Fatal(err)
}
cleaner.Start()
defer cleaner.Stop()
```

The cleaner runs in a background goroutine, paginates through all objects matching the prefix, and deletes those whose `UpdatedAt` is older than `max_age`.

---

## Production Checklist

### Security

- [ ] **Never hardcode credentials** --- use environment variables or a secret manager.
- [ ] **Enable bucket versioning** (S3/GCS) for audit trails and accidental deletion recovery.
- [ ] **Enable default encryption** on all buckets/containers (SSE-S3, SSE-KMS, or customer-managed keys).
- [ ] **Block all public access** at the bucket level unless using a dedicated public bucket.
- [ ] **Use IAM roles** (EKS, GKE Workload Identity, Azure Managed Identity) instead of static credentials when possible.
- [ ] **Set `X-Content-Type-Options: nosniff`** on served responses (the `PublicMapper.Mount` does this automatically).

### Monitoring

- [ ] **Log storage operations** at startup and on errors (the factory logs `"storage provider initialized"`).
- [ ] **Monitor cleanup logs**: the cleaner logs `deleted` and `errors` counts each run.
- [ ] **Track storage growth**: use `List()` with pagination to periodically count objects per tenant.

### Lifecycle Policies

- [ ] **Configure cloud provider lifecycle rules** for long-term retention (e.g., transition to Glacier/Archive after 90 days).
- [ ] **Set expiration policies** on the `_tmp/` prefix as a safety net in addition to the built-in cleaner.
- [ ] **Enable object lock / retention policies** for compliance-sensitive data.

### Performance

- [ ] **Use a CDN** (`public_url_base`) for public content --- do not serve directly from the storage provider.
- [ ] **Enable HTTP/2** on the storage client (enabled by default in all providers).
- [ ] **Set appropriate `Cache-Control` headers**: the `PublicMapper.Mount` sets `public, max-age=31536000, immutable`.
- [ ] **Use multipart uploads** for files >100MB (handled automatically by the S3 SDK via minio-go).

---

## Migration from Legacy Config

If your application previously used `storage_driver` and `storage_path` configuration fields, migrate as follows:

### Legacy config (example)

```yaml
# OLD --- no longer supported
storage_driver: s3
storage_path: /uploads
s3_bucket: my-bucket
s3_region: us-east-1
```

### New config

```yaml
# NEW --- use the storage package config
storage:
  provider: s3
  default: private
  s3:
    bucket: "my-bucket"
    region: "us-east-1"
    access_key_id: "${AWS_ACCESS_KEY_ID}"
    secret_access_key: "${AWS_SECRET_ACCESS_KEY}"
```

### Migration steps

1. **Replace config fields**: Map old `storage_driver` values to new `provider` values:

   | Old `storage_driver` | New `provider` |
   |---------------------|----------------|
   | `s3` | `s3` |
   | `local` | `local` |
   | `gcs` | `gcs` |
   | `azure` | `azure` |

2. **Move credentials**: Replace inline credential strings with environment variable references (e.g. `"${AWS_ACCESS_KEY_ID}"`) and ensure your deployment injects those env vars.

3. **Update initialization code**: Replace manual provider construction with `storage.New()`:

   ```go
   // OLD:
   store, err := NewS3Store(bucket, region, accessKey, secretKey)

   // NEW:
   store, err := storage.New(cfg, logger)
   ```

4. **Update key paths**: If your old code prepended `storage_path` manually, remove it --- the new config handles prefixing via `public_paths` and tenant prefixing.

5. **Add public path mapping**: If you previously served public files via a hardcoded route, configure `public_paths` and `public_url_base`:

   ```yaml
   storage:
     public_paths:
       "/uploads": "storage/public/uploads/"
     public_url_base: "https://cdn.example.com"
   ```

6. **Test with local driver first**: Before deploying to production, validate with `provider: local` to ensure all code paths work without cloud dependencies.

### Breaking changes to note

- `PublicURL()` on cloud providers returns an empty string for private objects --- use `SignedURL()` instead.
- The local driver does **not** support `SignedURL()` --- it returns an error. Use cloud providers for production.
- `TenantStore` applies prefixes automatically --- if you previously added tenant prefixes manually in application code, remove those.
