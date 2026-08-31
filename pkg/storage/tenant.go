package storage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// TenantKey is the context key for tenant ID.
type TenantKey struct{}

// ErrNoTenantInContext is returned by a strict TenantStore when an
// operation runs with no tenant resolvable from the context — the case
// that would otherwise read or write the SHARED (unprefixed) key space.
// See TenantStoreOptions.Strict.
var ErrNoTenantInContext = errors.New("storage: no tenant in context — this operation would use the shared (unprefixed) key space; carry the request scope into the context, or disable multitenant.require_tenant_storage if shared keys are intended")

// TenantStore wraps a Store and automatically prefixes all keys with the
// current tenant ID (extracted from context). This provides automatic
// tenant isolation at the storage level without requiring application code changes.
//
// Key transformation example:
//
//	// App code:
//	store.Put(ctx, "uploads/invoice.pdf", ...)
//
//	// Actual S3 key (if tenant "acme" is in context):
//	"acme/uploads/invoice.pdf"
//
// If no tenant is in context, keys are stored without prefix — by default
// SILENTLY, which is exactly how a background job without request scope
// ends up writing tenant data into the shared key space (NF-12). Construct
// with NewTenantStoreWithOptions to get a WARN on the first unprefixed
// operation, or Strict mode that rejects them with ErrNoTenantInContext.
type TenantStore struct {
	store  Store
	getter func(context.Context) string

	strict   bool
	logger   *slog.Logger
	warnOnce sync.Once
}

// NewTenantStore creates a tenant-prefixing wrapper.
// The getter function extracts the tenant ID from context.
// Pass nil for getter to disable tenant prefixing.
func NewTenantStore(store Store, getter func(context.Context) string) *TenantStore {
	return NewTenantStoreWithOptions(store, getter, TenantStoreOptions{})
}

// TenantStoreOptions configures how a TenantStore treats an operation
// whose context resolves to NO tenant (NF-12). The zero value preserves
// the historical behaviour: unprefixed keys, in silence.
type TenantStoreOptions struct {
	// Strict rejects tenant-less operations with ErrNoTenantInContext
	// instead of degrading to the shared key space. Wire it from
	// multitenant.require_tenant_storage.
	Strict bool

	// Logger, when non-nil and Strict is off, receives ONE warning the
	// first time an operation degrades to the shared key space, so the
	// degradation is at least visible without flooding the log on every
	// background-job call.
	Logger *slog.Logger
}

// NewTenantStoreWithOptions is NewTenantStore with an explicit policy for
// tenant-less operations. Both Strict and Logger are inert when getter is
// nil — a nil getter means prefixing is deliberately disabled.
func NewTenantStoreWithOptions(store Store, getter func(context.Context) string, opts TenantStoreOptions) *TenantStore {
	return &TenantStore{
		store:  store,
		getter: getter,
		strict: opts.Strict,
		logger: opts.Logger,
	}
}

func (t *TenantStore) resolveTenant(ctx context.Context) string {
	if t.getter == nil {
		return ""
	}
	return t.getter(ctx)
}

// checkTenant applies the tenant-less policy: nil error when the operation
// may proceed (with or without prefix), ErrNoTenantInContext in strict
// mode. The one-shot WARN fires here.
func (t *TenantStore) checkTenant(tenant string) error {
	if tenant != "" || t.getter == nil {
		return nil
	}
	if t.strict {
		return ErrNoTenantInContext
	}
	if t.logger != nil {
		t.warnOnce.Do(func() {
			t.logger.Warn("storage: operation with no tenant in context degraded to the SHARED (unprefixed) key space — background jobs without request scope do this silently; set multitenant.require_tenant_storage: true to reject instead (warned once, further occurrences are not logged)")
		})
	}
	return nil
}

func (t *TenantStore) prefixKey(ctx context.Context, key string) (string, error) {
	tenant := t.resolveTenant(ctx)
	if err := t.checkTenant(tenant); err != nil {
		return "", err
	}
	if tenant == "" {
		return key, nil
	}
	// Ensure tenant prefix doesn't double-slash
	tenant = strings.TrimRight(tenant, "/")
	key = strings.TrimLeft(key, "/")
	return tenant + "/" + key, nil
}

func (t *TenantStore) Put(ctx context.Context, key string, reader io.Reader, opts PutOptions) (ObjectInfo, error) {
	if opts.TenantPrefix == "" {
		var err error
		key, err = t.prefixKey(ctx, key)
		if err != nil {
			return ObjectInfo{}, err
		}
	} else {
		// Explicit override: prepend the custom prefix
		key = strings.TrimRight(opts.TenantPrefix, "/") + "/" + strings.TrimLeft(key, "/")
	}
	return t.store.Put(ctx, key, reader, opts)
}

func (t *TenantStore) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	key, err := t.prefixKey(ctx, key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	return t.store.Get(ctx, key)
}

func (t *TenantStore) Delete(ctx context.Context, key string) error {
	key, err := t.prefixKey(ctx, key)
	if err != nil {
		return err
	}
	return t.store.Delete(ctx, key)
}

func (t *TenantStore) Exists(ctx context.Context, key string) (bool, error) {
	key, err := t.prefixKey(ctx, key)
	if err != nil {
		return false, err
	}
	return t.store.Exists(ctx, key)
}

func (t *TenantStore) List(ctx context.Context, opts ListOptions) (ListResult, error) {
	tenant := t.resolveTenant(ctx)
	if err := t.checkTenant(tenant); err != nil {
		return ListResult{}, err
	}
	if tenant != "" {
		tenant = strings.TrimRight(tenant, "/")
		if opts.Prefix == "" {
			opts.Prefix = tenant + "/"
		} else {
			opts.Prefix = tenant + "/" + strings.TrimLeft(opts.Prefix, "/")
		}
	}
	return t.store.List(ctx, opts)
}

func (t *TenantStore) PublicURL(ctx context.Context, key string, opts URLConfig) (string, error) {
	key, err := t.prefixKey(ctx, key)
	if err != nil {
		return "", err
	}
	return t.store.PublicURL(ctx, key, opts)
}

func (t *TenantStore) SignedURL(ctx context.Context, key string, expires time.Duration, opts URLConfig) (string, error) {
	key, err := t.prefixKey(ctx, key)
	if err != nil {
		return "", err
	}
	return t.store.SignedURL(ctx, key, expires, opts)
}

func (t *TenantStore) Copy(ctx context.Context, srcKey, dstKey string) (ObjectInfo, error) {
	srcKey, err := t.prefixKey(ctx, srcKey)
	if err != nil {
		return ObjectInfo{}, err
	}
	dstKey, err = t.prefixKey(ctx, dstKey)
	if err != nil {
		return ObjectInfo{}, err
	}
	return t.store.Copy(ctx, srcKey, dstKey)
}

func (t *TenantStore) Close() error {
	return t.store.Close()
}

// Unwrap returns the underlying store (for type assertions to provider-specific features).
func (t *TenantStore) Unwrap() Store {
	return t.store
}

// UnwrapIfCleaner returns the underlying store for cleanup configuration.
func (t *TenantStore) UnwrapIfCleaner() Store {
	return t.store
}
