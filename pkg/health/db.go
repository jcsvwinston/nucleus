package health

import (
	"context"
	"errors"
)

// DBHealther is the minimum surface a SQL-database handle must expose to
// be probed by NewDBProbe. *pkg/db.DB satisfies this interface natively.
type DBHealther interface {
	Health(context.Context) error
}

// NewDBProbe wraps a database handle into a Prober. The probe surfaces
// the alias in the result name (e.g. "db:default"), so the handler can
// report per-alias status in multi-database deployments.
func NewDBProbe(name string, h DBHealther) Prober {
	return &dbProbe{name: name, handle: h}
}

type dbProbe struct {
	name   string
	handle DBHealther
}

func (p *dbProbe) Name() string { return p.name }

func (p *dbProbe) Probe(ctx context.Context) error {
	if p.handle == nil {
		return errors.New("db handle is nil")
	}
	return p.handle.Health(ctx)
}
