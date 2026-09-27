// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package mail

import (
	"context"
	"sync"
)

// MemorySender is the mail driver a test wants: it keeps every message the
// application sends, in order, and delivers nothing. Select it with
// mail_driver: memory; pkg/nucleustest selects it on its own when the
// application would otherwise discard mail (the noop driver), so a test can
// assert on what a flow sent without configuring anything.
//
// It never fails, so the circuit breaker is not wrapped around it: a breaker
// that cannot trip would only hide the type a test reaches for.
type MemorySender struct {
	mu   sync.Mutex
	sent []Message
}

// NewMemorySender returns an empty capturing sender.
func NewMemorySender() *MemorySender { return &MemorySender{} }

// Send records the message. The Message is copied by value; the slices it
// carries are the caller's.
func (m *MemorySender) Send(_ context.Context, message Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, message)
	return nil
}

// Sent returns every message sent so far, oldest first, as a copy.
func (m *MemorySender) Sent() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Message, len(m.sent))
	copy(out, m.sent)
	return out
}

// Reset forgets the messages sent so far.
func (m *MemorySender) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = nil
}

// Health reports a sender that is always available.
func (m *MemorySender) Health(context.Context) error { return nil }

func newMemorySender(Config) (Sender, error) { return NewMemorySender(), nil }
