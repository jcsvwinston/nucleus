// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package memoryprovider

import (
	"context"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// holdReason says why a job is being kept instead of run.
type holdReason string

const (
	// reasonNoHandler: no worker in THIS process registered the job's type.
	// It is not a failure of the job — it is a consumer that has not arrived,
	// which is the shape of a producer deployed ahead of its worker, or of a
	// typo. The job waits for a handler instead of being discarded.
	reasonNoHandler holdReason = "no_handler"
	// reasonRetriesExhausted: the handler ran and kept failing until the
	// policy's MaxRetry was spent.
	reasonRetriesExhausted holdReason = "retries_exhausted"
	// reasonShutdown: the process was cancelled while the job waited between
	// retries. It had attempts left; it never got them.
	reasonShutdown holdReason = "shutdown"
	// reasonQueueClosed: the job was still in the channel when the manager
	// stopped. It was accepted and never started.
	reasonQueueClosed holdReason = "queue_closed"
)

// holdKind is which of the two stores a job is kept in. They are separate on
// purpose: a mistyped task type must not be able to evict the jobs that
// actually died, and an operator emptying the dead letter must not throw away
// work that is only waiting for its consumer.
type holdKind int

const (
	// kindWaiting holds jobs whose type has no handler yet.
	kindWaiting holdKind = iota
	// kindDead holds jobs that ran out of retries, or that the process
	// stopped before it could finish with them.
	kindDead
)

// defaultHoldCapacity is how many jobs each store keeps before the oldest is
// evicted. Bounded on purpose: an unbounded store turns a leak of jobs into a
// leak of the process's memory. asynq, the reference this provider's snapshot
// vocabulary comes from, bounds its archive the same way (10 000 per queue,
// 90 days); an in-process store that dies with the process holds less.
const defaultHoldCapacity = 1000

// heldJob is a job kept out of the run path, with what it takes to run it
// later: the payload, the policy it was enqueued under, and the context it
// was enqueued with.
//
// The context is kept with context.WithoutCancel: this framework carries the
// request scope — site, tenant, database alias (pkg/app/requestscope.go) — in
// context VALUES, so dropping the context for a background one would quietly
// re-run the job against the wrong tenant's keyspace. Cancellation is what
// must not survive: the request that enqueued the job is long gone by the time
// anybody requeues it.
type heldJob struct {
	id       string
	task     *Task
	policy   tasks.EnqueuePolicy
	ctx      context.Context
	reason   holdReason
	counted  bool
	attempts int
	lastErr  string
	heldAt   time.Time
}

// holdStore keeps jobs the provider would otherwise drop.
//
// It has a mutex of its own: the manager's RWMutex guards the handler map on
// the hot lookup path, and holding it here would serialize dispatch behind
// bookkeeping.
type holdStore struct {
	mu       sync.Mutex
	capacity int
	jobs     map[holdKind][]heldJob
	evicted  map[holdKind]int64
}

func newHoldStore(capacity int) *holdStore {
	if capacity <= 0 {
		capacity = defaultHoldCapacity
	}
	return &holdStore{
		capacity: capacity,
		jobs:     map[holdKind][]heldJob{kindWaiting: nil, kindDead: nil},
		evicted:  map[holdKind]int64{},
	}
}

// hold keeps one job. It returns the job it had to evict to make room, if any,
// so the caller can say so in the log rather than lose it silently twice.
func (s *holdStore) hold(kind holdKind, job heldJob) (evicted *heldJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.jobs[kind]
	if len(list) >= s.capacity {
		oldest := list[0]
		list = list[1:]
		s.evicted[kind]++
		evicted = &oldest
	}
	s.jobs[kind] = append(list, job)
	return evicted
}

// takeByType removes and returns every waiting job of one task type. It is the
// atomic half of "a handler arrived": whoever takes a job owns it, so two
// callers racing to release the same type cannot both dispatch it.
func (s *holdStore) takeByType(kind holdKind, taskType string) []heldJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.jobs[kind]
	if len(list) == 0 {
		return nil
	}
	taken := make([]heldJob, 0, len(list))
	kept := list[:0]
	for _, job := range list {
		if job.task.Type() == taskType {
			taken = append(taken, job)
			continue
		}
		kept = append(kept, job)
	}
	// Clear the tail of the reused array: those slots still reference the
	// taken jobs' payloads and contexts, which would stay alive for as long
	// as the store does.
	for i := len(kept); i < len(list); i++ {
		list[i] = heldJob{}
	}
	s.jobs[kind] = kept
	if len(taken) == 0 {
		return nil
	}
	return taken
}

// takeAll removes everything in the given stores and returns it PER STORE, so
// a caller that has to put jobs back can return each one where it came from.
// Merging them would turn a job that is only waiting for its handler into a
// dead one — and the next purge would delete it.
func (s *holdStore) takeAll(kinds ...holdKind) map[holdKind][]heldJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	taken := map[holdKind][]heldJob{}
	for _, kind := range kinds {
		if len(s.jobs[kind]) == 0 {
			continue
		}
		taken[kind] = s.jobs[kind]
		s.jobs[kind] = nil
	}
	return taken
}

// putBack returns jobs to a store when a requeue could not be completed. A job
// that could not be handed back to the queue stays held — the alternative is
// losing it at the exact point this store exists to cover.
//
// They go to the FRONT (they are the oldest: they were held before the ones
// that arrived meanwhile) and the overflow is dropped from the END, which is
// the same "oldest survives, newest is evicted" rule hold() applies. Truncating
// the other way round would have destroyed the jobs already in the store to
// make room for the returning ones.
func (s *holdStore) putBack(kind holdKind, jobs []heldJob) (evicted int) {
	if len(jobs) == 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	merged := make([]heldJob, 0, len(jobs)+len(s.jobs[kind]))
	merged = append(merged, jobs...)
	merged = append(merged, s.jobs[kind]...)
	if overflow := len(merged) - s.capacity; overflow > 0 {
		merged = merged[:s.capacity]
		s.evicted[kind] += int64(overflow)
		evicted = overflow
	}
	s.jobs[kind] = merged
	return evicted
}

// purge drops everything in one store and reports how many went.
func (s *holdStore) purge(kind holdKind) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.jobs[kind])
	s.jobs[kind] = nil
	return n
}

// count reports how many jobs are held in the given stores.
func (s *holdStore) count(kinds ...holdKind) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, kind := range kinds {
		n += len(s.jobs[kind])
	}
	return n
}

// evictedCount reports how many jobs were pushed out by the capacity bound.
func (s *holdStore) evictedCount(kinds ...holdKind) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, kind := range kinds {
		n += s.evicted[kind]
	}
	return n
}
