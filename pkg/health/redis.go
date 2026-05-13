package health

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

// NewRedisProbe builds a Prober that PINGs a Redis server on Probe().
//
// The URL is parsed eagerly; if it is empty or malformed, the returned
// Prober reports unhealthy on every Probe call instead of refusing to
// construct. This keeps the operator-facing failure mode uniform — the
// /healthz body always lists the probe and shows the reason — and
// avoids App.New having to decide between several silent failure paths
// at startup.
//
// Each probe call lazily creates a short-lived client and closes it
// after PING. That matches the cadence of liveness probes (every 10–30 s
// in Kubernetes) and avoids holding a long-lived connection pool just
// for health checks.
func NewRedisProbe(name, redisURL string) Prober {
	url := strings.TrimSpace(redisURL)
	if url == "" {
		return &redisProbe{name: name, configErr: errors.New("redis url is empty")}
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		return &redisProbe{name: name, configErr: fmt.Errorf("parse redis url: %w", err)}
	}
	return &redisProbe{name: name, opts: opts}
}

type redisProbe struct {
	name      string
	opts      *redis.Options
	configErr error
}

func (p *redisProbe) Name() string { return p.name }

func (p *redisProbe) Probe(ctx context.Context) error {
	if p.configErr != nil {
		return p.configErr
	}
	client := redis.NewClient(p.opts)
	defer client.Close()
	return client.Ping(ctx).Err()
}
