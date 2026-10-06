package health

import (
	"context"
	"errors"

	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// NewStorageProbe builds a Prober that exercises a storage.Store with a
// trivial, non-destructive List call. The probe asks for at most one
// object under a sentinel prefix so it never touches real tenant data
// nor pays per-request egress for content download.
//
// The default sentinel prefix is `_nucleus_healthz/`. It is unlikely to
// collide with real keys; if a deployment does happen to use that
// prefix, the probe still works — List returns whatever exists or an
// empty result and either is treated as healthy by the underlying
// provider call succeeding.
//
// A *storage.TenantStore is probed below its tenant scoping, on the store
// it wraps. Whether the backend answers is a question no tenant owns, and a
// probe request (an orchestrator, a load balancer) carries no tenant: through
// the wrapper, the probe would trip the tenant-less policy that exists for
// background jobs — the one-shot WARN about the shared key space, or, with
// multitenant.require_tenant_storage, storage.ErrNoTenantInContext and a 503
// from a healthy application (NU-111). Only the probe takes this path; every
// operation the application makes through the TenantStore keeps the policy.
func NewStorageProbe(name string, store storage.Store) Prober {
	return &storageProbe{name: name, store: belowTenantScoping(store)}
}

// belowTenantScoping returns the store a TenantStore wraps — through as many
// tenant layers as there are — and any other store unchanged. Other wrappers
// (the circuit breaker, a provider's own decorators) are kept: they are part
// of how the backend answers.
func belowTenantScoping(store storage.Store) storage.Store {
	for {
		ts, ok := store.(*storage.TenantStore)
		if !ok || ts == nil {
			return store
		}
		store = ts.Unwrap()
	}
}

type storageProbe struct {
	name  string
	store storage.Store
}

func (p *storageProbe) Name() string { return p.name }

func (p *storageProbe) Probe(ctx context.Context) error {
	if p.store == nil {
		return errors.New("storage handle is nil")
	}
	_, err := p.store.List(ctx, storage.ListOptions{
		Prefix: "_nucleus_healthz/",
		Limit:  1,
	})
	return err
}
