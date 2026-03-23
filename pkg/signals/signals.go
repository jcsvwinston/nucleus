// Package signals provides a synchronous and asynchronous event bus for the
// GoFrame framework. It implements a pattern similar to Django signals, allowing
// decoupled components to react to model lifecycle events (pre/post save, delete, etc.).
package signals

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
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
}

// NewBus creates a new signal bus. The logger is used for async error reporting.
func NewBus(logger *slog.Logger) *Bus {
	return &Bus{
		handlers: make(map[Signal][]Handler),
		logger:   logger,
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
// event's signal are called in order. If any handler returns an error, execution
// stops and the error is returned.
func (b *Bus) Emit(event Event) error {
	b.mu.RLock()
	handlers := b.handlers[event.Signal]
	b.mu.RUnlock()

	for _, h := range handlers {
		if err := h(event); err != nil {
			return fmt.Errorf("signals.Emit %s: %w", event.Signal, err)
		}
	}
	return nil
}

// EmitAsync dispatches an event asynchronously. Each handler runs in its own
// goroutine. Errors are logged but do not propagate.
func (b *Bus) EmitAsync(event Event) {
	b.mu.RLock()
	handlers := b.handlers[event.Signal]
	b.mu.RUnlock()

	for _, h := range handlers {
		go func(fn Handler) {
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
