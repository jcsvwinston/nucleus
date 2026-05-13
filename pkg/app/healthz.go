package app

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/health"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// HealthzCheck is one entry in the /healthz response, reporting on a single
// dependency. `status` is `healthy` or `unhealthy`; degraded states are
// promoted to `unhealthy` so external probes (Kubernetes, ELB) treat the
// response uniformly.
type HealthzCheck struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

// HealthzResponse is the JSON body returned by /healthz.
type HealthzResponse struct {
	Status    string         `json:"status"`
	CheckedAt string         `json:"checked_at"`
	Checks    []HealthzCheck `json:"checks"`
}

// healthzPingTimeout is the per-dependency timeout. Kept short so a slow
// dependency cannot stall a probe (Kubernetes' default failureThreshold is
// 3 × periodSeconds, typically 30s end-to-end).
const healthzPingTimeout = 2 * time.Second

// handleHealthz responds with the aggregated liveness status of the app.
// 200 if every probed dependency is healthy, 503 otherwise. Probes are
// derived from current app state on every request (see buildHealthProbes)
// so subsystems attached after the handler is registered still surface.
func (a *App) handleHealthz(c *router.Context) error {
	checks := a.healthzChecks(c.Request.Context())

	overall := "healthy"
	for _, ch := range checks {
		if ch.Status != "healthy" {
			overall = "unhealthy"
			break
		}
	}

	status := http.StatusOK
	if overall != "healthy" {
		status = http.StatusServiceUnavailable
	}

	return c.JSON(status, HealthzResponse{
		Status:    overall,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Checks:    checks,
	})
}

// healthzChecks runs every applicable probe against current app state and
// returns the results in alias order so the response is deterministic for
// tests and operators eyeballing diffs.
func (a *App) healthzChecks(ctx context.Context) []HealthzCheck {
	probes := a.buildHealthProbes()
	results := health.Run(ctx, probes, healthzPingTimeout)

	out := make([]HealthzCheck, 0, len(results))
	for _, r := range results {
		status := "healthy"
		if !r.Healthy {
			status = "unhealthy"
		}
		out = append(out, HealthzCheck{
			Name:      r.Name,
			Status:    status,
			Message:   r.Message,
			LatencyMS: r.LatencyMS,
		})
	}
	return out
}

// buildHealthProbes assembles the probe set from current app state, in a
// stable order: every configured database (sorted by alias), then Redis
// (if a URL is configured), then object storage (if a Store is attached).
// Mail is not probed today — see pkg/health for the rationale.
func (a *App) buildHealthProbes() []health.Prober {
	probes := make([]health.Prober, 0, len(a.DBs)+2)

	aliases := make([]string, 0, len(a.DBs))
	for alias := range a.DBs {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		probes = append(probes, health.NewDBProbe("db:"+alias, a.DBs[alias]))
	}

	if a.Config != nil {
		if url := strings.TrimSpace(a.Config.RedisURL); url != "" {
			probes = append(probes, health.NewRedisProbe("redis", url))
		}
	}

	if a.Storage != nil {
		probes = append(probes, health.NewStorageProbe("storage", a.Storage))
	}

	return probes
}
