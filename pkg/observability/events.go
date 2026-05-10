package observability

import (
	"sync"
	"sync/atomic"
	"time"
)

// EventKind classifies events for routing, sampling, and per-type metrics.
// Values are stable: never reorder, never reuse. New kinds append at the end
// and bump numEventKinds. KindUnknown is the zero value to surface accidents.
type EventKind uint8

const (
	KindUnknown      EventKind = 0
	KindHTTPRequest  EventKind = 1
	KindSQLStatement EventKind = 2
	KindSessionChange EventKind = 3
	KindCustom       EventKind = 4

	// numEventKinds is the array length used for per-kind atomic counters.
	// It is one past the highest legal kind so that activeCounts[KindCustom]
	// is in range. Bumping kinds requires bumping this constant.
	numEventKinds = int(KindCustom) + 1
)

// String returns a stable lowercase identifier for the kind. It is used in
// metric labels and protocol enum keys.
func (k EventKind) String() string {
	switch k {
	case KindHTTPRequest:
		return "http_request"
	case KindSQLStatement:
		return "sql_statement"
	case KindSessionChange:
		return "session_change"
	case KindCustom:
		return "custom"
	default:
		return "unknown"
	}
}

// Event is the sealed interface every observability event satisfies. It is
// sealed (private method) so the framework can guarantee that every event
// passing through Bus.Emit was produced via Acquire* and is therefore part
// of the refcount + sync.Pool lifecycle. External packages construct events
// with the exported AcquireXxxEvent helpers.
type Event interface {
	// Kind returns the type of this event. The publisher and subscribers may
	// switch on Kind to type-assert to the concrete struct.
	Kind() EventKind

	// EmittedAt returns the wall-clock timestamp the producer set on the
	// event before calling Bus.Emit. Subscribers MUST NOT mutate this.
	EmittedAt() time.Time

	// NodeID returns the framework process identifier the event came from.
	// May be empty during local development.
	NodeID() string

	// Release decrements the refcount by one. When the refcount reaches
	// zero, the event is reset to its zero value and returned to its
	// sync.Pool. Calling Release more times than references are held
	// panics; this is an intentional crash-on-bug.
	Release()

	// acquireRef is called by the bus only, once per delivery target, to
	// reserve a reference that the consumer's eventual Release will balance.
	// Unexported to keep producers from poking at the refcount.
	acquireRef()
}

// baseEvent is embedded by every concrete event type. It carries the
// refcount and a back-pointer to the pool so Release can return the event
// to the right pool. baseEvent does NOT implement Event by itself; the
// concrete struct provides Kind() / EmittedAt() / NodeID().
type baseEvent struct {
	emittedAt time.Time
	nodeID    string

	refs atomic.Int32
	pool *sync.Pool
}

// EmittedAt and NodeID are shared across all events through the embedded
// baseEvent. Concrete types do not redefine these.
func (b *baseEvent) EmittedAt() time.Time { return b.emittedAt }
func (b *baseEvent) NodeID() string       { return b.nodeID }

// acquireRef increments the refcount. Internal to the package.
func (b *baseEvent) acquireRef() { b.refs.Add(1) }

// refsLoad is a test helper. Production code MUST NOT use it.
func (b *baseEvent) refsLoad() int32 { return b.refs.Load() }

// releaseRef decrements the refcount and runs reset+pool-put when it hits
// zero. It panics on negative refcount.
func (b *baseEvent) releaseRef(reset func()) {
	switch n := b.refs.Add(-1); {
	case n > 0:
		return
	case n == 0:
		reset()
	default:
		panic("observability: event Release called with no outstanding reference")
	}
}

// =============================================================================
// HTTPRequestEvent
// =============================================================================

// HTTPRequestEvent describes a completed HTTP request handled by the
// framework's router. Fields are pre-sanitized: PayloadPreview is a redacted
// summary of the request body or query string; sensitive query keys (key,
// secret, password, token) are starred at the producer.
type HTTPRequestEvent struct {
	baseEvent

	Method         string
	Path           string
	Status         int
	Duration       time.Duration
	RequestID      string
	TraceID        string
	UserID         string
	RemoteIP       string
	UserAgent      string
	PayloadPreview string
}

// Kind implements Event.
func (e *HTTPRequestEvent) Kind() EventKind { return KindHTTPRequest }

// Release implements Event. It returns the event to httpPool when the last
// reference is released.
func (e *HTTPRequestEvent) Release() {
	e.releaseRef(func() {
		pool := e.pool
		*e = HTTPRequestEvent{}
		e.pool = pool
		pool.Put(e)
	})
}

var httpPool = sync.Pool{
	New: func() any { return &HTTPRequestEvent{} },
}

// AcquireHTTPRequestEvent returns a zero-valued event with refcount=1 from
// the pool. The caller fills exported fields and the embedded baseEvent's
// timestamp + node id, then transfers ownership to Bus.Emit.
func AcquireHTTPRequestEvent(emittedAt time.Time, nodeID string) *HTTPRequestEvent {
	e := httpPool.Get().(*HTTPRequestEvent)
	e.pool = &httpPool
	e.emittedAt = emittedAt
	e.nodeID = nodeID
	e.refs.Store(1)
	return e
}

// =============================================================================
// SQLStatementEvent
// =============================================================================

// SQLStatementEvent describes a SQL query executed by the framework's CRUD
// layer. Args is pre-sanitized: each entry is a "type(len):***" marker for
// strings/bytes; primitives are formatted as "type:value".
type SQLStatementEvent struct {
	baseEvent

	ModelName string
	Operation string
	Query     string
	Args      []string
	Duration  time.Duration
	Err       string

	RequestID string
	TraceID   string
	UserID    string
}

func (e *SQLStatementEvent) Kind() EventKind { return KindSQLStatement }

func (e *SQLStatementEvent) Release() {
	e.releaseRef(func() {
		pool := e.pool
		// Reuse the Args slice capacity but zero its length to drop GC roots.
		args := e.Args[:0]
		*e = SQLStatementEvent{Args: args}
		e.pool = pool
		pool.Put(e)
	})
}

var sqlPool = sync.Pool{
	New: func() any { return &SQLStatementEvent{Args: make([]string, 0, 8)} },
}

// AcquireSQLStatementEvent returns a zero-valued event with refcount=1 from
// the pool. The Args slice has spare capacity ready for append.
func AcquireSQLStatementEvent(emittedAt time.Time, nodeID string) *SQLStatementEvent {
	e := sqlPool.Get().(*SQLStatementEvent)
	e.pool = &sqlPool
	e.emittedAt = emittedAt
	e.nodeID = nodeID
	e.refs.Store(1)
	return e
}

// =============================================================================
// SessionChangeEvent
// =============================================================================

// SessionChangeKind narrows a session change to one of three discrete events.
// The zero value SessionChangeUnspecified is illegal in published events.
type SessionChangeKind uint8

const (
	SessionChangeUnspecified SessionChangeKind = 0
	SessionChangeCreated     SessionChangeKind = 1
	SessionChangeTouched     SessionChangeKind = 2
	SessionChangeDestroyed   SessionChangeKind = 3
)

func (k SessionChangeKind) String() string {
	switch k {
	case SessionChangeCreated:
		return "created"
	case SessionChangeTouched:
		return "touched"
	case SessionChangeDestroyed:
		return "destroyed"
	default:
		return "unspecified"
	}
}

// SessionChangeEvent describes a lifecycle event on a user session. The
// session token never travels in cleartext; only TokenShort (first 8 chars)
// goes on the wire.
type SessionChangeEvent struct {
	baseEvent

	Change     SessionChangeKind
	TokenShort string
	UserID     string
	IP         string
	UserAgent  string
	LastRoute  string
	TraceID    string
}

func (e *SessionChangeEvent) Kind() EventKind { return KindSessionChange }

func (e *SessionChangeEvent) Release() {
	e.releaseRef(func() {
		pool := e.pool
		*e = SessionChangeEvent{}
		e.pool = pool
		pool.Put(e)
	})
}

var sessionPool = sync.Pool{
	New: func() any { return &SessionChangeEvent{} },
}

// AcquireSessionChangeEvent returns a zero-valued event with refcount=1
// from the pool.
func AcquireSessionChangeEvent(emittedAt time.Time, nodeID string) *SessionChangeEvent {
	e := sessionPool.Get().(*SessionChangeEvent)
	e.pool = &sessionPool
	e.emittedAt = emittedAt
	e.nodeID = nodeID
	e.refs.Store(1)
	return e
}

// =============================================================================
// CustomEvent
// =============================================================================

// CustomEvent is the application-defined extension point. Use it for
// domain-specific signals that do not fit one of the framework-managed
// kinds. Payload is opaque bytes; ContentType helps the UI render it.
type CustomEvent struct {
	baseEvent

	Name        string
	Labels      map[string]string
	Payload     []byte
	ContentType string
}

func (e *CustomEvent) Kind() EventKind { return KindCustom }

func (e *CustomEvent) Release() {
	e.releaseRef(func() {
		pool := e.pool
		// Reuse the Payload buffer but zero its length to drop GC roots.
		payload := e.Payload[:0]
		// Don't reuse Labels: maps cost more to clear than to drop.
		*e = CustomEvent{Payload: payload}
		e.pool = pool
		pool.Put(e)
	})
}

var customPool = sync.Pool{
	New: func() any { return &CustomEvent{Payload: make([]byte, 0, 256)} },
}

// AcquireCustomEvent returns a zero-valued event with refcount=1 from the
// pool. The Payload slice has spare capacity for append.
func AcquireCustomEvent(emittedAt time.Time, nodeID string) *CustomEvent {
	e := customPool.Get().(*CustomEvent)
	e.pool = &customPool
	e.emittedAt = emittedAt
	e.nodeID = nodeID
	e.refs.Store(1)
	return e
}
