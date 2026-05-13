package app

import (
	"context"
	"net/http"
	"sort"
	"time"

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
// 200 if every probed dependency is healthy, 503 otherwise. Currently
// probes every configured SQL database via DB.Health; Redis, storage and
// mail probes are tracked as follow-ups (see audit D3).
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

// healthzChecks runs every probe and returns the results in alias order so
// the response is deterministic for tests and operators eyeballing diffs.
func (a *App) healthzChecks(ctx context.Context) []HealthzCheck {
	out := make([]HealthzCheck, 0, len(a.DBs))

	aliases := make([]string, 0, len(a.DBs))
	for alias := range a.DBs {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)

	for _, alias := range aliases {
		out = append(out, probeDB(ctx, alias, a.DBs[alias]))
	}
	return out
}

func probeDB(parent context.Context, alias string, handle interface {
	Health(context.Context) error
}) HealthzCheck {
	if handle == nil {
		return HealthzCheck{Name: "db:" + alias, Status: "unhealthy", Message: "db handle is nil"}
	}
	ctx, cancel := context.WithTimeout(parent, healthzPingTimeout)
	defer cancel()

	start := time.Now()
	err := handle.Health(ctx)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return HealthzCheck{
			Name:      "db:" + alias,
			Status:    "unhealthy",
			Message:   err.Error(),
			LatencyMS: latency,
		}
	}
	return HealthzCheck{
		Name:      "db:" + alias,
		Status:    "healthy",
		LatencyMS: latency,
	}
}
