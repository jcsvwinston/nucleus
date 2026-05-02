package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/quark"
)

// Store is a professional in-memory implementation of quark.CacheStore.
var _ quark.CacheStore = (*Store)(nil)

// It supports tag-based invalidation via a reverse-index and is thread-safe.
type Store struct {
	mu         sync.RWMutex
	data       map[string]cacheEntry
	tagToIndex map[string]map[string]struct{}
}

type cacheEntry struct {
	Value      []byte
	Expiration time.Time
	Tags       []string
}

// New creates a new in-memory cache store.
func New() *Store {
	s := &Store{
		data:       make(map[string]cacheEntry),
		tagToIndex: make(map[string]map[string]struct{}),
	}
	// Start cleanup goroutine to prevent memory leaks
	go s.cleanupLoop()
	return s
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.data[key]
	if !ok || time.Now().After(entry.Expiration) {
		return nil, fmt.Errorf("cache miss")
	}
	return entry.Value, nil
}

func (s *Store) Set(ctx context.Context, key string, val []byte, ttl time.Duration, tags ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = cacheEntry{
		Value:      val,
		Expiration: time.Now().Add(ttl),
		Tags:       tags,
	}

	for _, tag := range tags {
		if _, ok := s.tagToIndex[tag]; !ok {
			s.tagToIndex[tag] = make(map[string]struct{})
		}
		s.tagToIndex[tag][key] = struct{}{}
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *Store) InvalidateTags(ctx context.Context, tags ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, tag := range tags {
		if keys, ok := s.tagToIndex[tag]; ok {
			for key := range keys {
				delete(s.data, key)
			}
			delete(s.tagToIndex, tag)
		}
	}
	return nil
}

func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for key, entry := range s.data {
			if now.After(entry.Expiration) {
				delete(s.data, key)
			}
		}
		s.mu.Unlock()
	}
}
