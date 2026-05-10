// Package nodes is the in-memory registry of agents currently connected
// to the admin server. It tracks NodeRegistration metadata, the live
// frame send channel, and the timestamps used for inactivity expiry.
//
// Registry concurrency model:
//
//   - The agent service handler creates an Entry on stream open and
//     removes it on stream close. The handler owns the entry; nothing
//     outside ever closes Send.
//   - Other components (ControlService, UI) read the registry through
//     read-only methods: List, Lookup. Mutations are funneled through
//     Add / Remove.
//   - The frame send channel is the only path from the server to an
//     agent. The agent service's writer goroutine drains it and writes
//     to the bidi stream. Producers MUST use TryEnqueue (non-blocking).
package nodes

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

// Entry is a single connected agent.
type Entry struct {
	// NodeID is the stable identifier the agent reported. Used as the
	// registry key.
	NodeID string

	// Info captures NodeRegistration fields plus runtime stats. Never
	// mutate these fields directly; go through Touch / UpdateInfo on the
	// Registry instead so the Connected/LastSeenAt counters update
	// consistently.
	Info NodeInfo

	// Send is the per-agent frame queue the server writes to. The agent
	// stream's writer goroutine drains it in order. Buffer size is
	// configured at registry construction time.
	Send chan *adminv1.Frame

	// CtxDone fires when the agent stream handler is exiting (server-
	// initiated close, server shutdown, or client disconnect). Producers
	// of frames check this before blocking on Send.
	CtxDone <-chan struct{}

	// closeOnce protects the cleanup path.
	closeOnce sync.Once
}

// NodeInfo is the snapshot the UI sees via ControlService.ListNodes.
type NodeInfo struct {
	NodeID           string
	Version          string
	Labels           map[string]string
	StartedAt        time.Time
	LastSeenAt       time.Time
	Connected        bool
	RegisteredModels []string
}

// Registry maintains the live set of connected agents.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*Entry

	// Watchers receive node-change notifications (connected / disconnected).
	// The slice is rebuilt copy-on-write, so reads are lock-free at the
	// fan-out site.
	watchersMu sync.RWMutex
	watchers   []chan<- NodeChange
}

// NodeChange is published whenever an agent connects or disconnects, or
// on inactivity timeout.
type NodeChange struct {
	NodeID    string
	Connected bool
	Info      NodeInfo
}

// New constructs a Registry.
func New() *Registry {
	return &Registry{
		entries: make(map[string]*Entry),
	}
}

// Add registers a new agent. Returns the entry plus a deregister function
// the caller (the AgentService handler) MUST call when its stream ends.
//
// The cleanup function is idempotent.
func (r *Registry) Add(ctx context.Context, info NodeInfo, sendBuffer int) (*Entry, func()) {
	if sendBuffer <= 0 {
		sendBuffer = 64
	}
	info.NodeID = strings.TrimSpace(info.NodeID)
	if info.NodeID == "" {
		// Defensive: refuse to register a nameless agent. The caller
		// should validate before calling Add.
		dummyDone := make(chan struct{})
		close(dummyDone)
		return &Entry{Send: make(chan *adminv1.Frame, 1), CtxDone: dummyDone}, func() {}
	}
	info.LastSeenAt = time.Now().UTC()
	info.Connected = true

	e := &Entry{
		NodeID:  info.NodeID,
		Info:    info,
		Send:    make(chan *adminv1.Frame, sendBuffer),
		CtxDone: ctx.Done(),
	}

	r.mu.Lock()
	// If a previous entry exists (agent reconnected before its old entry
	// was cleaned up), evict it. The old handler will detect ctx done on
	// its next loop iteration.
	if old, ok := r.entries[info.NodeID]; ok {
		old.closeOnce.Do(func() {})
	}
	r.entries[info.NodeID] = e
	r.mu.Unlock()

	r.publish(NodeChange{NodeID: info.NodeID, Connected: true, Info: info})

	return e, func() { r.remove(info.NodeID, e) }
}

func (r *Registry) remove(nodeID string, owner *Entry) {
	r.mu.Lock()
	current, ok := r.entries[nodeID]
	if ok && current == owner {
		delete(r.entries, nodeID)
	}
	r.mu.Unlock()

	if !ok {
		return
	}

	owner.closeOnce.Do(func() {})
	info := owner.Info
	info.Connected = false
	info.LastSeenAt = time.Now().UTC()
	r.publish(NodeChange{NodeID: nodeID, Connected: false, Info: info})
}

// Touch updates the last-seen timestamp on every event/heartbeat the
// AgentService handler receives. Idempotent.
func (r *Registry) Touch(nodeID string, at time.Time) {
	r.mu.Lock()
	if e, ok := r.entries[nodeID]; ok {
		e.Info.LastSeenAt = at
	}
	r.mu.Unlock()
}

// List returns a stable snapshot of every registered node, sorted by
// NodeID.
func (r *Registry) List() []NodeInfo {
	r.mu.RLock()
	out := make([]NodeInfo, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.Info)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

// Lookup returns the entry for nodeID, or (nil, false). The Entry's
// fields (especially Info) may be racy with concurrent Touch / UpdateInfo
// calls; consume them only as a snapshot.
func (r *Registry) Lookup(nodeID string) (*Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[nodeID]
	return e, ok
}

// ForEach calls fn with every live entry. fn MUST NOT block on the
// entry's Send channel; it is meant for fan-out enumeration only. The
// registry holds the read lock during iteration.
func (r *Registry) ForEach(fn func(*Entry)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		fn(e)
	}
}

// TryEnqueue pushes f onto the entry's Send channel without blocking.
// Returns false if the channel is full or the entry has been closed.
func TryEnqueue(e *Entry, f *adminv1.Frame) bool {
	if e == nil || f == nil {
		return false
	}
	select {
	case <-e.CtxDone:
		return false
	default:
	}
	select {
	case e.Send <- f:
		return true
	case <-e.CtxDone:
		return false
	default:
		return false
	}
}

// Watch subscribes to node-change notifications. The returned channel
// receives NodeChange values; the cancel function MUST be called to
// release resources.
//
// The channel is unbuffered: if a watcher does not drain promptly, the
// publisher will skip notifications for that watcher (we never block the
// fan-out path on a slow watcher).
func (r *Registry) Watch() (<-chan NodeChange, func()) {
	ch := make(chan NodeChange, 16)

	r.watchersMu.Lock()
	r.watchers = append(r.watchers, ch)
	r.watchersMu.Unlock()

	cancel := func() {
		r.watchersMu.Lock()
		defer r.watchersMu.Unlock()
		for i, w := range r.watchers {
			if w == ch {
				r.watchers = append(r.watchers[:i], r.watchers[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, cancel
}

func (r *Registry) publish(change NodeChange) {
	r.watchersMu.RLock()
	watchers := make([]chan<- NodeChange, len(r.watchers))
	copy(watchers, r.watchers)
	r.watchersMu.RUnlock()

	for _, ch := range watchers {
		select {
		case ch <- change:
		default:
			// drop silently — slow watcher
		}
	}
}

// AnyWithModel returns any connected entry that registered the named
// model. Returns (nil, false) when no such agent is connected.
//
// The implementation is deliberately first-match (no scoring): models
// are typically homogeneous across the fleet, and any agent that has
// the model can answer.
func (r *Registry) AnyWithModel(modelName string) (*Entry, bool) {
	if r == nil || strings.TrimSpace(modelName) == "" {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		for _, m := range e.Info.RegisteredModels {
			if strings.EqualFold(m, modelName) {
				return e, true
			}
		}
	}
	return nil, false
}

// AggregateModels returns the union of every connected agent's
// RegisteredModels. The list is sorted alphabetically and de-duplicated
// case-insensitively so the UI gets a stable view regardless of how
// many agents reported it.
func (r *Registry) AggregateModels() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]string{}
	for _, e := range r.entries {
		for _, m := range e.Info.RegisteredModels {
			key := strings.ToLower(strings.TrimSpace(m))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; !ok {
				seen[key] = m
			}
		}
	}
	out := make([]string, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// Inactivity inspects every entry and returns those whose LastSeenAt is
// older than `now - timeout`. The caller decides whether to evict them.
func (r *Registry) Inactivity(now time.Time, timeout time.Duration) []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NodeInfo, 0)
	for _, e := range r.entries {
		if !e.Info.LastSeenAt.IsZero() && now.Sub(e.Info.LastSeenAt) > timeout {
			out = append(out, e.Info)
		}
	}
	return out
}
