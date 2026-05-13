// Package health provides a small abstraction for dependency probes used
// by the /healthz handler in pkg/app. Each subsystem (DB, Redis, object
// storage, etc.) exposes a Prober that the handler aggregates into the
// JSON response.
//
// The package exists so pkg/app stays free of direct dependencies on
// driver-specific libraries (e.g. github.com/redis/go-redis/v9), which
// the project firewall rules prefer to keep wrapped.
package health

import (
	"context"
	"sync"
	"time"
)

// Prober describes a single dependency that can be probed for health.
// Probe should return nil when the dependency is reachable and usable,
// or a non-nil error explaining the failure. Implementations must
// respect the deadline carried by ctx.
type Prober interface {
	// Name is the identifier surfaced in the /healthz response, e.g.
	// "db:default" or "redis". Stable across releases — operators key
	// alerts off these strings.
	Name() string

	// Probe runs the dependency check. Implementations should keep the
	// operation cheap and non-destructive (PING, HEAD, LIST with limit 1).
	Probe(ctx context.Context) error
}

// Result is the outcome of one probe run.
type Result struct {
	Name      string
	Healthy   bool
	Message   string
	LatencyMS int64
}

// Run executes every probe in probes with the given per-probe timeout
// budget and returns the results in the same order. Probes run
// concurrently; total wall time is bounded by the slowest probe rather
// than the sum.
//
// A zero or negative timeout disables the per-probe deadline; callers
// remain responsible for parent-context cancellation.
func Run(parent context.Context, probes []Prober, timeout time.Duration) []Result {
	if len(probes) == 0 {
		return nil
	}

	results := make([]Result, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Add(1)
		go func(idx int, prober Prober) {
			defer wg.Done()
			results[idx] = runOne(parent, prober, timeout)
		}(i, p)
	}
	wg.Wait()
	return results
}

func runOne(parent context.Context, p Prober, timeout time.Duration) Result {
	name := p.Name()

	ctx := parent
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	}
	defer cancel()

	start := time.Now()
	err := p.Probe(ctx)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return Result{Name: name, Healthy: false, Message: err.Error(), LatencyMS: latency}
	}
	return Result{Name: name, Healthy: true, LatencyMS: latency}
}
