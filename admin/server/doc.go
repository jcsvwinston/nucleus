// Package server implements the standalone Nucleus admin observability
// server. It accepts AgentService streams from agents (one per framework
// process) and ControlService unary/server-streaming calls from the embedded
// web UI.
//
// Phase 1 ships only the module skeleton. Phase 4 implements the connection
// registry, ring buffers, snapshot routing, auth middleware, and metrics.
//
// Architecture invariants for the server (see refactor plan):
//
//   - The admin server is single-instance by default. Active-passive failover
//     is supported by configuring multiple endpoints in the agents'
//     admin.endpoints list. Active-active is documented as a future extension
//     but is NOT implemented in this refactor.
//
//   - The server NEVER calls back into agents over a separate connection. All
//     server-to-agent traffic (Subscribe, Unsubscribe, SnapshotRequest)
//     travels on the existing AgentService.Stream multiplexed Frame channel.
//
//   - The server is NOT persistence: events live in bounded ring buffers and
//     are dropped on overflow. Long-term retention is OpenTelemetry's job.
//
//   - mTLS or shared-token auth gates the agent listener; trusted-proxy
//     headers (X-Auth-User, X-Auth-Email) plus an optional bearer fallback
//     gate the UI listener. The server is never exposed through the
//     application's public load balancer.
package server
