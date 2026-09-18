// Package signals provides a synchronous and asynchronous event bus for the
// Nucleus framework. It implements a pattern similar to Django signals, allowing
// decoupled components to react to model lifecycle events (pre/post save, delete, etc.).
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

// Signal identifies the type of event being emitted.
type Signal string

// Built-in signals for model lifecycle events.
const (
	PreCreate  Signal = "pre_create"
	PostCreate Signal = "post_create"
	PreSave    Signal = "pre_save"
	PostSave   Signal = "post_save"
	PreDelete  Signal = "pre_delete"
	PostDelete Signal = "post_delete"
	PreUpdate  Signal = "pre_update"
	PostUpdate Signal = "post_update"
)

// Event carries the data associated with a signal emission.
type Event struct {
	Signal    Signal
	ModelName string
	Payload   any
	Ctx       context.Context
}

// Handler is a function that processes an event. Returning an error from a
// synchronous handler aborts the operation that triggered the signal.
type Handler func(Event) error

// Bus manages signal handlers and dispatches events.
type Bus struct {
	mu       sync.RWMutex
	handlers map[Signal][]Handler
	logger   *slog.Logger
	// asyncSem bounds the goroutines EmitAsync has RUNNING; a burst of
	// events used to spawn one per handler with no ceiling (NU-35).
	asyncSem chan struct{}
	// admitSem bounds how many dispatches exist at all, so not blocking the
	// emitter (NU-78) cannot turn into unbounded goroutines.
	admitSem chan struct{}
	dropped  atomic.Int64
}

// asyncLimit is how many EmitAsync handlers may RUN at once per bus.
const asyncLimit = 64

// admitLimit is how many dispatches may be in flight — running or waiting for
// a slot — before further ones are dropped. It is deliberately far above
// asyncLimit: a burst should queue, and only a subscriber that has genuinely
// stopped keeping up should cost events.
const admitLimit = 4096

// NewBus creates a new signal bus. The logger is used for async error reporting.
func NewBus(logger *slog.Logger) *Bus {
	return &Bus{
		handlers: make(map[Signal][]Handler),
		logger:   logger,
		asyncSem: make(chan struct{}, asyncLimit),
		admitSem: make(chan struct{}, admitLimit),
	}
}

// On registers a handler for the given signal. Handlers are called in
// registration order when the signal is emitted.
func (b *Bus) On(signal Signal, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[signal] = append(b.handlers[signal], handler)
}

// Emit dispatches an event synchronously. All registered handlers for the
// event's signal are called in order. If any handler returns an error,
// execution stops and the error is returned.
//
// A handler that PANICS is turned into that same error rather than unwinding
// into the caller. The asynchronous path has recovered since NU-35; this one
// did not, so a subscriber could take down the model save or the request that
// emitted the event — two paths of one bus with different guarantees, and the
// dangerous one was the path that runs inside the user's operation (NU-79).
func (b *Bus) Emit(event Event) error {
	b.mu.RLock()
	handlers := b.handlers[event.Signal]
	b.mu.RUnlock()

	for _, h := range handlers {
		if err := b.call(event, h); err != nil {
			return fmt.Errorf("signals.Emit %s: %w", event.Signal, err)
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
func (b *Bus) call(event Event, h Handler) (err error) {
	defer func() {
		if rv := recover(); rv != nil {
			if b.logger != nil {
				b.logger.Error("signals.Emit handler panicked",
					"signal", string(event.Signal),
					"model", event.ModelName,
					"panic", fmt.Sprint(rv),
					"stack", string(debug.Stack()),
				)
			}
			err = fmt.Errorf("%w: %v", ErrHandlerPanic, rv)
		}
	}()
	return h(event)
}

// EmitAsync dispatches an event asynchronously. Each handler runs in its own
// goroutine. Errors are logged but do not propagate.
//
// It does NOT block the caller. It used to: the concurrency slot was taken on
// the caller's goroutine, so once the ceiling was full the emitter waited for a
// handler to finish — and the emitter is usually a model save inside a request,
// so the slowest subscriber became latency for the user (NU-78). The slot is
// now taken inside the goroutine.
//
// That leaves the question the old shape answered by accident: what happens
// when handlers cannot keep up. The answer is written down rather than
// inherited — a bounded number of dispatches may be in flight, and beyond that
// an event is DROPPED with a log. Dropping is the honest failure for a bus
// whose contract already says errors do not propagate; blocking the emitter is
// not, because it turns a subscriber's problem into the user's.
func (b *Bus) EmitAsync(event Event) {
	b.mu.RLock()
	handlers := b.handlers[event.Signal]
	b.mu.RUnlock()

	for _, h := range handlers {
		if !b.admit() {
			b.dropped.Add(1)
			if b.logger != nil {
				b.logger.Error("signals.EmitAsync dropped an event: too many dispatches in flight",
					"signal", string(event.Signal),
					"model", event.ModelName,
					"in_flight_limit", admitLimit,
					"dropped_total", b.dropped.Load(),
				)
			}
			continue
		}
		go func(fn Handler) {
			defer b.release()
			// The execution slot is taken HERE, not in the caller.
			b.acquireAsync()
			defer b.releaseAsync()
			defer func() {
				// A panicking async handler took the process down; now it
				// is a logged failure like a returned error.
				if rv := recover(); rv != nil && b.logger != nil {
					b.logger.Error("signals.EmitAsync handler panicked",
						"signal", string(event.Signal),
						"model", event.ModelName,
						"panic", fmt.Sprint(rv),
					)
				}
			}()
			if err := fn(event); err != nil && b.logger != nil {
				b.logger.Error("signals.EmitAsync handler failed",
					"signal", string(event.Signal),
					"model", event.ModelName,
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

// Clear removes all handlers for the given signal, or all handlers if no signal
// is specified. Useful for testing.
func (b *Bus) Clear(signals ...Signal) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(signals) == 0 {
		b.handlers = make(map[Signal][]Handler)
		return
	}
	for _, s := range signals {
		delete(b.handlers, s)
	}
}
