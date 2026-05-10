# Nucleus Admin Observability — Benchmarks

This document publishes the runtime overhead of the new admin
observability subsystem on the framework's hot path. Numbers are
reproducible from this repository:

```bash
# Hot-path bus benchmarks (Phase 2 deliverable)
go test ./pkg/observability/ -bench='HasSubscribers|Emit|Acquire' \
    -benchtime=2s -run=^$ -benchmem
```

Reference hardware: **Apple M4 Pro, Go 1.26.3, darwin/arm64**, single-
threaded. The constants below are an order-of-magnitude — relative
ranking is what matters when porting to a different CPU.

## Hot-path gate (`Bus.HasSubscribers`)

The framework's HTTP middleware and SQL observer call this BEFORE
allocating an event:

```go
if !bus.HasSubscribers(observability.KindHTTPRequest) {
    next.ServeHTTP(w, r)
    return
}
// ... full instrumentation only when an operator is watching ...
```

| Benchmark                   | ns/op   | B/op | allocs/op |
|-----------------------------|--------:|-----:|----------:|
| `HasSubscribers` idle       |   0.246 |    0 |         0 |
| `HasSubscribers` active     |   0.234 |    0 |         0 |

The compiler inlines the atomic `Load`. Cost is **one CPU cycle on
modern x86/arm**, well under the design target of < 5 ns. There is no
measurable difference between idle (no subscribers) and active (one or
more subscribers): both paths read the same `atomic.Int64`.

Operationally: if no operator has the admin panel open, the framework
pays a single inlined `mov` + `cmp` per HTTP request and per SQL query
to discover that nothing is observing, then continues on the original
code path. **The framework's worst-case latency is unaffected.**

## Bus emit cost (when something IS observing)

These benchmarks measure the cost of constructing and publishing one
event, with and without a subscriber. The producer obtains an event
from `sync.Pool`, fills it, and calls `Bus.Emit`. The bus increments
refcounts, fans out non-blockingly to every matching subscriber, and
balances the producer's reference.

| Benchmark                       |    ns/op | B/op | allocs/op |
|---------------------------------|---------:|-----:|----------:|
| `AcquireRelease` (no Emit)      |    9.90  |    0 |         0 |
| `Emit` no subscribers           |   11.05  |    0 |         0 |
| `Emit` one subscriber           |  201.10  |    0 |         0 |

**Zero allocations across every path.** The `sync.Pool` round-trip
covers the producer side; the bus reuses fixed slices for fanout.

Interpretation:

- *Idle* HTTP request: ~0.25 ns. The middleware's gate short-circuits.
- *No-subscriber Emit*: ~11 ns. Negligible. This is the path where a
  hook ignores `HasSubscribers` and emits anyway — still cheap because
  the bus's per-kind counter is the next gate.
- *One subscriber*: ~201 ns. The cost includes channel send, refcount
  arithmetic, and per-subscription filter evaluation. With N additional
  subscribers we pay roughly +50 ns per subscriber for the additional
  fanout sends.

For perspective, a typical SQLite SELECT on a hot table is on the order
of microseconds (1000 ns); a real PostgreSQL query is tens to hundreds
of microseconds. **The bus is two to three orders of magnitude cheaper
than the operations it observes.** Even in the worst case (10 operators
all subscribed to all kinds), the agent's overhead is less than 1 % of
typical request latency.

## Agent shipping cost (Phase 3)

When the bus has at least one subscriber, the agent's drain goroutine
serializes each event to a proto message and writes it on the bidi
stream. Empirically, on the same hardware:

- Conversion `observability.Event → proto Event`:
  ~120 ns/op for the largest event (`HTTPRequestEvent`), zero allocs
  beyond a fresh proto message.
- Stream send (Connect-RPC over h2c, localhost): ~5–8 µs/op,
  dominated by HTTP/2 framing overhead in `golang.org/x/net/http2`.

Net: when an operator IS watching, the per-event hot-path cost from
"happened in the framework" to "left the agent over the wire" is
roughly **5–8 µs**. That is below typical HTTP request servicing time
(milliseconds) and well within the budget of any production service.

## Hard constraints we maintain

The benchmarks above hold true while the agent satisfies these
invariants (verified by `go test -race ./...` and by `goleak.VerifyTestMain`
in pkg/observability):

1. **Hot path never blocks**. Bus channels use non-blocking sends with
   drop-newest on overflow. A slow subscriber NEVER stalls the
   framework's request thread.
2. **Hot path never allocates** when nobody is watching. The
   `HasSubscribers` gate is the only cost.
3. **No goroutines leak**. The bus does not spawn goroutines; the
   agent's three goroutines (recv/send/heartbeat) all observe a single
   stream-lifetime context for shutdown.
4. **Agent failure is invisible to the framework**. A disconnected
   admin server, a full ring buffer, or an outright agent crash never
   propagate to user-facing requests. (`require_admin` opts out of this
   for compliance-sensitive deployments — see admin/README.md.)

## Reproducing on your hardware

```bash
# Bus benchmarks
go test ./pkg/observability/ -bench='HasSubscribers|Emit|Acquire' \
    -benchtime=2s -run=^$ -benchmem

# Agent / wire benchmarks (require buf for codegen on a fresh checkout)
make proto
go test ./admin/agent/... -bench=. -run=^$ -benchmem
```

Numbers within a factor of two on different hardware are expected;
within an order of magnitude is the contract.
