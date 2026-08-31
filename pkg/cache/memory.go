package cache

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Memory is an in-process Cache backend. It is safe for concurrent use and
// suitable for single-replica deployments; replicas do NOT share it — use
// the SQL backend (or an external store) when running more than one server
// process (see docs/guides/DEPLOYMENT_GUIDE.md, shared-state table).
//
// Expired entries are dropped lazily: Get treats them as absent and deletes
// them, and every sweepEvery-th Set sweeps the whole map (amortised, so a
// write-heavy cache does not pay O(n) per Set). There is no background
// janitor goroutine, so a Memory cache needs no shutdown call. Call
// PruneExpired from your own maintenance schedule for tighter bounds.
type Memory struct {
	mu       sync.Mutex
	entries  map[string]memoryEntry
	setCount int

	// now is the clock, injectable in tests.
	now func() time.Time
}

// sweepEvery is the number of Sets between full expired-entry sweeps.
const sweepEvery = 1024

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

// NewMemory creates an empty in-memory cache.
func NewMemory() *Memory {
	return &Memory{
		entries: make(map[string]memoryEntry),
		now:     time.Now,
	}
}

// Get implements Cache.
func (m *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	if strings.TrimSpace(key) == "" {
		return nil, false, ErrEmptyKey
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[key]
	if !ok {
		return nil, false, nil
	}
	if !entry.expiresAt.After(m.now()) {
		delete(m.entries, key)
		return nil, false, nil
	}
	// Copy so a caller mutating the returned slice cannot corrupt the cache.
	out := make([]byte, len(entry.value))
	copy(out, entry.value)
	return out, true, nil
}

// Set implements Cache.
func (m *Memory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if strings.TrimSpace(key) == "" {
		return ErrEmptyKey
	}
	if ttl <= 0 {
		return ErrNonPositiveTTL
	}
	stored := make([]byte, len(value))
	copy(stored, value)

	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	m.setCount++
	if m.setCount >= sweepEvery {
		m.setCount = 0
		for k, entry := range m.entries {
			if !entry.expiresAt.After(now) {
				delete(m.entries, k)
			}
		}
	}
	m.entries[key] = memoryEntry{value: stored, expiresAt: now.Add(ttl)}
	return nil
}

// Delete implements Cache.
func (m *Memory) Delete(_ context.Context, key string) error {
	if strings.TrimSpace(key) == "" {
		return ErrEmptyKey
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}

// PruneExpired removes every expired entry and returns how many it removed.
func (m *Memory) PruneExpired(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	removed := 0
	for k, entry := range m.entries {
		if !entry.expiresAt.After(now) {
			delete(m.entries, k)
			removed++
		}
	}
	return removed, nil
}
