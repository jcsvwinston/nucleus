// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
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
// There are two ways to arbitrate between workers, chosen per engine when the
// store opens, and both keep the same contract: a row goes to exactly one
// claimer, and the claim charges one attempt.
//
//   - Where the engine has SELECT ... FOR UPDATE SKIP LOCKED (PostgreSQL,
//     MySQL 8, MariaDB 10.6 or later), the claim locks the rows it takes and every other
//     worker steps over them to the next ones. Workers never contend for the
//     same row, so adding workers adds throughput.
//   - Everywhere else — SQLite, and an older MySQL or MariaDB — the claim
//     selects candidates and then races a conditional UPDATE per row: whoever's
//     update affects the row owns it, and the loser sees zero rows affected and
//     moves on. It is the mechanism pkg/outbox uses and it is correct on every
//     engine, but every worker selects the same head of the queue, so under
//     contention most of them lose (NU-87: at 16 workers on PostgreSQL, nine
//     claims in ten came back empty).
func (s *Store) Claim(ctx context.Context, owner string, queues []string, limit int, lease time.Duration, now time.Time) ([]Job, error) {
	jobs, _, err := s.claim(ctx, owner, queues, limit, lease, now)
	return jobs, err
}

// claim is Claim, and also reports whether the claim LOST a race: there were
// due jobs, and other workers took every one of them first. The manager needs
// the difference. An empty queue is a reason to wait a poll interval; a lost
// race is not, because the work is there and the next attempt finds the next
// row. Only the portable path can lose — SKIP LOCKED steps over what another
// worker holds instead of colliding with it.
func (s *Store) claim(ctx context.Context, owner string, queues []string, limit int, lease time.Duration, now time.Time) ([]Job, bool, error) {
	if limit <= 0 {
		return nil, false, nil
	}
	if len(queues) == 0 {
		queues = []string{"default"}
	}
	now = now.UTC()

	var claimed []Job
	lost := false
	for _, queue := range queues {
		if len(claimed) >= limit {
			break
		}
		var (
			batch     []Job
			lostQueue bool
			err       error
		)
		switch {
		case s.skipLocked && s.flavor == FlavorPostgres:
			batch, err = s.claimSkipLockedPostgres(ctx, owner, queue, limit-len(claimed), lease, now)
		case s.skipLocked && s.flavor == FlavorMySQL:
			batch, err = s.claimSkipLockedMySQL(ctx, owner, queue, limit-len(claimed), lease, now)
		default:
			batch, lostQueue, err = s.claimFromQueue(ctx, owner, queue, limit-len(claimed), lease, now)
		}
		if err != nil {
			return claimed, false, err
		}
		claimed = append(claimed, batch...)
		lost = lost || lostQueue
	}
	if len(claimed) > 0 {
		lost = false
	}
	return claimed, lost, nil
}

// claimableColumns is what a claim reads back about each job it takes.
const claimableColumns = `id, queue, task_type, payload, status, attempts, max_attempts,
			timeout_ms, backoff_base_ms, backoff_max_ms, available_at, created_at, last_error`

// The SKIP LOCKED claims read the two kinds of claimable row separately, and
// that is what lets them stop at the first free row instead of sorting the
// whole backlog on every claim.
//
// One predicate covering both — pending, OR running with an expired lease —
// cannot be served in order by the claim index (queue, status, available_at):
// the engine has to collect every due row and sort them. PostgreSQL then sorts
// the full backlog for each claim, because the row lock sits between the sort
// and the limit (37 ms a claim at 200 000 pending jobs, spilling to disk), and
// MySQL does worse — InnoDB locks every row it reads for the sort, so the
// first claimer holds the whole queue and every other worker skips it.
// Measured on 2026-10-04 with one predicate: sixteen workers on MySQL 8.4
// drained 161 jobs/s against one worker's 150.
//
// Split, each half is a range of the index read in order. The jobs a dead
// worker left behind come first, so a backlog of new work can never keep them
// waiting; there are at most as many of them as there are workers, so looking
// costs a short index range. Within each half the order is available_at, the
// order the index holds; jobs due at the same microsecond come in the index's
// order rather than by created_at, which the index does not carry.

// postgresClaimSQL takes up to n jobs in ONE statement. The rescued CTE locks
// the expired leases first; the second sub-select fills what is left of the
// limit with pending jobs. Both sub-selects run exactly once — the CTE is read
// twice, which makes PostgreSQL materialise it, and the pending one is an
// ARRAY(...) InitPlan — because a sub-select with LIMIT that the planner is
// free to re-run can lock more rows than it returns.
//
// FOR UPDATE applies after the ORDER BY and the LIMIT and re-checks the
// predicate on the newest version of each row, so a row another worker claimed
// and committed in the meantime is skipped, not taken twice.
func (s *Store) postgresClaimSQL() string {
	return s.rebind(fmt.Sprintf(
		`WITH rescued AS (
			SELECT id FROM %[1]s
			WHERE queue = ? AND status = ? AND available_at <= ?
			  AND lease_until IS NOT NULL AND lease_until <= ?
			ORDER BY available_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
		UPDATE %[1]s
		SET status = ?, lease_owner = ?, lease_until = ?, attempts = attempts + 1
		WHERE id = ANY(ARRAY(SELECT id FROM rescued) || ARRAY(
			SELECT id FROM %[1]s
			WHERE queue = ? AND status = ? AND available_at <= ?
			ORDER BY available_at ASC
			LIMIT ? - (SELECT COUNT(*) FROM rescued)
			FOR UPDATE SKIP LOCKED
		))
		RETURNING %[2]s`, s.quotedTable(), claimableColumns))
}

func (s *Store) claimSkipLockedPostgres(ctx context.Context, owner, queue string, limit int, lease time.Duration, now time.Time) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, s.postgresClaimSQL(),
		queue, string(StatusRunning), now, now, limit,
		string(StatusRunning), owner, now.Add(lease),
		queue, string(StatusPending), now, limit)
	if err != nil {
		return nil, fmt.Errorf("sqlprovider: claim from %s: %w", queue, err)
	}
	claimed, err := scanJobs(rows, limit)
	if err != nil {
		return nil, err
	}
	// RETURNING carries no order.
	sortByDue(claimed)
	return claimed, nil
}

// mysqlClaimSQL locks up to n rows of each kind. MySQL allows neither UPDATE
// ... RETURNING nor a LIMIT in a sub-select of the table being updated, so the
// claim is a transaction: lock, take, commit. A UNION ALL keeps the locking
// read to one round trip; when it returns more than the limit — only when
// rescued jobs were found — the pending rows past it stay locked until the
// commit and are not taken.
func (s *Store) mysqlClaimSQL() string {
	return fmt.Sprintf(
		`(SELECT %[2]s FROM %[1]s
			WHERE queue = ? AND status = ? AND available_at <= ?
			  AND lease_until IS NOT NULL AND lease_until <= ?
			ORDER BY available_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED)
		UNION ALL
		(SELECT %[2]s FROM %[1]s
			WHERE queue = ? AND status = ? AND available_at <= ?
			ORDER BY available_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED)`, s.quotedTable(), claimableColumns)
}

// claimSkipLockedMySQL runs the claim at READ COMMITTED. Under MySQL's default
// REPEATABLE READ a locking read also locks the GAPS it scans, and the scan of
// the running range then blocks every other claimer moving a row into it —
// measured on MySQL 8.4, sixteen workers were no faster than one. READ
// COMMITTED takes no gap locks and lets go of a row as soon as it fails the
// predicate; it costs one more round trip, to set the level.
func (s *Store) claimSkipLockedMySQL(ctx context.Context, owner, queue string, limit int, lease time.Duration, now time.Time) (claimed []Job, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("sqlprovider: claim from %s: begin: %w", queue, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, s.mysqlClaimSQL(),
		queue, string(StatusRunning), now, now, limit,
		queue, string(StatusPending), now, limit)
	if err != nil {
		return nil, fmt.Errorf("sqlprovider: claim from %s: %w", queue, err)
	}
	locked, err := scanJobs(rows, limit)
	if err != nil {
		return nil, err
	}
	if len(locked) == 0 {
		return nil, tx.Commit()
	}
	// Rescued first, then by due time — the order the union was written in,
	// which MySQL does not promise to keep.
	sort.SliceStable(locked, func(i, j int) bool {
		if locked[i].Status != locked[j].Status {
			return locked[i].Status == StatusRunning
		}
		return locked[i].AvailableAt.Before(locked[j].AvailableAt)
	})
	if len(locked) > limit {
		locked = locked[:limit]
	}

	placeholders := make([]string, len(locked))
	args := make([]any, 0, len(locked)+3)
	args = append(args, string(StatusRunning), owner, now.Add(lease))
	for i, job := range locked {
		placeholders[i] = "?"
		args = append(args, job.ID)
	}
	// The rows are this transaction's until the commit, so nothing else can
	// have changed them: the ids are predicate enough.
	updateSQL := fmt.Sprintf(
		`UPDATE %s SET status = ?, lease_owner = ?, lease_until = ?, attempts = attempts + 1
		WHERE id IN (%s)`, s.quotedTable(), joinComma(placeholders))
	if _, err = tx.ExecContext(ctx, updateSQL, args...); err != nil {
		return nil, fmt.Errorf("sqlprovider: claim from %s: %w", queue, err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("sqlprovider: claim from %s: commit: %w", queue, err)
	}
	for i := range locked {
		locked[i].Status = StatusRunning
		locked[i].Attempts++
	}
	sortByDue(locked)
	return locked, nil
}

// scanJobs reads a claim's rows and closes them.
func scanJobs(rows *sql.Rows, capacity int) ([]Job, error) {
	jobs := make([]Job, 0, capacity)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("sqlprovider: scan claimed jobs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return jobs, nil
}

// sortByDue puts claimed jobs in the order the queue serves them.
func sortByDue(jobs []Job) {
	sort.SliceStable(jobs, func(i, j int) bool {
		if !jobs[i].AvailableAt.Equal(jobs[j].AvailableAt) {
			return jobs[i].AvailableAt.Before(jobs[j].AvailableAt)
		}
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
}

// claimFromQueue is the portable claim: select candidates, then race a
// conditional UPDATE for each. It reports lost when it found candidates and
// claimed none of them — every one went to another worker in between.
func (s *Store) claimFromQueue(ctx context.Context, owner, queue string, limit int, lease time.Duration, now time.Time) ([]Job, bool, error) {
	selectSQL := s.rebind(fmt.Sprintf(
		`SELECT %s
		FROM %s
		WHERE queue = ? AND available_at <= ?
		  AND (
		        status = ?
		     OR (status = ? AND lease_until IS NOT NULL AND lease_until <= ?)
		      )
		ORDER BY available_at ASC, created_at ASC
		LIMIT ?`, claimableColumns, s.quotedTable()))

	rows, err := s.db.QueryContext(ctx, selectSQL,
		queue, now, string(StatusPending), string(StatusRunning), now, limit)
	if err != nil {
		return nil, false, fmt.Errorf("sqlprovider: select candidates: %w", err)
	}
	candidates, err := scanJobs(rows, limit)
	if err != nil {
		return nil, false, err
	}

	claimed := make([]Job, 0, len(candidates))
	for _, job := range candidates {
		ok, err := s.tryClaim(ctx, &job, owner, lease, now)
		if err != nil {
			return claimed, false, err
		}
		if ok {
			claimed = append(claimed, job)
		}
	}
	return claimed, len(candidates) > 0 && len(claimed) == 0, nil
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
