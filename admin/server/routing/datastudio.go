package routing

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
)

// ErrDataStudioTimeout is returned when an agent does not answer a
// DataStudioRequest within the configured timeout.
var ErrDataStudioTimeout = errors.New("admin server: data studio request timed out")

// ErrDataStudioInflightOverflow is returned when too many simultaneous
// data studio requests are pending.
var ErrDataStudioInflightOverflow = errors.New("admin server: too many in-flight data studio requests")

// DataStudioRouter correlates server-issued DataStudioRequest frames
// with the DataStudioResponse frames the agent sends back over the
// same bidi stream. Mirrors the SnapshotRouter pattern.
type DataStudioRouter struct {
	mu       sync.Mutex
	pending  map[string]chan *adminv1.DataStudioResponse
	maxAlive int
	nextID   atomic.Uint64
}

// NewDataStudioRouter constructs a router. maxAlive caps the total
// number of pending requests across all agents; values <= 0 default to
// 256.
func NewDataStudioRouter(maxAlive int) *DataStudioRouter {
	if maxAlive <= 0 {
		maxAlive = 256
	}
	return &DataStudioRouter{
		pending:  make(map[string]chan *adminv1.DataStudioResponse),
		maxAlive: maxAlive,
	}
}

// Begin allocates a fresh request_id and registers a channel. The
// returned cancel MUST be called even on success to release the slot.
func (r *DataStudioRouter) Begin() (id string, ch chan *adminv1.DataStudioResponse, cancel func(), err error) {
	r.mu.Lock()
	if len(r.pending) >= r.maxAlive {
		r.mu.Unlock()
		return "", nil, nil, ErrDataStudioInflightOverflow
	}
	id = newDataStudioRequestID(r.nextID.Add(1))
	ch = make(chan *adminv1.DataStudioResponse, 1)
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

// Resolve delivers a DataStudioResponse to the awaiting Begin caller
// (if any). Returns true when a pending request consumed the response.
func (r *DataStudioRouter) Resolve(resp *adminv1.DataStudioResponse) bool {
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
func (r *DataStudioRouter) PendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}

// WaitDataStudio blocks for a response on ch up to the deadline.
// Cancel is the cleanup func from Begin (always run, even on success).
func WaitDataStudio(ch <-chan *adminv1.DataStudioResponse, cancel func(), timeout time.Duration) (*adminv1.DataStudioResponse, error) {
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
		return nil, ErrDataStudioTimeout
	}
}

func newDataStudioRequestID(seq uint64) string {
	const hex = "0123456789abcdef"
	b := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		b[i] = hex[seq&0xf]
		seq >>= 4
	}
	return "ds-" + string(b)
}
