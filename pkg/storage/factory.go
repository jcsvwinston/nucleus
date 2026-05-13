package storage

import (
	"context"
	"fmt"
	"log/slog"
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

	var store Store
	var err error

	switch cfg.Provider {
	case ProviderS3:
		store, err = NewS3Store(cfg.S3)
	case ProviderLocal:
		store, err = NewLocalStore(cfg.Local)
	case ProviderGCS:
		store, err = NewGCSStore(cfg.GCS)
	case ProviderAzure:
		store, err = NewAzureStore(cfg.Azure)
	default:
		err = fmt.Errorf("storage: unsupported provider %q", cfg.Provider)
	}

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
		store = wrapStoreWithBreaker(store, cfg.CircuitBreaker)
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
