package storage

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/circuit"
)

// CircuitBreakerConfig configures the optional circuit breaker that
// wraps remote storage operations. Zero values fall back to pkg/circuit
// defaults when Enabled is true; pkg/app applies its own framework
// defaults before constructing the breaker.
//
// The breaker wraps the network-touching operations of Store (Put, Get,
// Delete, Exists, List, Copy, SignedURL). PublicURL is pass-through
// because it is pure string composition. ErrNotFound is treated as a
// success for the breaker — a missing object is a normal outcome, not
// a dependency failure.
type CircuitBreakerConfig struct {
	// Enabled turns on circuit-breaker wrapping for the returned Store.
	Enabled bool `koanf:"enabled"`

	// FailureThreshold is the number of consecutive failures required
	// to trip the breaker open. Non-positive falls back to pkg/circuit's
	// default (1).
	FailureThreshold int `koanf:"failure_threshold"`

	// Cooldown is the duration the breaker stays open before admitting
	// half-open probes. Non-positive falls back to pkg/circuit's
	// default (30s).
	Cooldown time.Duration `koanf:"cooldown"`

	// HalfOpenMaxConcurrent caps in-flight probes in the half-open
	// state. Non-positive falls back to pkg/circuit's default (1).
	HalfOpenMaxConcurrent int `koanf:"half_open_max_concurrent"`
}

// wrapStoreWithBreaker decorates a Store with a circuit breaker. The
// returned Store is itself a Store so the wrapping is transparent to
// callers. PublicURL is pass-through (no network call); ErrNotFound
// from Get/Exists is not counted as a failure (a missing object is a
// normal outcome, not a dependency outage).
func wrapStoreWithBreaker(inner Store, cfg CircuitBreakerConfig) Store {
	br := circuit.New(circuit.Config{
		FailureThreshold:      cfg.FailureThreshold,
		Cooldown:              cfg.Cooldown,
		HalfOpenMaxConcurrent: cfg.HalfOpenMaxConcurrent,
	})
	return &breakerStore{inner: inner, breaker: br}
}

type breakerStore struct {
	inner   Store
	breaker *circuit.Breaker
}

// isExpectedNotFound returns true when err represents a missing object.
// A missing object is a legitimate outcome of Get/Exists and must not
// count as a dependency failure for the breaker.
func isExpectedNotFound(err error) bool {
	if err == nil {
		return false
	}
	var nf ErrNotFound
	return errors.As(err, &nf)
}

func (b *breakerStore) Put(ctx context.Context, key string, reader io.Reader, opts PutOptions) (ObjectInfo, error) {
	var info ObjectInfo
	err := b.breaker.Do(ctx, func(ctx context.Context) error {
		var innerErr error
		info, innerErr = b.inner.Put(ctx, key, reader, opts)
		return innerErr
	})
	return info, err
}

func (b *breakerStore) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	var (
		rc       io.ReadCloser
		info     ObjectInfo
		notFound bool
	)
	err := b.breaker.Do(ctx, func(ctx context.Context) error {
		var innerErr error
		rc, info, innerErr = b.inner.Get(ctx, key)
		if isExpectedNotFound(innerErr) {
			// Mask from breaker accounting — a missing object is a
			// normal outcome, not a dependency failure. Flag for the
			// outer return so the caller still sees ErrNotFound.
			notFound = true
			return nil
		}
		return innerErr
	})
	if err != nil {
		return rc, info, err
	}
	if notFound {
		// Drop any partial state the provider may have set and return
		// the canonical not-found shape.
		return nil, ObjectInfo{}, ErrNotFound(key)
	}
	return rc, info, nil
}

func (b *breakerStore) Delete(ctx context.Context, key string) error {
	return b.breaker.Do(ctx, func(ctx context.Context) error {
		return b.inner.Delete(ctx, key)
	})
}

func (b *breakerStore) Exists(ctx context.Context, key string) (bool, error) {
	var exists bool
	err := b.breaker.Do(ctx, func(ctx context.Context) error {
		var innerErr error
		exists, innerErr = b.inner.Exists(ctx, key)
		// Defensive: the Store contract says Exists returns
		// (false, nil) for a missing key, but if a future provider
		// adapter ever surfaces ErrNotFound instead, mask it from the
		// breaker rather than letting a normal outcome trip it.
		if isExpectedNotFound(innerErr) {
			return nil
		}
		return innerErr
	})
	return exists, err
}

func (b *breakerStore) List(ctx context.Context, opts ListOptions) (ListResult, error) {
	var result ListResult
	err := b.breaker.Do(ctx, func(ctx context.Context) error {
		var innerErr error
		result, innerErr = b.inner.List(ctx, opts)
		return innerErr
	})
	return result, err
}

func (b *breakerStore) PublicURL(ctx context.Context, key string, opts URLConfig) (string, error) {
	return b.inner.PublicURL(ctx, key, opts)
}

func (b *breakerStore) SignedURL(ctx context.Context, key string, expires time.Duration, opts URLConfig) (string, error) {
	var url string
	err := b.breaker.Do(ctx, func(ctx context.Context) error {
		var innerErr error
		url, innerErr = b.inner.SignedURL(ctx, key, expires, opts)
		return innerErr
	})
	return url, err
}

func (b *breakerStore) Copy(ctx context.Context, srcKey, dstKey string) (ObjectInfo, error) {
	var info ObjectInfo
	err := b.breaker.Do(ctx, func(ctx context.Context) error {
		var innerErr error
		info, innerErr = b.inner.Copy(ctx, srcKey, dstKey)
		return innerErr
	})
	return info, err
}

func (b *breakerStore) Close() error { return b.inner.Close() }
