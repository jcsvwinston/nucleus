// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryStore is the storage provider a test wants: every object lives in
// the process and disappears with it. Select it with storage.provider:
// memory. It honours the same key rules as the local store and is what
// pkg/nucleustest reads back through Stored and StoredKeys.
type MemoryStore struct {
	mu      sync.RWMutex
	objects map[string]memoryObject
}

type memoryObject struct {
	data []byte
	info ObjectInfo
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{objects: map[string]memoryObject{}} }

func (s *MemoryStore) Put(_ context.Context, key string, reader io.Reader, opts PutOptions) (ObjectInfo, error) {
	key = normalizeKey(key)
	if err := validateKey(key); err != nil {
		return ObjectInfo{}, err
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("storage: memory Put %q: %w", key, err)
	}
	visibility := opts.Visibility
	if visibility == "" {
		visibility = Private
	}
	info := ObjectInfo{
		Key:         key,
		Size:        int64(len(data)),
		ContentType: opts.ContentType,
		Visibility:  visibility,
		Metadata:    copyMetadata(opts.Metadata),
		UpdatedAt:   time.Now().UTC(),
	}
	s.mu.Lock()
	s.objects[key] = memoryObject{data: data, info: info}
	s.mu.Unlock()
	return info, nil
}

func (s *MemoryStore) Get(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	key = normalizeKey(key)
	if err := validateKey(key); err != nil {
		return nil, ObjectInfo{}, err
	}
	s.mu.RLock()
	obj, ok := s.objects[key]
	s.mu.RUnlock()
	if !ok {
		return nil, ObjectInfo{}, ErrNotFound(key)
	}
	return io.NopCloser(bytes.NewReader(obj.data)), obj.info, nil
}

func (s *MemoryStore) Delete(_ context.Context, key string) error {
	key = normalizeKey(key)
	if err := validateKey(key); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.objects, key)
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Exists(_ context.Context, key string) (bool, error) {
	key = normalizeKey(key)
	if err := validateKey(key); err != nil {
		return false, err
	}
	s.mu.RLock()
	_, ok := s.objects[key]
	s.mu.RUnlock()
	return ok, nil
}

func (s *MemoryStore) List(_ context.Context, opts ListOptions) (ListResult, error) {
	prefix := normalizeKey(opts.Prefix)
	if prefix != "" {
		if err := validateKeyPrefix(prefix); err != nil {
			return ListResult{}, err
		}
	}
	s.mu.RLock()
	keys := make([]string, 0, len(s.objects))
	for k := range s.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	result := ListResult{}
	seen := map[string]bool{}
	for _, k := range keys {
		if opts.Marker != "" && k <= opts.Marker {
			continue
		}
		if opts.Delimiter != "" {
			rest := strings.TrimPrefix(k, prefix)
			if i := strings.Index(rest, opts.Delimiter); i >= 0 {
				cp := prefix + rest[:i+len(opts.Delimiter)]
				if !seen[cp] {
					seen[cp] = true
					result.CommonPrefixes = append(result.CommonPrefixes, cp)
				}
				continue
			}
		}
		if opts.Limit > 0 && len(result.Objects) >= opts.Limit {
			result.Truncated = true
			result.NextMarker = result.Objects[len(result.Objects)-1].Key
			break
		}
		result.Objects = append(result.Objects, s.objects[k].info)
	}
	s.mu.RUnlock()
	return result, nil
}

func (s *MemoryStore) PublicURL(_ context.Context, key string, _ URLConfig) (string, error) {
	key = normalizeKey(key)
	if err := validateKey(key); err != nil {
		return "", err
	}
	return "memory:///" + key, nil
}

func (s *MemoryStore) SignedURL(ctx context.Context, key string, _ time.Duration, opts URLConfig) (string, error) {
	return s.PublicURL(ctx, key, opts)
}

func (s *MemoryStore) Copy(ctx context.Context, srcKey, dstKey string) (ObjectInfo, error) {
	rc, info, err := s.Get(ctx, srcKey)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer func() { _ = rc.Close() }()
	return s.Put(ctx, dstKey, rc, PutOptions{Visibility: info.Visibility, ContentType: info.ContentType, Metadata: info.Metadata})
}

func (s *MemoryStore) Close() error { return nil }

func copyMetadata(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
