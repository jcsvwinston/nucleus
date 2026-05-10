// Package agent embeds in every Nucleus framework process and connects to a
// standalone admin server (see ../server) to ship observability events.
//
// At the time of writing this file is a Phase-1 skeleton: the module exists
// only so that go.work and the generated proto stubs have a place to be
// imported from later. The actual agent loop, ring buffer, reconnection
// strategy, and Prometheus metrics ship in Phase 3 of the refactor plan.
//
// Hard architectural invariants for the agent (see refactor plan):
//
//   - The framework's hot path must NEVER block on, fail because of, or wait
//     for the agent. The agent is a strictly opt-in observer.
//   - When no operator is subscribed, the agent's per-event-type atomic
//     counter is zero and the agent is a no-op. Constructing an event must be
//     gated on that counter at the call site.
//   - The agent dials the admin server, never the other way around. This
//     makes NAT/firewall traversal symmetric between on-prem and cloud
//     deployments.
//   - The connection list (admin.endpoints in nucleus.yml) is tried in order
//     with health-check; only after every endpoint fails does the agent enter
//     exponential backoff (cap 30s, jitter).
//   - Events that cannot be shipped are dropped (drop-oldest per type) and
//     reported via the admin_agent_events_dropped_total Prometheus counter.
//     The framework never persists telemetry; that is OpenTelemetry's job.
package agent
