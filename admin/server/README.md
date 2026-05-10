# admin/server

Standalone Nucleus admin observability server. Accepts agent
connections (`AgentService.Stream`) and serves the admin web UI plus
its `ControlService` API.

## Run it

```bash
make build              # builds bin/admin-server with the UI embedded
./bin/admin-server      # default flags: agents :9090, UI :8080

# Production-flavoured invocation:
./bin/admin-server \
  --agent-addr=:9090 \
  --ui-addr=:8080 \
  --agent-token="$NUCLEUS_ADMIN_TOKEN" \
  --agent-cert=/etc/nucleus/server.crt \
  --agent-key=/etc/nucleus/server.key \
  --ui-trusted-cidrs=10.42.0.0/16 \
  --log-format=json --log-level=info
```

Run `./bin/admin-server --help` (or `--version`) for the full surface.
Every flag has a `NUCLEUS_ADMIN_*` env var counterpart.

## Sub-packages

| Sub-package         | Responsibility                                                                                                            |
|---------------------|---------------------------------------------------------------------------------------------------------------------------|
| `config`            | `Config` struct: addresses, TLS, tokens, ring buffer sizes, snapshot timeout, agent inactivity timeout.                   |
| `nodes`             | Connected-agents registry with watchers and per-entry frame-send channels.                                                |
| `routing/eventbus`  | Server-side fanout: per-UI subscriptions, drop-newest on full channel, `AggregateFilter` for the agent-side union sub.    |
| `routing/replay`    | Per-event-kind drop-oldest replay buffer for `include_recent`.                                                           |
| `routing/snapshot`  | Request-ID correlation between UI's `GetSnapshot` and the agent's `SnapshotResponse`.                                    |
| `routing/match`     | HTTP method/glob/status-class + SQL model matchers shared with the in-process Filter.                                    |
| `auth`              | Agent shared bearer token + UI trusted-proxy/bearer middlewares. `/healthz` is carved out of auth on both listeners.     |
| `services`          | Connect-RPC handlers for `AgentService.Stream` and `ControlService.{ListNodes,StreamEvents,GetSnapshot}`.                 |
| `ui`                | `//go:embed all:dist`. Serves the React bundle at `/`, falls back to a placeholder if the dist hasn't been built.        |
| `cmd/admin-server`  | The binary's main: flags, env, signal handling, TLS loading.                                                            |

The top-level `Server` (`server.go`) composes everything:

* Two `http.Server` listeners (h2c by default, TLS when configured)
  with separate auth chains — one for agents, one for UIs.
* `/healthz` public on both listeners (load balancer-friendly).
* Graceful shutdown on ctx cancel: best-effort `http.Server.Shutdown`
  with a 2-second timeout per listener.

## Observability of the observability server

* `/metrics` (when `--metrics-addr` is set; not exposed by default).
* Structured logging via `slog`. JSON or text format.
* Per-stream events are NEVER persisted. The replay buffer is in-memory
  and bounded.

## Tests

```bash
cd admin/server && go test -race ./...
```

* `server_integration_test.go` — full agent-server-UI happy paths and
  the auth gates (`AgentToken`, `UIBearer`, `UI placeholder`).
* `nodes/registry_test.go`, `routing/eventbus_test.go`,
  `routing/snapshot_test.go` — table-driven unit coverage of the
  routing primitives.
