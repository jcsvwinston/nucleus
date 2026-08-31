package storage

import (
	"github.com/jcsvwinston/nucleus/pkg/storage/provider"

	"context"
	"fmt"
	"log/slog"
	"strings"
)

// New creates a Store from configuration.
// This is the primary entry point for the storage package.
//
// Example:
//
//	store, err := storage.New(cfg, logger)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer store.Close()
func New(cfg Config, logger *slog.Logger) (Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("storage: invalid config: %w", err)
	}

	// Resolved through the registry rather than a switch, so a backend this
	// framework has never heard of — Ceph, Swift, an internal object store —
	// is selectable by name without patching the framework. Built-ins are
	// registered in builtins.go through the same public call.
	factory, ok := provider.Lookup(string(cfg.Provider))
	if !ok {
		// The error is the only place a plugin author finds out the
		// registry exists, so it names what IS available.
		return nil, fmt.Errorf("storage: unsupported provider %q (registered: %s) — register a third-party backend with storage.RegisterProvider",
			cfg.Provider, strings.Join(RegisteredProviders(), ", "))
	}
	store, err := factory(cfg)
	if err != nil {
		return nil, err
	}

	if logger != nil {
		logger.Info("storage provider initialized", "provider", string(cfg.Provider))
	}

	// Circuit breaker wrapping. Skipped for local provider (filesystem
	// failures are not the kind of outage breakers are designed to
	// isolate). Skipped when CircuitBreaker.Enabled is false.
	if cfg.CircuitBreaker.Enabled && cfg.Provider != ProviderLocal {
		store = wrapStoreWithBreaker(store, cfg.CircuitBreaker, logger)
		if logger != nil {
			logger.Info(
				"storage circuit breaker enabled",
				"provider", string(cfg.Provider),
				"failure_threshold", cfg.CircuitBreaker.FailureThreshold,
				"cooldown", cfg.CircuitBreaker.Cooldown,
			)
		}
	}

	return store, nil
}

// NewWithTenant wraps a store with tenant prefixing.
// The tenantGetter extracts tenant ID from context.
// Pass nil for tenantGetter to disable tenant prefixing.
func NewWithTenant(store Store, tenantGetter func(context.Context) string) *TenantStore {
	return NewTenantStore(store, tenantGetter)
}

// NewPublicMapper creates a public URL mapper for the given store.
func NewPublicMapperForConfig(store Store, cfg Config) *PublicMapper {
	return NewPublicMapper(store, cfg.PublicPaths, cfg.PublicURLBase)
}
