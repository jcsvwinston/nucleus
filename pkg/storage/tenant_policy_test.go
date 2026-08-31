package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func tenantFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(TenantKey{}).(string); ok {
		return v
	}
	return ""
}

// keyRecordingStore remembers the exact keys each operation received, so
// the tests can assert on real prefixes (stubStore's Exists always answers
// true regardless of key).
type keyRecordingStore struct {
	*stubStore
	keys map[string]bool
}

func newKeyRecordingStore() *keyRecordingStore {
	return &keyRecordingStore{stubStore: newStubStore(nil), keys: map[string]bool{}}
}

func (s *keyRecordingStore) Put(ctx context.Context, key string, r io.Reader, opts PutOptions) (ObjectInfo, error) {
	s.keys[key] = true
	return s.stubStore.Put(ctx, key, r, opts)
}

// NF-12: a TenantStore whose context resolves to no tenant used to degrade
// to the shared (unprefixed) key space in complete silence — a background
// job without request scope wrote tenant data into shared keys and nothing
// said so. Strict mode rejects the operation.
func TestTenantStoreStrictRejectsTenantlessOperations(t *testing.T) {
	inner := newKeyRecordingStore()
	ts := NewTenantStoreWithOptions(inner, tenantFromCtx, TenantStoreOptions{Strict: true})

	ctx := context.Background() // no tenant
	if _, err := ts.Put(ctx, "uploads/a.txt", strings.NewReader("x"), PutOptions{}); !errors.Is(err, ErrNoTenantInContext) {
		t.Fatalf("Put without tenant err = %v; want ErrNoTenantInContext", err)
	}
	if _, _, err := ts.Get(ctx, "uploads/a.txt"); !errors.Is(err, ErrNoTenantInContext) {
		t.Fatalf("Get without tenant err = %v; want ErrNoTenantInContext", err)
	}
	if err := ts.Delete(ctx, "uploads/a.txt"); !errors.Is(err, ErrNoTenantInContext) {
		t.Fatalf("Delete without tenant err = %v; want ErrNoTenantInContext", err)
	}
	if _, err := ts.List(ctx, ListOptions{}); !errors.Is(err, ErrNoTenantInContext) {
		t.Fatalf("List without tenant err = %v; want ErrNoTenantInContext", err)
	}

	// With a tenant in context the same store works and prefixes.
	tctx := context.WithValue(ctx, TenantKey{}, "acme")
	if _, err := ts.Put(tctx, "uploads/a.txt", strings.NewReader("x"), PutOptions{}); err != nil {
		t.Fatalf("Put with tenant: %v", err)
	}
	if !inner.keys["acme/uploads/a.txt"] {
		t.Fatalf("inner key not tenant-prefixed; recorded keys: %v", inner.keys)
	}
}

// Non-strict mode keeps the historical behaviour but warns ONCE.
func TestTenantStoreWarnsOnceOnTenantlessDegradation(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	inner := newStubStore(nil)
	ts := NewTenantStoreWithOptions(inner, tenantFromCtx, TenantStoreOptions{Logger: logger})

	ctx := context.Background()
	if _, err := ts.Put(ctx, "a.txt", strings.NewReader("x"), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := ts.Put(ctx, "b.txt", strings.NewReader("x"), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	logged := buf.String()
	if !strings.Contains(logged, "unprefixed") {
		t.Fatalf("degradation did not warn:\n%s", logged)
	}
	if n := strings.Count(logged, "unprefixed"); n != 1 {
		t.Fatalf("degradation warned %d times; want exactly 1:\n%s", n, logged)
	}
}

// A nil getter means prefixing is deliberately off: neither strict nor the
// warn applies.
func TestTenantStoreNilGetterIsExemptFromPolicy(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	inner := newStubStore(nil)
	ts := NewTenantStoreWithOptions(inner, nil, TenantStoreOptions{Strict: true, Logger: logger})

	if _, err := ts.Put(context.Background(), "a.txt", strings.NewReader("x"), PutOptions{}); err != nil {
		t.Fatalf("Put with nil getter: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("nil getter logged: %s", buf.String())
	}
}
