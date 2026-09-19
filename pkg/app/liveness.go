// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

// LivenessResponse is the body of /livez.
type LivenessResponse struct {
	Status string `json:"status"`
	// Uptime is how long this process has been serving, which is the one
	// thing a liveness answer can honestly report about itself.
	UptimeSeconds int64 `json:"uptime_seconds"`
}

// ReadinessResponse is the body of /readyz.
type ReadinessResponse struct {
	Status    string         `json:"status"`
	CheckedAt string         `json:"checked_at"`
	Reason    string         `json:"reason,omitempty"`
	Checks    []HealthzCheck `json:"checks"`
}

// draining is set when the application begins shutting down, so readiness can
// answer "not ready" while in-flight requests finish.
var draining atomic.Bool

// BeginDraining marks the application as shutting down: /readyz starts failing
// so a load balancer stops sending new work here, while /livez keeps passing
// so the orchestrator does not kill the process mid-drain.
//
// That difference is the whole reason these are two endpoints. With one, a
// slow dependency reads as a dead process and gets restarted, and a draining
// process keeps receiving traffic until it disappears.
func BeginDraining() { draining.Store(true) }

// Draining reports whether the application has started shutting down.
func Draining() bool { return draining.Load() }

// resetDrainingForTest puts the flag back, because it is process-wide state
// and a test that sets it would otherwise change what every later test sees.
func resetDrainingForTest() { draining.Store(false) }

// handleLivez answers whether the PROCESS is alive.
//
// It deliberately checks nothing else. A liveness probe that fails when the
// database is down asks the orchestrator to restart a process that is working
// perfectly — restarting it will not fix the database, and doing so across
// every replica at once turns a dependency's bad minute into an outage.
func (a *App) handleLivez(c *router.Context) error {
	return c.JSON(http.StatusOK, LivenessResponse{
		Status:        "alive",
		UptimeSeconds: int64(time.Since(a.startedAt).Seconds()),
	})
}

// handleReadyz answers whether this instance should receive traffic.
//
// This is where dependencies matter: an instance whose database is unreachable
// can be alive and unable to serve, and taking it out of the load balancer is
// exactly right. It is also what answers "no" while the process drains.
func (a *App) handleReadyz(c *router.Context) error {
	if Draining() {
		return c.JSON(http.StatusServiceUnavailable, ReadinessResponse{
			Status:    "draining",
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
			Reason:    "the process is shutting down",
			Checks:    []HealthzCheck{},
		})
	}

	checks := a.healthzChecks(c.Request.Context())
	status := "ready"
	code := http.StatusOK
	for _, ch := range checks {
		if ch.Status != "healthy" {
			status = "not ready"
			code = http.StatusServiceUnavailable
			break
		}
	}
	return c.JSON(code, ReadinessResponse{
		Status:    status,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Checks:    checks,
	})
}
