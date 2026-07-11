// Package observability is the in-process event bus that the Nucleus
// framework uses to expose runtime activity (HTTP requests, SQL statements,
// session changes, custom application events) to optional observers — most
// importantly, the orbit module (github.com/jcsvwinston/orbit), which
// consumes this bus via Runtime.Observability() to power its live admin feed.
//
// The package is the "core" Phase-2 deliverable of the admin refactor. It is
// owned by the framework's hot path; correctness, lock-discipline, and idle
// cost matter more than ergonomics.
//
// # Architecture invariants
//
//  1. The Bus is a process-wide fan-out: one publisher (Emit) → N subscribers
//     (chan Event). It does NOT spawn goroutines. Each subscriber drains its
//     own channel from its own goroutine.
//
//  2. The hot path on the publisher side has a hard "zero-cost when nobody is
//     watching" requirement. Bus.HasSubscribers(kind) is a single atomic load
//     (target: < 5 ns on x86). Hooks MUST gate all event construction on it
//     before allocating, copying request/query bodies, or doing any other
//     instrumentation work.
//
//  3. Events are pooled via sync.Pool to keep allocations off the hot path
//     when an operator IS watching. Concrete event types embed a refcount;
//     the bus increments the refcount per delivery target and decrements on
//     drop or successful subscriber Release(). When the refcount reaches
//     zero, the event resets and returns to its pool.
//
//  4. The bus uses non-blocking sends (select with default) so a slow
//     subscriber cannot stall the framework. When a subscriber's channel is
//     full, the event for that subscriber is dropped and counted in
//     Bus.Stats(kind).Dropped. The publisher never blocks.
//
//  5. Events are immutable once Emit returns ownership to the bus. Both the
//     publisher and the subscriber MUST treat the event as read-only after
//     that point. The only legal mutation is Release(), which is safe to
//     call once per acquired reference.
//
// # Ownership rules (read carefully before writing producer code)
//
//   - The producer calls AcquireXxxEvent() to obtain an event with refcount=1
//     from the pool. The producer fills exported fields and calls Bus.Emit(e).
//     At that point the producer transfers ownership to the bus. The producer
//     MUST NOT touch the event after Emit returns.
//
//   - The bus increments the refcount once per delivery target (subscriber
//     whose Filter matches). It then attempts a non-blocking send for each
//     target. On a successful send the consumer owns one reference. On drop
//     the bus calls Release immediately on behalf of that target.
//
//   - The original reference the producer transferred is released by the bus
//     after fanout. So the bus never holds a long-term reference; it merely
//     forwards ownership.
//
//   - Each consumer reads its event from the channel, processes it, and
//     calls Release() exactly once. Calling Release more times than
//     references are held panics — that is by design, to surface lifecycle
//     bugs in tests.
//
//   - When there are no subscribers, the bus calls Release on the producer's
//     reference and returns immediately. The producer never observes that
//     "no one is listening"; it only observes that Emit took ownership.
//
// # Hook responsibilities
//
// Hooks (HTTP middleware, SQL observer, session activity recorder) live in
// the hooks subpackage. Each hook starts every code path with:
//
//	if !bus.HasSubscribers(observability.KindHTTPRequest) {
//	    next.ServeHTTP(w, r)
//	    return
//	}
//
// and only allocates the event after that gate is open. Hooks are also
// responsible for sanitizing user-supplied data: query strings are redacted
// at the source, SQL argument values become "type(len):***" markers rather
// than raw values, etc. The bus carries pre-sanitized strings to keep the
// admin server stateless about what is sensitive.
//
// # Threading model summary
//
//	Producer goroutine: AcquireXxx → fill → Bus.Emit → Bus.Emit returns
//	    [Bus internal: read subscribers under RLock, refcount++ per target,
//	     non-blocking send, release on drop, release original ref]
//	Subscriber goroutine: <-sub.Ch() → process → Event.Release()
//
// No goroutine is spawned by the bus or by Subscribe. Cancellation removes
// the subscription from the bus map; the subscriber's channel is leaked
// (drained by the runtime) so that an in-flight Emit racing with cancel
// cannot panic by sending on a closed channel.
package observability
