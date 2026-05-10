package routing

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

// ErrSnapshotTimeout is returned when an agent does not answer a
// SnapshotRequest within the configured timeout.
var ErrSnapshotTimeout = errors.New("admin server: snapshot request timed out")

// ErrSnapshotInflightOverflow is returned when too many simultaneous
// snapshot requests are pending. This guards against a misbehaving UI
// flooding the request channel.
var ErrSnapshotInflightOverflow = errors.New("admin server: too many in-flight snapshot requests")

// SnapshotRouter correlates server-issued SnapshotRequest frames with
// the SnapshotResponse frames the agent sends back over the same bidi
// stream. Identifiers are short opaque strings allocated at request time.
type SnapshotRouter struct {
	mu       sync.Mutex
	pending  map[string]chan *adminv1.SnapshotResponse
	maxAlive int
	nextID   atomic.Uint64
}

// NewSnapshotRouter constructs a router. maxAlive caps the total number
// of pending requests across all agents; values <= 0 default to 256.
func NewSnapshotRouter(maxAlive int) *SnapshotRouter {
	if maxAlive <= 0 {
		maxAlive = 256
	}
	return &SnapshotRouter{
		pending:  make(map[string]chan *adminv1.SnapshotResponse),
		maxAlive: maxAlive,
	}
}

// Begin allocates a fresh request_id and registers a channel the caller
// will block on. The returned cancel MUST be called even on success
// (after consuming the response) to release the slot.
func (r *SnapshotRouter) Begin() (id string, ch chan *adminv1.SnapshotResponse, cancel func(), err error) {
	r.mu.Lock()
	if len(r.pending) >= r.maxAlive {
		r.mu.Unlock()
		return "", nil, nil, ErrSnapshotInflightOverflow
	}
	id = newRequestID(r.nextID.Add(1))
	ch = make(chan *adminv1.SnapshotResponse, 1)
	r.pending[id] = ch
	r.mu.Unlock()

	cancel = func() {
		r.mu.Lock()
		if existing, ok := r.pending[id]; ok && existing == ch {
			delete(r.pending, id)
		}
		r.mu.Unlock()
	}
	return id, ch, cancel, nil
}

// Resolve delivers a SnapshotResponse to the awaiting Begin caller (if
// any). Returns true when a pending request consumed the response.
func (r *SnapshotRouter) Resolve(resp *adminv1.SnapshotResponse) bool {
	if resp == nil || resp.RequestId == "" {
		return false
	}
	r.mu.Lock()
	ch, ok := r.pending[resp.RequestId]
	if ok {
		delete(r.pending, resp.RequestId)
	}
	r.mu.Unlock()

	if !ok {
		return false
	}
	select {
	case ch <- resp:
		return true
	default:
		return false
	}
}

// PendingCount is intended for tests / metrics.
func (r *SnapshotRouter) PendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}

// Wait blocks for a response on ch up to the deadline. Cancel is the
// cleanup func from Begin (always run, even on success).
func Wait(ch <-chan *adminv1.SnapshotResponse, cancel func(), timeout time.Duration) (*adminv1.SnapshotResponse, error) {
	defer cancel()
	if timeout <= 0 {
		select {
		case resp := <-ch:
			return resp, nil
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
		return nil, ErrSnapshotTimeout
	}
}

func newRequestID(seq uint64) string {
	// Deterministic but unique per call. The agent never inspects the
	// shape; we just need uniqueness within a server lifetime.
	const hex = "0123456789abcdef"
	b := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		b[i] = hex[seq&0xf]
		seq >>= 4
	}
	return "snap-" + string(b)
}
