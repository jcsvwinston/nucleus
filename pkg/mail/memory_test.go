// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package mail

import (
	"context"
	"testing"
)

func TestMemorySenderKeepsWhatWasSent(t *testing.T) {
	s, err := NewSender(Config{Driver: "memory", CircuitBreaker: CircuitBreakerConfig{Enabled: true, FailureThreshold: 1}})
	if err != nil {
		t.Fatalf("NewSender(memory): %v", err)
	}
	mem, ok := s.(*MemorySender)
	if !ok {
		t.Fatalf("the memory driver came back wrapped as %T: a test cannot reach its messages", s)
	}
	if err := mem.Send(context.Background(), Message{To: []string{"a@example.test"}, Subject: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := mem.Send(context.Background(), Message{To: []string{"b@example.test"}, Subject: "two"}); err != nil {
		t.Fatal(err)
	}
	sent := mem.Sent()
	if len(sent) != 2 || sent[0].Subject != "one" || sent[1].Subject != "two" {
		t.Fatalf("sent = %+v", sent)
	}
	sent[0].Subject = "mutated"
	if mem.Sent()[0].Subject != "one" {
		t.Fatal("Sent returned the internal slice, not a copy")
	}
	mem.Reset()
	if len(mem.Sent()) != 0 {
		t.Fatal("Reset kept messages")
	}
}
