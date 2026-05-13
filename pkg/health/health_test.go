package health

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/storage"
)

type fakeProbe struct {
	name string
	err  error
}

func (f *fakeProbe) Name() string                       { return f.name }
func (f *fakeProbe) Probe(_ context.Context) error      { return f.err }

func TestRun_AllHealthy(t *testing.T) {
	results := Run(context.Background(), []Prober{
		&fakeProbe{name: "a"},
		&fakeProbe{name: "b"},
	}, time.Second)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Healthy {
			t.Fatalf("%s: expected healthy, got %+v", r.Name, r)
		}
	}
}

func TestRun_FailingProbeReportsMessage(t *testing.T) {
	results := Run(context.Background(), []Prober{
		&fakeProbe{name: "ok"},
		&fakeProbe{name: "down", err: errors.New("boom")},
	}, time.Second)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Healthy {
		t.Fatalf("first probe should be healthy: %+v", results[0])
	}
	if results[1].Healthy {
		t.Fatalf("second probe should be unhealthy")
	}
	if results[1].Message != "boom" {
		t.Fatalf("expected message 'boom', got %q", results[1].Message)
	}
}

func TestRun_NoProbesReturnsNil(t *testing.T) {
	if r := Run(context.Background(), nil, time.Second); r != nil {
		t.Fatalf("expected nil for no probes, got %+v", r)
	}
}

// ---------- DB probe ----------

type stubDB struct{ err error }

func (s *stubDB) Health(_ context.Context) error { return s.err }

func TestDBProbe_HealthyAndUnhealthy(t *testing.T) {
	p := NewDBProbe("db:default", &stubDB{})
	if err := p.Probe(context.Background()); err != nil {
		t.Fatalf("expected healthy, got %v", err)
	}
	if p.Name() != "db:default" {
		t.Fatalf("unexpected name: %s", p.Name())
	}

	bad := NewDBProbe("db:shard", &stubDB{err: errors.New("conn refused")})
	if err := bad.Probe(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDBProbe_NilHandle(t *testing.T) {
	p := NewDBProbe("db:default", nil)
	if err := p.Probe(context.Background()); err == nil {
		t.Fatal("expected error for nil handle")
	}
}

// ---------- Redis probe ----------

func TestRedisProbe_EmptyURLFailsConsistently(t *testing.T) {
	p := NewRedisProbe("redis", "")
	if p.Name() != "redis" {
		t.Fatalf("unexpected name: %s", p.Name())
	}
	err := p.Probe(context.Background())
	if err == nil {
		t.Fatal("empty URL must surface as Probe error, not silent skip")
	}
}

func TestRedisProbe_MalformedURLFailsConsistently(t *testing.T) {
	p := NewRedisProbe("redis", "not-a-redis-url")
	err := p.Probe(context.Background())
	if err == nil {
		t.Fatal("malformed URL must surface as Probe error")
	}
}

// ---------- Storage probe ----------

type stubStore struct {
	storage.Store
	listErr      error
	gotPrefix    string
	gotLimit     int
}

func (s *stubStore) List(_ context.Context, opts storage.ListOptions) (storage.ListResult, error) {
	s.gotPrefix = opts.Prefix
	s.gotLimit = opts.Limit
	if s.listErr != nil {
		return storage.ListResult{}, s.listErr
	}
	return storage.ListResult{}, nil
}

// All other storage.Store methods are unused by the probe but satisfy the
// interface via the embedded storage.Store nil. They must not be called.
func (s *stubStore) Put(context.Context, string, io.Reader, storage.PutOptions) (storage.ObjectInfo, error) {
	panic("Put should not be called by the storage probe")
}

func TestStorageProbe_UsesSentinelPrefixAndSmallLimit(t *testing.T) {
	stub := &stubStore{}
	p := NewStorageProbe("storage", stub)

	if err := p.Probe(context.Background()); err != nil {
		t.Fatalf("expected healthy, got %v", err)
	}
	if stub.gotPrefix != "_nucleus_healthz/" {
		t.Fatalf("expected sentinel prefix, got %q", stub.gotPrefix)
	}
	if stub.gotLimit != 1 {
		t.Fatalf("expected limit 1, got %d", stub.gotLimit)
	}
}

func TestStorageProbe_PropagatesListError(t *testing.T) {
	stub := &stubStore{listErr: errors.New("403 forbidden")}
	p := NewStorageProbe("storage", stub)

	if err := p.Probe(context.Background()); err == nil {
		t.Fatal("expected list error to bubble up")
	}
}

func TestStorageProbe_NilStore(t *testing.T) {
	p := NewStorageProbe("storage", nil)
	if err := p.Probe(context.Background()); err == nil {
		t.Fatal("expected error for nil store")
	}
}
