package observability

import (
	"sync"
	"testing"
	"time"
)

func TestRingBuffer_DropOldest(t *testing.T) {
	rb := NewRingBuffer[int](3)
	rb.Push(time.Now(), 1)
	rb.Push(time.Now(), 2)
	rb.Push(time.Now(), 3)
	rb.Push(time.Now(), 4) // evicts 1
	rb.Push(time.Now(), 5) // evicts 2

	got := rb.Snapshot(10)
	want := []int{5, 4, 3} // newest first
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if rb.Len() != 3 {
		t.Errorf("Len = %d, want 3", rb.Len())
	}
	if rb.Capacity() != 3 {
		t.Errorf("Capacity = %d, want 3", rb.Capacity())
	}
	if rb.Dropped() != 2 {
		t.Errorf("Dropped = %d, want 2", rb.Dropped())
	}
}

func TestRingBuffer_Snapshot_LimitClamping(t *testing.T) {
	rb := NewRingBuffer[string](10)
	rb.Push(time.Now(), "a")
	rb.Push(time.Now(), "b")

	if got := rb.Snapshot(0); len(got) != 0 {
		t.Errorf("Snapshot(0) should be empty, got %v", got)
	}
	if got := rb.Snapshot(-1); len(got) != 0 {
		t.Errorf("Snapshot(negative) should be empty, got %v", got)
	}
	if got := rb.Snapshot(100); len(got) != 2 {
		t.Errorf("Snapshot(over) should clamp to size, got len=%d", len(got))
	}
}

func TestRingBuffer_DefaultCapacity(t *testing.T) {
	rb := NewRingBuffer[int](0)
	if rb.Capacity() != 64 {
		t.Errorf("default capacity = %d, want 64", rb.Capacity())
	}
}

func TestRingBuffer_ConcurrentPushSnapshot(t *testing.T) {
	rb := NewRingBuffer[int](128)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			rb.Push(time.Now(), i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = rb.Snapshot(50)
		}
	}()

	wg.Wait()

	// Final snapshot should not panic and should respect capacity.
	if got := rb.Snapshot(rb.Capacity()); len(got) > rb.Capacity() {
		t.Fatalf("snapshot len=%d exceeds capacity=%d", len(got), rb.Capacity())
	}
}
