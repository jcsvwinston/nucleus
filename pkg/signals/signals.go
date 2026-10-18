// Package signals provides a synchronous and asynchronous event bus for the
// Nucleus framework: named, typed topics an application publishes its own
// events on, with handlers that receive the payload as the type the topic
// declares and the context the event was published with.
//
// The bus is keyed by name. A Topic[T] is a name plus a payload type;
// Subscribe, Publish and PublishAsync are the API. The untyped door of v1 —
// Signal, Event, Handler, Bus.On, Bus.Emit and Bus.EmitAsync — left at
// v2.0.0 (DEP-2026-025), and with it the model lifecycle names pkg/model
// emitted (DEP-2026-024). What feeds the bus from outside the process — the
// Redis relay and the outbox's bus bridge — hands a typed subscriber the JSON
// it carried, which the subscriber decodes into its topic's type.
package signals

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// event is what travels inside the bus: a topic name, the payload as the
// publisher handed it over, and the context it was published with.
type event struct {
	name    string
	payload any
	ctx     context.Context
}

// handler is a subscriber as the bus stores it. Subscribe wraps the typed
// function the application wrote; the bus never sees the payload's type.
type handler func(event) error

// Bus manages subscribers and dispatches events.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]handler
	logger   *slog.Logger
	// asyncSem bounds the goroutines PublishAsync has RUNNING; a burst of
	// events used to spawn one per handler with no ceiling (NU-35).
	asyncSem chan struct{}
	// admitSem bounds how many dispatches exist at all, so not blocking the
	// publisher (NU-78) cannot turn into unbounded goroutines.
	admitSem chan struct{}
	dropped  atomic.Int64
}

// asyncLimit is how many PublishAsync handlers may RUN at once per bus.
const asyncLimit = 64

// admitLimit is how many dispatches may be in flight — running or waiting for
// a slot — before further ones are dropped. It is deliberately far above
// asyncLimit: a burst should queue, and only a subscriber that has genuinely
// stopped keeping up should cost events.
const admitLimit = 4096

// NewBus creates a new bus. The logger is used for async error reporting.
func NewBus(logger *slog.Logger) *Bus {
	return &Bus{
		handlers: make(map[string][]handler),
		logger:   logger,
		asyncSem: make(chan struct{}, asyncLimit),
		admitSem: make(chan struct{}, admitLimit),
	}
}

// on registers a handler for the topic name. Handlers are called in
// registration order when an event of that name is published.
func (b *Bus) on(name string, h handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[name] = append(b.handlers[name], h)
}

// emit dispatches an event synchronously. All handlers registered for the
// event's name are called in order. If any handler returns an error,
// execution stops and the error is returned.
//
// A handler that PANICS is turned into that same error rather than unwinding
// into the caller. The asynchronous path has recovered since NU-35; this one
// did not, so a subscriber could take down the operation that published the
// event — two paths of one bus with different guarantees, and the dangerous
// one was the path that runs inside the user's operation (NU-79).
func (b *Bus) emit(e event) error {
	b.mu.RLock()
	handlers := b.handlers[e.name]
	b.mu.RUnlock()

	for _, h := range handlers {
		if err := b.call(e, h); err != nil {
			return fmt.Errorf("signals.Publish %s: %w", e.name, err)
		}
	}
	return nil
}

// ErrHandlerPanic is what a panicking handler becomes. It is a distinct error
// so a caller can tell "the subscriber refused this operation" from "the
// subscriber is broken".
var ErrHandlerPanic = errors.New("signals: handler panicked")

// call runs one handler, converting a panic into an error. The stack is logged
// where it happens, because the error that reaches the caller cannot carry it.
func (b *Bus) call(e event, h handler) (err error) {
	defer func() {
		if rv := recover(); rv != nil {
			if b.logger != nil {
				b.logger.Error("signals.Publish handler panicked",
					"topic", e.name,
					"panic", fmt.Sprint(rv),
					"stack", string(debug.Stack()),
				)
			}
			err = fmt.Errorf("%w: %v", ErrHandlerPanic, rv)
		}
	}()
	return h(e)
}

// emitAsync dispatches an event asynchronously. Each handler runs in its own
// goroutine. Errors are logged but do not propagate.
//
// It does NOT block the caller. It used to: the concurrency slot was taken on
// the caller's goroutine, so once the ceiling was full the publisher waited for
// a handler to finish — and the publisher is usually a write inside a request,
// so the slowest subscriber became latency for the user (NU-78). The slot is
// now taken inside the goroutine.
//
// That leaves the question the old shape answered by accident: what happens
// when handlers cannot keep up. The answer is written down rather than
// inherited — a bounded number of dispatches may be in flight, and beyond that
// an event is DROPPED with a log. Dropping is the honest failure for a bus
// whose contract already says errors do not propagate; blocking the publisher
// is not, because it turns a subscriber's problem into the user's.
func (b *Bus) emitAsync(e event) {
	b.mu.RLock()
	handlers := b.handlers[e.name]
	b.mu.RUnlock()

	for _, h := range handlers {
		if !b.admit() {
			b.dropped.Add(1)
			if b.logger != nil {
				b.logger.Error("signals.PublishAsync dropped an event: too many dispatches in flight",
					"topic", e.name,
					"in_flight_limit", admitLimit,
					"dropped_total", b.dropped.Load(),
				)
			}
			continue
		}
		go func(fn handler) {
			defer b.release()
			// The execution slot is taken HERE, not in the caller.
			b.acquireAsync()
			defer b.releaseAsync()
			defer func() {
				// A panicking async handler took the process down; now it
				// is a logged failure like a returned error.
				if rv := recover(); rv != nil && b.logger != nil {
					b.logger.Error("signals.PublishAsync handler panicked",
						"topic", e.name,
						"panic", fmt.Sprint(rv),
					)
				}
			}()
			if err := fn(e); err != nil && b.logger != nil {
				b.logger.Error("signals.PublishAsync handler failed",
					"topic", e.name,
					"error", err.Error(),
				)
			}
		}(h)
	}
}

// Dropped reports how many asynchronous dispatches were dropped because too
// many were already in flight. A bus that silently drops is worse than one that
// blocks; this is how an application notices.
func (b *Bus) Dropped() int64 { return b.dropped.Load() }

// admit reserves one of the in-flight slots without blocking.
func (b *Bus) admit() bool {
	if b.admitSem == nil {
		return true
	}
	select {
	case b.admitSem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (b *Bus) release() {
	if b.admitSem != nil {
		<-b.admitSem
	}
}

func (b *Bus) acquireAsync() {
	if b.asyncSem != nil {
		b.asyncSem <- struct{}{}
	}
}

func (b *Bus) releaseAsync() {
	if b.asyncSem != nil {
		<-b.asyncSem
	}
}

// Clear removes all handlers for the given topic names, or every handler if
// no name is given. Useful for testing. A name is what Define was given, or
// Topic.String().
func (b *Bus) Clear(topics ...string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(topics) == 0 {
		b.handlers = make(map[string][]handler)
		return
	}
	for _, name := range topics {
		delete(b.handlers, name)
	}
}
