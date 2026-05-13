package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/circuit"
)

// stubStore is a test Store that returns a configurable error from
// every remote operation. PublicURL is implemented as pure string
// composition (no error path).
type stubStore struct {
	err   error
	calls map[string]int
}

func newStubStore(err error) *stubStore {
	return &stubStore{err: err, calls: map[string]int{}}
}

func (s *stubStore) record(op string) { s.calls[op]++ }

func (s *stubStore) Put(_ context.Context, key string, _ io.Reader, _ PutOptions) (ObjectInfo, error) {
	s.record("Put")
	if s.err != nil {
		return ObjectInfo{}, s.err
	}
	return ObjectInfo{Key: key, Size: 0}, nil
}

func (s *stubStore) Get(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	s.record("Get")
	if s.err != nil {
		return nil, ObjectInfo{}, s.err
	}
	return io.NopCloser(strings.NewReader("hi")), ObjectInfo{Key: key}, nil
}

func (s *stubStore) Delete(_ context.Context, _ string) error {
	s.record("Delete")
	return s.err
}

func (s *stubStore) Exists(_ context.Context, _ string) (bool, error) {
	s.record("Exists")
	if s.err != nil {
		return false, s.err
	}
	return true, nil
}

func (s *stubStore) List(_ context.Context, _ ListOptions) (ListResult, error) {
	s.record("List")
	if s.err != nil {
		return ListResult{}, s.err
	}
	return ListResult{}, nil
}

func (s *stubStore) PublicURL(_ context.Context, key string, _ URLConfig) (string, error) {
	s.record("PublicURL")
	return "https://cdn.example.com/" + key, nil
}

func (s *stubStore) SignedURL(_ context.Context, key string, _ time.Duration, _ URLConfig) (string, error) {
	s.record("SignedURL")
	if s.err != nil {
		return "", s.err
	}
	return "https://signed.example.com/" + key, nil
}

func (s *stubStore) Copy(_ context.Context, _, dstKey string) (ObjectInfo, error) {
	s.record("Copy")
	if s.err != nil {
		return ObjectInfo{}, s.err
	}
	return ObjectInfo{Key: dstKey}, nil
}

func (s *stubStore) Close() error { return nil }

func TestBreakerStore_PassesThroughOnSuccess(t *testing.T) {
	inner := newStubStore(nil)
	w := wrapStoreWithBreaker(inner, CircuitBreakerConfig{FailureThreshold: 2})

	if _, err := w.Put(context.Background(), "k", bytes.NewReader(nil), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, _, err := w.Get(context.Background(), "k"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := w.Delete(context.Background(), "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, err := w.Exists(context.Background(), "k"); !ok || err != nil {
		t.Fatalf("Exists: %v %v", ok, err)
	}
}

func TestBreakerStore_TripsAfterFailureThreshold(t *testing.T) {
	innerErr := errors.New("s3 boom")
	inner := newStubStore(innerErr)
	w := wrapStoreWithBreaker(inner, CircuitBreakerConfig{
		FailureThreshold: 2,
		Cooldown:         time.Hour,
	})

	if _, err := w.Put(context.Background(), "k", bytes.NewReader(nil), PutOptions{}); !errors.Is(err, innerErr) {
		t.Fatalf("first Put: want innerErr, got %v", err)
	}
	if err := w.Delete(context.Background(), "k"); !errors.Is(err, innerErr) {
		t.Fatalf("Delete: want innerErr, got %v", err)
	}

	// Breaker tripped — subsequent calls short-circuit with ErrOpen.
	if _, err := w.Put(context.Background(), "k", bytes.NewReader(nil), PutOptions{}); !errors.Is(err, circuit.ErrOpen) {
		t.Fatalf("third Put: want ErrOpen, got %v", err)
	}
	if inner.calls["Put"] != 1 {
		t.Fatalf("expected Put to be called once before short-circuit, got %d", inner.calls["Put"])
	}
}

func TestBreakerStore_NotFoundDoesNotTripBreaker(t *testing.T) {
	// ErrNotFound is a normal Get/Exists outcome — it must not
	// accumulate failures in the breaker.
	inner := newStubStore(ErrNotFound("missing"))
	w := wrapStoreWithBreaker(inner, CircuitBreakerConfig{
		FailureThreshold: 2,
		Cooldown:         time.Hour,
	})

	for i := 0; i < 10; i++ {
		_, _, err := w.Get(context.Background(), "missing")
		var nf ErrNotFound
		if !errors.As(err, &nf) {
			t.Fatalf("Get call %d: want ErrNotFound, got %v", i, err)
		}
	}

	// Exists with ErrNotFound is the defensive-guard path — must also
	// not accumulate breaker failures even though the contract says
	// providers should return (false, nil) instead.
	for i := 0; i < 10; i++ {
		_, err := w.Exists(context.Background(), "missing")
		var nf ErrNotFound
		if !errors.As(err, &nf) {
			// The guard surfaces it as nil err in production paths
			// (matches the contract). Either nil or ErrNotFound is
			// acceptable here — what matters is that no failure
			// accumulates in the breaker. The hard-failure check
			// below proves the breaker is still fully fresh.
			if err != nil {
				t.Fatalf("Exists call %d: want nil or ErrNotFound, got %v", i, err)
			}
		}
	}

	// Switch underlying error to a hard failure — Put should still
	// succeed at tripping the breaker (proves the previous Gets/Exists
	// did not accumulate).
	inner.err = errors.New("real failure")
	if err := w.Delete(context.Background(), "k"); err == nil {
		t.Fatalf("Delete: expected error after switching inner err, got nil")
	}
	if err := w.Delete(context.Background(), "k"); err == nil {
		t.Fatalf("Delete: expected second error to trip breaker")
	}
	if err := w.Delete(context.Background(), "k"); !errors.Is(err, circuit.ErrOpen) {
		t.Fatalf("third Delete: want ErrOpen, got %v", err)
	}
}

func TestBreakerStore_PublicURLBypassesBreaker(t *testing.T) {
	innerErr := errors.New("everything broken")
	inner := newStubStore(innerErr)
	w := wrapStoreWithBreaker(inner, CircuitBreakerConfig{
		FailureThreshold: 1,
		Cooldown:         time.Hour,
	})

	// Trip the breaker.
	if err := w.Delete(context.Background(), "k"); !errors.Is(err, innerErr) {
		t.Fatalf("Delete: want innerErr, got %v", err)
	}
	if err := w.Delete(context.Background(), "k"); !errors.Is(err, circuit.ErrOpen) {
		t.Fatalf("Delete after trip: want ErrOpen, got %v", err)
	}

	// PublicURL is pass-through string composition — it must work
	// regardless of breaker state.
	url, err := w.PublicURL(context.Background(), "k", URLConfig{})
	if err != nil {
		t.Fatalf("PublicURL: %v", err)
	}
	if url == "" {
		t.Fatalf("PublicURL: empty url")
	}
}

func TestNew_BreakerSkippedForLocalProvider(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{
		Provider: ProviderLocal,
		Local:    LocalConfig{Path: tmp},
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 1,
		},
	}
	store, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	// Local store should never be wrapped — confirm by type assertion.
	if _, isBreaker := store.(*breakerStore); isBreaker {
		t.Fatalf("local provider must not be wrapped with breaker")
	}
}
