# admin/agent

The Nucleus admin observability agent. Embeds in every framework
process and ships events to a standalone admin server over a single
Connect-RPC bidi stream.

## Wiring into a Nucleus app

```go
import (
    "github.com/jcsvwinston/nucleus/pkg/app"
    "github.com/jcsvwinston/nucleus/admin/agent"
)

func main() {
    cfg := app.MustLoadConfig("nucleus.yml")
    a, err := app.New(cfg,
        app.WithExtensions(
            agent.NewExtension(cfg.AdminAgent, cfg.StateDir, "v0.7.0"),
        ),
    )
    if err != nil {
        log.Fatal(err)
    }
    if err := a.Run(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

When `cfg.AdminAgent.Endpoints` is empty, the extension is a no-op and
the framework runs unchanged. When it is set, the agent starts in
parallel with the framework's `Run`; observability events flow through
`pkg/observability` into the bidi stream.

See `admin/README.md` for the configuration reference.

## Layered structure

| Sub-package         | Responsibility                                                                                                     |
|---------------------|--------------------------------------------------------------------------------------------------------------------|
| `identity`          | Resolves the persistent NodeID (UUIDv4 in `${state_dir}/node_id`) with hostname-derived ephemeral fallback.        |
| `convert`           | `pkg/observability` events → proto events; `Filter` proto → in-process Filter.                                     |
| `sampler`           | Per-kind sampling rate + HTTP/SQL filter from server-side `Subscribe` commands.                                   |
| `buffer`            | Per-event-kind drop-oldest ring buffer for bridging brief disconnects.                                            |
| `connection`        | Endpoint failover dialer with exponential backoff (cap 30s) and rate-limited disconnect WARN (1/min).             |
| `stream`            | Bidi stream lifecycle: registration, three-goroutine recv/send/heartbeat loop, command dispatch, replay on reconnect. |
| `metrics`           | `admin_agent_*` Prometheus collectors + standalone `/metrics` + `/healthz` server.                                |
| `internal/testserver` | In-process h2c admin server fake for integration tests. (Internal; do not import.)                              |

The top-level `Agent` (`agent.go`) composes everything and exposes:

* `New(cfg)` — constructor; `ErrDisabled` when no endpoints.
* `Run(ctx)` — blocks until ctx cancels; reconnect loop with backoff.
* `NodeID()` — the resolved identifier.
* `Connected()` — channel closed on first successful stream open.
  Used by `NewExtension(...).Attach` when `RequireConnection` is true.
* `Metrics()` — the Prometheus registry, in case the host wants to
  serve `/metrics` from its own port.

## Hot-path invariants

The agent never blocks the framework's request thread. Every public
producer-side path (the HTTP middleware, the SQL observer) starts with
a single atomic load on `pkg/observability.Bus.HasSubscribers(kind)`
and short-circuits when nobody is watching. See `admin/BENCHMARKS.md`
for the measured cost (≈ 0.25 ns idle).

## Tests

```bash
cd admin/agent && go test -race ./...
```

Integration tests live in `agent_test.go` and `extension_test.go`. The
in-process `testserver` fakes the admin server with enough fidelity to
exercise reconnect, subscribe/unsubscribe, drain, and Goodbye paths.
