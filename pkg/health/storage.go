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
func NewStorageProbe(name string, store storage.Store) Prober {
	return &storageProbe{name: name, store: store}
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
