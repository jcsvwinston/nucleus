// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package memoryprovider

import (
	"errors"
	"fmt"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// ErrUnsupportedQueueAction reports a queue action this provider cannot
// perform. The wording matters beyond this package: Orbit's panel classifies
// a queue-action error by substring and answers 400 for one that says
// "unsupported", rather than a 500 that reads like the provider broke.
var ErrUnsupportedQueueAction = errors.New("memoryprovider: unsupported queue action")

// ErrManagerStopped reports an operation asked of a manager that is no longer
// running. Requeueing onto a stopped manager would move held jobs onto a
// channel nobody reads again — which looks like success and is a loss.
var ErrManagerStopped = errors.New("memoryprovider: manager is stopped")

type Inspector struct {
	manager *Manager
}

func NewInspector(manager *Manager) *Inspector {
	return &Inspector{manager: manager}
}

func (i *Inspector) InspectRuntime() tasks.RuntimeSnapshot {
	if i.manager == nil {
		return tasks.RuntimeSnapshot{Enabled: false, Reason: "nil manager"}
	}
	m := i.manager

	// Order matters. A job is held BEFORE its counter moves, so a reader that
	// reads the counters FIRST and the store second can never observe
	// "failed, and nothing held": whatever the counter already reflects is in
	// the store by the time the store is read. Reading the store first opens
	// exactly that window — held=0, then the writer holds and counts, then
	// failed=1 — and it is the window the bench probes land in.
	processed := int(m.processed.Load())
	failed := int(m.failed.Load())
	waiting := m.holds.count(kindWaiting)
	dead := m.holds.count(kindDead)
	held := waiting + dead

	snap := tasks.RuntimeSnapshot{
		Enabled:     true,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		// This provider keeps one pair of counters for the life of the
		// process, so "today" and "all time" are the same number and both are
		// published rather than leaving one at zero: a consumer that reads
		// total_processed_all — the panel does — would otherwise see a queue
		// that has never processed anything.
		TotalProcessed:    processed,
		TotalProcessedAll: processed,
		TotalFailed:       failed,
		TotalFailedAll:    failed,
		// Held jobs are reported as archived: it is the field this snapshot
		// already publishes for "kept, not running", and the vocabulary the
		// queue actions are named in. It covers BOTH stores — the dead and
		// the ones waiting for a handler — because both are jobs the queue is
		// holding and neither is running. purge-archived only empties the
		// dead ones, so this number can stay above zero after a purge; the
		// action's own message says how many were kept and why.
		TotalArchived: held,
		TotalQueues:   1,
	}
	// Pending and active are NOT reported: this provider does not track them
	// yet, and a zero would claim the queue is empty rather than admit it is
	// not counted. That is A7's S3, and the gap is recorded as NU-82.
	snap.Queues = []tasks.RuntimeQueueSnapshot{{
		Name:           "default",
		Archived:       held,
		ProcessedToday: processed,
		ProcessedAll:   processed,
		FailedToday:    failed,
		FailedAll:      failed,
	}}
	return snap
}

// OperateQueue performs the queue actions this provider can honour.
//
// Two of the six the API names mean something for an in-process queue and are
// implemented; the rest are refused by name rather than pretended. Refusing is
// a deliberate answer: an operator who presses "pause" and gets a 400 saying
// so is better served than one whose click does nothing.
func (i *Inspector) OperateQueue(queue, action string) (tasks.QueueActionResult, error) {
	if i.manager == nil {
		return tasks.QueueActionResult{}, errors.New("memoryprovider: queue operations require a manager")
	}
	normalized, ok := tasks.NormalizeQueueAction(action)
	if !ok {
		return tasks.QueueActionResult{}, fmt.Errorf("%w: %q is not a queue action", ErrUnsupportedQueueAction, action)
	}
	if q := queue; q != "" && q != "default" {
		// Worded through ErrUnsupportedQueueAction on purpose: Orbit reads the
		// error text, and ErrUnsupportedQueue says "are not supported", which
		// does not contain the substring the panel looks for — the operator
		// would get a 500 for what is plainly a bad request.
		return tasks.QueueActionResult{}, fmt.Errorf("%w: queue %q does not exist in this provider", ErrUnsupportedQueueAction, q)
	}

	result := tasks.QueueActionResult{
		Enabled:     true,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Queue:       "default",
		Action:      normalized,
	}

	switch normalized {
	case tasks.QueueActionRetryArchived:
		if i.manager.ctx.Err() != nil {
			return tasks.QueueActionResult{}, ErrManagerStopped
		}
		taken := i.manager.holds.takeAll(kindWaiting, kindDead)
		requeued, stillHeld := 0, 0
		// Each store is requeued and returned SEPARATELY. Merging them would
		// file a job that is only waiting for its handler under "dead", and
		// the next purge would delete it as though it had failed.
		for _, kind := range []holdKind{kindWaiting, kindDead} {
			jobs := taken[kind]
			if len(jobs) == 0 {
				continue
			}
			n, returned := i.manager.requeue(jobs)
			if evicted := i.manager.holds.putBack(kind, returned); evicted > 0 {
				result.Message = fmt.Sprintf("%d held job(s) were evicted putting them back", evicted)
			}
			requeued += n
			stillHeld += len(returned)
		}
		result.Applied = true
		result.Affected = requeued
		result.Message = fmt.Sprintf("requeued %d held job(s); %d still held", requeued, stillHeld)
		return result, nil

	case tasks.QueueActionPurgeArchived:
		// Only the dead are purged. Jobs waiting for a handler are not dead —
		// they are work whose consumer has not started yet, and emptying the
		// dead letter must not be a way to lose them by accident.
		purged := i.manager.holds.purge(kindDead)
		waiting := i.manager.holds.count(kindWaiting)
		result.Applied = true
		result.Affected = purged
		result.Message = fmt.Sprintf("purged %d dead job(s); %d job(s) waiting for a handler were kept", purged, waiting)
		return result, nil

	default:
		return tasks.QueueActionResult{}, fmt.Errorf(
			"%w: %q needs a broker-backed provider; this one supports %q and %q",
			ErrUnsupportedQueueAction, normalized,
			tasks.QueueActionRetryArchived, tasks.QueueActionPurgeArchived)
	}
}
