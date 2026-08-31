package storage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/circuit"
)

// wrapStoreWithBreaker decorates a Store with a circuit breaker. The
// returned Store is itself a Store so the wrapping is transparent to
// callers. PublicURL is pass-through (no network call); ErrNotFound
// from Get/Exists is not counted as a failure (a missing object is a
// normal outcome, not a dependency outage).
//
// State transitions are logged (NF-9): before this the breaker opened and
// closed in complete silence, so a storage outage looked like scattered
// "circuit breaker is open" errors with no line saying when or why the
// breaker moved.
func wrapStoreWithBreaker(inner Store, cfg CircuitBreakerConfig, logger *slog.Logger) Store {
	var br *circuit.Breaker
	br = circuit.New(circuit.Config{
		FailureThreshold:      cfg.FailureThreshold,
		Cooldown:              cfg.Cooldown,
		HalfOpenMaxConcurrent: cfg.HalfOpenMaxConcurrent,
		OnStateChange:         breakerTransitionLogger("storage", &br, logger),
	})
	return &breakerStore{inner: inner, breaker: br}
}

// breakerTransitionLogger returns an OnStateChange callback that logs
// every transition with the cumulative open count. WARN when the breaker
// opens (calls are now failing fast), INFO otherwise (probing, recovered).
// The breaker pointer is filled in after circuit.New returns; the callback
// only runs on calls through the breaker, so it never observes nil.
func breakerTransitionLogger(subsystem string, br **circuit.Breaker, logger *slog.Logger) func(from, to circuit.State) {
	if logger == nil {
		logger = slog.Default()
	}
	return func(from, to circuit.State) {
		var opens uint64
		if *br != nil {
			opens = (*br).Opens()
		}
		msg := subsystem + " circuit breaker state change"
		args := []any{"from", from.String(), "to", to.String(), "opens_total", opens}
		if to == circuit.StateOpen {
			logger.Warn(msg+" — calls now fail fast until the cooldown elapses", args...)
			return
		}
		logger.Info(msg, args...)
	}
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
