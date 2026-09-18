// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// ErrUnsupportedQueueAction reports an action this provider cannot perform.
// The wording carries "unsupported" because Orbit's panel classifies a queue
// action error by substring: without it a bad request reaches the operator as
// a 500.
var ErrUnsupportedQueueAction = errors.New("sqlprovider: unsupported queue action")

// Inspector answers what the queue holds, by reading the table.
type Inspector struct {
	store *Store
}

func NewInspector(store *Store) *Inspector { return &Inspector{store: store} }

// InspectRuntime counts the queue by status and by queue name.
//
// Unlike the in-process provider, these numbers are real: they come from the
// table, so they describe the whole deployment rather than one process.
func (i *Inspector) InspectRuntime() tasks.RuntimeSnapshot {
	if i == nil || i.store == nil {
		return tasks.RuntimeSnapshot{Enabled: false, Reason: "nil store"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := fmt.Sprintf(`SELECT queue, status, COUNT(*) FROM %s GROUP BY queue, status`, i.store.quotedTable())
	rows, err := i.store.db.QueryContext(ctx, query)
	if err != nil {
		return tasks.RuntimeSnapshot{Enabled: false, Reason: fmt.Sprintf("count jobs: %v", err)}
	}
	defer func() { _ = rows.Close() }()

	byQueue := map[string]*tasks.RuntimeQueueSnapshot{}
	snap := tasks.RuntimeSnapshot{
		Enabled:     true,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	for rows.Next() {
		var queue, status string
		var count int
		if err := rows.Scan(&queue, &status, &count); err != nil {
			return tasks.RuntimeSnapshot{Enabled: false, Reason: fmt.Sprintf("scan counts: %v", err)}
		}
		q, ok := byQueue[queue]
		if !ok {
			q = &tasks.RuntimeQueueSnapshot{Name: queue}
			byQueue[queue] = q
		}
		switch Status(status) {
		case StatusPending:
			q.Pending += count
			snap.TotalPending += count
		case StatusRunning:
			q.Active += count
			snap.TotalActive += count
		case StatusDone:
			q.Completed += count
			snap.TotalCompleted += count
			snap.TotalProcessed += count
			snap.TotalProcessedAll += count
		case StatusDead:
			q.Archived += count
			snap.TotalArchived += count
			snap.TotalFailed += count
			snap.TotalFailedAll += count
		}
		// Size is what the queue HOLDS — waiting, running and dead. Finished
		// jobs stay in the table until they are cleaned up, and counting them
		// here would tell an operator a drained queue has two hundred jobs in
		// it.
		if Status(status) != StatusDone {
			q.Size += count
			snap.TotalSize += count
		}
	}
	if err := rows.Err(); err != nil {
		return tasks.RuntimeSnapshot{Enabled: false, Reason: fmt.Sprintf("read counts: %v", err)}
	}
	for _, q := range byQueue {
		snap.Queues = append(snap.Queues, *q)
	}
	snap.TotalQueues = len(snap.Queues)
	return snap
}

// OperateQueue performs the actions this provider can honour: putting the dead
// back, and emptying them.
func (i *Inspector) OperateQueue(queue, action string) (tasks.QueueActionResult, error) {
	if i == nil || i.store == nil {
		return tasks.QueueActionResult{}, errors.New("sqlprovider: queue operations require a store")
	}
	normalized, ok := tasks.NormalizeQueueAction(action)
	if !ok {
		return tasks.QueueActionResult{}, fmt.Errorf("%w: %q is not a queue action", ErrUnsupportedQueueAction, action)
	}
	if queue == "" {
		queue = "default"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := tasks.QueueActionResult{
		Enabled:     true,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Queue:       queue,
		Action:      normalized,
	}

	switch normalized {
	case tasks.QueueActionRetryArchived:
		// Back to pending with a fresh budget: an operator putting a dead job
		// back means "try it again", and returning it with its attempts
		// already spent would send it straight back to the dead letter.
		query := i.store.rebind(fmt.Sprintf(
			`UPDATE %s SET status = ?, attempts = 0, available_at = ?, lease_owner = NULL, lease_until = NULL
			WHERE queue = ? AND status = ?`, i.store.quotedTable()))
		res, err := i.store.db.ExecContext(ctx, query,
			string(StatusPending), time.Now().UTC(), queue, string(StatusDead))
		if err != nil {
			return tasks.QueueActionResult{}, fmt.Errorf("sqlprovider: requeue dead jobs: %w", err)
		}
		n, _ := res.RowsAffected()
		result.Applied = true
		result.Affected = int(n)
		result.Message = fmt.Sprintf("requeued %d dead job(s) on %q", n, queue)
		return result, nil

	case tasks.QueueActionPurgeArchived:
		query := i.store.rebind(fmt.Sprintf(`DELETE FROM %s WHERE queue = ? AND status = ?`, i.store.quotedTable()))
		res, err := i.store.db.ExecContext(ctx, query, queue, string(StatusDead))
		if err != nil {
			return tasks.QueueActionResult{}, fmt.Errorf("sqlprovider: purge dead jobs: %w", err)
		}
		n, _ := res.RowsAffected()
		result.Applied = true
		result.Affected = int(n)
		result.Message = fmt.Sprintf("purged %d dead job(s) on %q", n, queue)
		return result, nil

	default:
		return tasks.QueueActionResult{}, fmt.Errorf(
			"%w: %q is unsupported by the sql provider, which supports %q and %q",
			ErrUnsupportedQueueAction, normalized,
			tasks.QueueActionRetryArchived, tasks.QueueActionPurgeArchived)
	}
}
