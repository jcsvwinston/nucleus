// Package hooks plugs the framework's existing instrumentation points
// (HTTP middleware, SQL observer, session manager) into the
// observability.Bus so the agent (and direct subscribers) can receive
// strongly typed events.
//
// Each hook is independent: import only the ones you need. All hooks gate
// event construction on observability.Bus.HasSubscribers(kind) so they are
// safe to mount unconditionally — when nobody is watching, the gate
// short-circuits to a single atomic load.
//
// Sanitization is the hook's responsibility. By the time the event reaches
// the bus, sensitive request body bytes, raw SQL argument values, and full
// session tokens have been replaced with redacted summaries. The bus itself
// makes no judgement about content.
package hooks
