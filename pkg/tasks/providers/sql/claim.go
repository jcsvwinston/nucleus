// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Claim takes up to limit jobs for owner, across the given queues in the order
// they are listed — which is how priority is expressed: the first queue is
// served before the second.
//
// Two kinds of row are claimable, and the second is what makes the queue
// durable across a crash:
//
//   - pending, and due;
//   - RUNNING with an EXPIRED lease — claimed by a process that never came
//     back. Without this a worker that dies mid-job strands it for ever, which
//     is precisely the defect the outbox carried until NU-84.
//
// The claim is a SELECT of candidates followed by a conditional UPDATE per
// row, and the UPDATE is what arbitrates: whoever's update affects the row
// owns it, and the loser sees zero rows affected and moves on. It is the
// mechanism pkg/outbox uses, and it works on all three engines — including
// SQLite, which has no SKIP LOCKED. Reaching for SELECT ... FOR UPDATE SKIP
// LOCKED would be faster under contention on Postgres and MySQL 8, and it is
// deliberately NOT done here: it would be a second code path, only exercised
// on two of the three engines, in the session that introduces the queue.
func (s *Store) Claim(ctx context.Context, owner string, queues []string, limit int, lease time.Duration, now time.Time) ([]Job, error) {
	if limit <= 0 {
		return nil, nil
	}
	if len(queues) == 0 {
		queues = []string{"default"}
	}
	now = now.UTC()

	var claimed []Job
	for _, queue := range queues {
		if len(claimed) >= limit {
			break
		}
		batch, err := s.claimFromQueue(ctx, owner, queue, limit-len(claimed), lease, now)
		if err != nil {
			return claimed, err
		}
		claimed = append(claimed, batch...)
	}
	return claimed, nil
}

func (s *Store) claimFromQueue(ctx context.Context, owner, queue string, limit int, lease time.Duration, now time.Time) ([]Job, error) {
	selectSQL := s.rebind(fmt.Sprintf(
		`SELECT id, queue, task_type, payload, status, attempts, max_attempts,
			timeout_ms, backoff_base_ms, backoff_max_ms, available_at, created_at, last_error
		FROM %s
		WHERE queue = ? AND available_at <= ?
		  AND (
		        status = ?
		     OR (status = ? AND lease_until IS NOT NULL AND lease_until <= ?)
		      )
		ORDER BY available_at ASC, created_at ASC
		LIMIT ?`, s.quotedTable()))

	rows, err := s.db.QueryContext(ctx, selectSQL,
		queue, now, string(StatusPending), string(StatusRunning), now, limit)
	if err != nil {
		return nil, fmt.Errorf("sqlprovider: select candidates: %w", err)
	}
	candidates := make([]Job, 0, limit)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		candidates = append(candidates, job)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("sqlprovider: scan candidates: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	claimed := make([]Job, 0, len(candidates))
	for _, job := range candidates {
		ok, err := s.tryClaim(ctx, &job, owner, lease, now)
		if err != nil {
			return claimed, err
		}
		if ok {
			claimed = append(claimed, job)
		}
	}
	return claimed, nil
}

// tryClaim is the arbiter. Its WHERE repeats the candidate predicate, so a row
// another worker took between the select and here fails the update and is
// skipped.
func (s *Store) tryClaim(ctx context.Context, job *Job, owner string, lease time.Duration, now time.Time) (bool, error) {
	query := s.rebind(fmt.Sprintf(
		`UPDATE %s
		SET status = ?, lease_owner = ?, lease_until = ?, attempts = attempts + 1
		WHERE id = ? AND available_at <= ?
		  AND (
		        status = ?
		     OR (status = ? AND lease_until IS NOT NULL AND lease_until <= ?)
		      )`, s.quotedTable()))
	res, err := s.db.ExecContext(ctx, query,
		string(StatusRunning), owner, now.Add(lease),
		job.ID, now, string(StatusPending), string(StatusRunning), now)
	if err != nil {
		return false, fmt.Errorf("sqlprovider: claim %s: %w", job.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("sqlprovider: claim rows %s: %w", job.ID, err)
	}
	if n == 0 {
		return false, nil
	}
	job.Status = StatusRunning
	job.Attempts++
	return true, nil
}

// Heartbeat extends the lease of the jobs a worker still holds, so a handler
// that legitimately takes longer than one lease is not reclaimed underneath
// itself while it runs.
func (s *Store) Heartbeat(ctx context.Context, owner string, ids []string, lease time.Duration, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+3)
	args = append(args, now.UTC().Add(lease), owner)
	for i := range ids {
		placeholders[i] = "?"
	}
	for _, id := range ids {
		args = append(args, id)
	}
	query := s.rebind(fmt.Sprintf(
		`UPDATE %s SET lease_until = ? WHERE lease_owner = ? AND status = '%s' AND id IN (%s)`,
		s.quotedTable(), StatusRunning, joinComma(placeholders)))
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("sqlprovider: heartbeat: %w", err)
	}
	return nil
}

// Succeed marks a job done.
// Succeed marks a job done. Fenced by owner: a worker whose lease expired
// while it ran has already had the job taken over, and writing the outcome
// anyway would overwrite the state of whoever holds it now — the classic way a
// finished job comes back from the dead.
func (s *Store) Succeed(ctx context.Context, owner, id string, now time.Time) (bool, error) {
	query := s.rebind(fmt.Sprintf(
		`UPDATE %s SET status = ?, finished_at = ?, lease_owner = NULL, lease_until = NULL, unique_key = NULL
		WHERE id = ? AND lease_owner = ?`, s.quotedTable()))
	res, err := s.db.ExecContext(ctx, query, string(StatusDone), now.UTC(), id, owner)
	if err != nil {
		return false, fmt.Errorf("sqlprovider: mark done %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return true, nil
	}
	return n > 0, nil
}

// Retry puts a failed job back with its next attempt due later, keeping the
// reason. A job that has spent its attempts goes to the dead letter instead.
func (s *Store) Retry(ctx context.Context, owner string, job Job, cause error, now time.Time) (dead bool, err error) {
	reason := ""
	if cause != nil {
		reason = cause.Error()
	}
	if job.Attempts >= job.MaxAttempts {
		query := s.rebind(fmt.Sprintf(
			`UPDATE %s SET status = ?, finished_at = ?, last_error = ?, lease_owner = NULL, lease_until = NULL, unique_key = NULL
			WHERE id = ? AND lease_owner = ?`, s.quotedTable()))
		if _, err := s.db.ExecContext(ctx, query, string(StatusDead), now.UTC(), reason, job.ID, owner); err != nil {
			return false, fmt.Errorf("sqlprovider: mark dead %s: %w", job.ID, err)
		}
		return true, nil
	}
	next := now.UTC().Add(backoff(job, job.Attempts))
	query := s.rebind(fmt.Sprintf(
		`UPDATE %s SET status = ?, available_at = ?, last_error = ?, lease_owner = NULL, lease_until = NULL
		WHERE id = ? AND lease_owner = ?`, s.quotedTable()))
	if _, err := s.db.ExecContext(ctx, query, string(StatusPending), next, reason, job.ID, owner); err != nil {
		return false, fmt.Errorf("sqlprovider: schedule retry %s: %w", job.ID, err)
	}
	return false, nil
}

// Release puts a job back as pending and GIVES BACK the attempt the claim
// spent. The claim charges one up front so a worker that dies still moves the
// job towards its budget; a job released without having run never got its
// chance, and charging it would retire work that was never attempted — a type
// this replica has no handler for would burn three attempts in three seconds
// and die without executing once.
//
// It is fenced by owner: only the worker that holds the lease releases it, so
// a straggler cannot put back a job another worker has since claimed.
func (s *Store) Release(ctx context.Context, owner string, ids []string, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+3)
	args = append(args, string(StatusPending), now.UTC(), owner)
	for i := range ids {
		placeholders[i] = "?"
	}
	for _, id := range ids {
		args = append(args, id)
	}
	query := s.rebind(fmt.Sprintf(
		`UPDATE %s
		SET status = ?, available_at = ?,
		    attempts = CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END,
		    lease_owner = NULL, lease_until = NULL
		WHERE lease_owner = ? AND id IN (%s)`, s.quotedTable(), joinComma(placeholders)))
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("sqlprovider: release: %w", err)
	}
	return nil
}

// PurgeFinished removes the jobs that finished longer ago than keep.
//
// Without it the table only grows, in a system that is working perfectly: a
// queue doing a million jobs a day keeps a million rows a day, and the index
// the claim depends on gets slower every week. Retention is the thing every
// durable queue needs and nobody remembers until the disk fills.
//
// Dead jobs are NEVER purged here, whatever the retention: they are the ones
// somebody may still want to requeue, and purge-archived is the deliberate act
// that removes them.
func (s *Store) PurgeFinished(ctx context.Context, keep time.Duration, now time.Time) (int, error) {
	if keep <= 0 {
		return 0, nil
	}
	cutoff := now.UTC().Add(-keep)
	query := s.rebind(fmt.Sprintf(
		`DELETE FROM %s WHERE status = ? AND finished_at IS NOT NULL AND finished_at <= ?`,
		s.quotedTable()))
	res, err := s.db.ExecContext(ctx, query, string(StatusDone), cutoff)
	if err != nil {
		return 0, fmt.Errorf("sqlprovider: purge finished jobs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return int(n), nil
}

// ReapAbandoned retires the jobs whose owner never came back AND that have
// spent their attempts. The manager runs it on a timer, NOT on every claim:
// it is a write against every expired-lease row, and one per worker per poll
// would be permanent load on a table that is mostly idle.
//
// ReapAbandoned retires the jobs whose owner never came back AND that have
// spent their attempts. It is the bound on the rescue the claim performs:
// without it, a job that takes its process down every time it is picked up is
// reclaimed for ever, because a handler that never returns never reaches the
// attempts check.
func (s *Store) ReapAbandoned(ctx context.Context, now time.Time) (int, error) {
	query := s.rebind(fmt.Sprintf(
		`UPDATE %s SET status = ?, finished_at = ?, last_error = ?, lease_owner = NULL, lease_until = NULL, unique_key = NULL
		WHERE status = ? AND lease_until IS NOT NULL AND lease_until <= ? AND attempts >= max_attempts`,
		s.quotedTable()))
	res, err := s.db.ExecContext(ctx, query,
		string(StatusDead), now.UTC(), "abandoned by its worker and out of attempts",
		string(StatusRunning), now.UTC())
	if err != nil {
		return 0, fmt.Errorf("sqlprovider: reap abandoned: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return int(n), nil
}

// backoff is the wait before the next attempt: the job's own curve when it
// carries one, and otherwise the provider's default of 1s doubling to a
// minute. The curve travels WITH the job — an application that wants a
// different one sets it when enqueueing, rather than taking whatever the
// provider hard-codes.
func backoff(job Job, attempt int) time.Duration {
	base := job.BackoffBase
	if base <= 0 {
		base = time.Second
	}
	max := job.BackoffMax
	if max <= 0 {
		max = time.Minute
	}
	d := base << uint(max2(attempt-1, 0))
	if d <= 0 || d > max {
		return max
	}
	return d
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(sc rowScanner) (Job, error) {
	var (
		job                      Job
		status                   string
		timeoutMS, baseMS, maxMS int64
		availableAt, createdAt   any
		lastError                sql.NullString
		payload                  string
	)
	if err := sc.Scan(&job.ID, &job.Queue, &job.TaskType, &payload, &status,
		&job.Attempts, &job.MaxAttempts, &timeoutMS, &baseMS, &maxMS,
		&availableAt, &createdAt, &lastError); err != nil {
		return Job{}, fmt.Errorf("sqlprovider: scan job: %w", err)
	}
	job.Payload = []byte(payload)
	job.Status = Status(status)
	job.Timeout = time.Duration(timeoutMS) * time.Millisecond
	job.BackoffBase = time.Duration(baseMS) * time.Millisecond
	job.BackoffMax = time.Duration(maxMS) * time.Millisecond
	job.LastError = lastError.String
	var err error
	if job.AvailableAt, err = parseTimeValue(availableAt); err != nil {
		return Job{}, err
	}
	if job.CreatedAt, err = parseTimeValue(createdAt); err != nil {
		return Job{}, err
	}
	return job, nil
}

// parseTimeValue copes with the drivers that hand back a string instead of a
// time.Time. Every layout here is UTC because every write is UTC.
func parseTimeValue(raw any) (time.Time, error) {
	switch v := raw.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return v.UTC(), nil
	case []byte:
		return parseTimeString(string(v))
	case string:
		return parseTimeString(v)
	default:
		return time.Time{}, fmt.Errorf("sqlprovider: unexpected time value %T", raw)
	}
}

func parseTimeString(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("sqlprovider: cannot parse time %q", raw)
}
