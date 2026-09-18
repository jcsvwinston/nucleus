// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jcsvwinston/nucleus/internal/jobstelemetry"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// Task is one job handed to a handler.
type Task struct {
	taskType string
	payload  []byte
}

func (t *Task) Type() string    { return t.taskType }
func (t *Task) Payload() []byte { return t.payload }

// providerName labels every metric this provider records.
const providerName = "sql"

// defaultMaxAttempts is what MaxRetry -1 (the "provider default" value of
// tasks.DefaultEnqueuePolicy) means here: the job is tried three times in all.
const defaultMaxAttempts = 3

// ManagerConfig configures the worker side.
type ManagerConfig struct {
	// Store is the queue's table.
	Store *Store
	// Concurrency is how many jobs this process runs at once.
	Concurrency int
	// Queues is the order queues are served in — the first is served before
	// the second, which is how priority is expressed. Empty means "default".
	Queues []string
	// LeaseDuration is how long a claim holds a job before another worker may
	// take it over. The heartbeat renews it while the handler runs.
	LeaseDuration time.Duration
	// PollInterval is how long a worker waits when it finds nothing.
	PollInterval time.Duration
	// Owner identifies this process in the lease rows. Empty derives one from
	// the hostname and pid, so a lease can be traced to the process holding it.
	Owner string
	// ShutdownGrace is how long Close waits for the handlers that are already
	// running before it cancels their contexts. Zero uses the lease duration,
	// which is the longest a job can run unnoticed anyway. It exists because
	// an orderly shutdown that waits FOR EVER hangs the process, and one that
	// cuts immediately leaves the lease to expire under a handler that is
	// still working — and then a second replica runs the same job alongside it.
	ShutdownGrace time.Duration
}

// Manager is the tasks.Manager implementation backed by SQL.
type Manager struct {
	cfg    ManagerConfig
	logger *slog.Logger

	mu       sync.RWMutex
	handlers map[string]tasks.HandlerFunc

	// ctx says "stop taking new work"; jobsCtx is what the handlers run under
	// and is cancelled only when the grace period is over, so a shutdown does
	// not cut a job that was about to finish.
	ctx        context.Context
	cancel     context.CancelFunc
	jobsCtx    context.Context
	jobsCancel context.CancelFunc
	running    bool

	lifecycle sync.Mutex
	wg        sync.WaitGroup

	inflightMu sync.Mutex
	inflight   map[string]struct{}
}

// NewManager builds the provider. It does not start anything: Run does.
func NewManager(cfg ManagerConfig, logger *slog.Logger) (*Manager, error) {
	if cfg.Store == nil {
		return nil, errors.New("sqlprovider: store is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 30 * time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if len(cfg.Queues) == 0 {
		cfg.Queues = []string{"default"}
	}
	if cfg.Owner == "" {
		host, _ := os.Hostname()
		cfg.Owner = fmt.Sprintf("%s/%d", host, os.Getpid())
	}
	if cfg.ShutdownGrace <= 0 {
		cfg.ShutdownGrace = cfg.LeaseDuration
	}
	ctx, cancel := context.WithCancel(context.Background())
	jobsCtx, jobsCancel := context.WithCancel(context.Background())
	return &Manager{
		cfg:        cfg,
		logger:     logger,
		handlers:   map[string]tasks.HandlerFunc{},
		ctx:        ctx,
		cancel:     cancel,
		jobsCtx:    jobsCtx,
		jobsCancel: jobsCancel,
		inflight:   map[string]struct{}{},
	}, nil
}

// HandleFunc registers the handler for a task type.
func (m *Manager) HandleFunc(taskType string, handler tasks.HandlerFunc) error {
	if taskType == "" {
		return tasks.ErrTaskTypeRequired
	}
	if handler == nil {
		return tasks.ErrNilHandler
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[taskType] = handler
	return nil
}

// Run works the queue until ctx is done.
func (m *Manager) Run(ctx context.Context) error {
	m.lifecycle.Lock()
	if m.running {
		m.lifecycle.Unlock()
		return errors.New("sqlprovider: manager is already running")
	}
	if m.ctx.Err() != nil {
		m.lifecycle.Unlock()
		return errors.New("sqlprovider: manager is stopped")
	}
	m.running = true
	for i := 0; i < m.cfg.Concurrency; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	m.wg.Add(1)
	go m.heartbeat()
	m.wg.Add(1)
	go m.reap()
	m.lifecycle.Unlock()

	<-ctx.Done()
	return m.Close()
}

// Close stops taking new work, gives the handlers that are already running a
// grace period to finish, and releases whatever is still held so the next
// process can pick it up immediately instead of waiting out the leases.
//
// The order matters and was wrong at first. Cancelling everything at once
// killed the heartbeat while the handlers kept working: the leases expired
// under them and another replica ran the SAME job alongside the one still
// going — which is not the duplicate an at-least-once queue forgives, because
// nothing had died. So the heartbeat lives as long as the workers do, and the
// handlers are cut only when the grace period is over.
func (m *Manager) Close() error {
	m.cancel()

	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(m.cfg.ShutdownGrace):
		// Out of grace: cut the handlers. Their jobs go back to the queue
		// below, with the attempt returned — they were interrupted, not failed.
		m.logger.Warn("sqlprovider: handlers still running after the shutdown grace period; cancelling them",
			"grace", m.cfg.ShutdownGrace, "jobs", len(m.inflightIDs()))
		m.jobsCancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			m.logger.Error("sqlprovider: workers did not return after cancellation; their jobs will be recovered when the leases expire")
		}
	}
	m.jobsCancel()

	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()

	ids := m.inflightIDs()
	if len(ids) == 0 {
		return nil
	}
	// An independent context: the manager's is cancelled, and this last write
	// is what keeps an orderly shutdown from costing a lease of latency.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.cfg.Store.Release(ctx, m.cfg.Owner, ids, time.Now()); err != nil {
		m.logger.Error("sqlprovider: could not release in-flight jobs on shutdown",
			"error", err, "jobs", len(ids))
		// Not fatal: the leases expire and another worker picks them up.
		return nil
	}
	m.logger.Info("sqlprovider: released in-flight jobs on shutdown", "jobs", len(ids))
	return nil
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		if m.ctx.Err() != nil {
			return
		}
		n, err := m.workOnce()
		if err != nil {
			m.logger.Error("sqlprovider: claim failed", "error", err)
		}
		if n > 0 {
			continue
		}
		select {
		case <-time.After(m.cfg.PollInterval):
		case <-m.ctx.Done():
			return
		}
	}
}

// workOnce claims at most one job and runs it. One at a time per worker keeps
// the lease a worker holds equal to the work it is actually doing: a batch
// claim would hold leases on jobs sitting in a slice.
func (m *Manager) workOnce() (int, error) {
	now := time.Now().UTC()
	jobs, err := m.cfg.Store.Claim(m.ctx, m.cfg.Owner, m.cfg.Queues, 1, m.cfg.LeaseDuration, now)
	if err != nil || len(jobs) == 0 {
		return 0, err
	}
	job := jobs[0]
	jobstelemetry.Started(m.jobsCtx, providerName, job.Queue, job.TaskType)
	m.markInflight(job.ID, true)
	defer m.markInflight(job.ID, false)
	m.execute(job)
	return 1, nil
}

func (m *Manager) execute(job Job) {
	m.mu.RLock()
	handler, ok := m.handlers[job.TaskType]
	m.mu.RUnlock()
	if !ok {
		// Nobody in THIS process handles it. The job goes back to pending —
		// another replica may have the handler, and a worker deployed later
		// certainly will. It is not a failure of the job, so it does not
		// spend the attempt the claim took.
		jobstelemetry.Held(m.jobsCtx, providerName, job.Queue, job.TaskType, "no_handler")
		if err := m.cfg.Store.Release(m.ctx, m.cfg.Owner, []string{job.ID}, time.Now().Add(m.cfg.PollInterval)); err != nil {
			m.logger.Error("sqlprovider: could not release an unhandled job", "error", err, "type", job.TaskType)
		}
		return
	}

	ctx := m.jobsCtx
	var cancel context.CancelFunc = func() {}
	if job.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, job.Timeout)
	}
	startedAt := time.Now()
	err := handler(ctx, &Task{taskType: job.TaskType, payload: job.Payload})
	cancel()

	// The finishing write uses a context of its own: the manager's may already
	// be cancelled by a shutdown, and a job that RAN has to be recorded as
	// having run, or the next process runs it again.
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()

	if err == nil {
		owned, markErr := m.cfg.Store.Succeed(writeCtx, m.cfg.Owner, job.ID, time.Now())
		if markErr != nil {
			m.logger.Error("sqlprovider: could not mark a job done", "error", markErr, "id", job.ID)
			return
		}
		jobstelemetry.Succeeded(writeCtx, providerName, job.Queue, job.TaskType, time.Since(startedAt))
		if !owned {
			// The lease was lost while the handler ran, so somebody else owns
			// the job now and will run it again. Saying so is the difference
			// between an at-least-once queue and a silent duplicate.
			m.logger.Warn("sqlprovider: finished a job whose lease had already been taken over; it will run again",
				"type", job.TaskType, "id", job.ID, "lease", m.cfg.LeaseDuration)
		}
		return
	}
	dead, markErr := m.cfg.Store.Retry(writeCtx, m.cfg.Owner, job, err, time.Now())
	if markErr != nil {
		m.logger.Error("sqlprovider: could not record a job failure", "error", markErr, "id", job.ID)
		return
	}
	if dead {
		jobstelemetry.Failed(writeCtx, providerName, job.Queue, job.TaskType)
		m.logger.Error("sqlprovider: job is out of attempts and went to the dead letter",
			"error", err, "type", job.TaskType, "id", job.ID, "attempts", job.Attempts)
		return
	}
	jobstelemetry.Retried(writeCtx, providerName, job.Queue, job.TaskType, time.Since(startedAt))
	m.logger.Warn("sqlprovider: job failed, retrying later",
		"error", err, "type", job.TaskType, "id", job.ID, "attempt", job.Attempts)
}

// heartbeat renews the leases of the jobs this process is running, so a
// handler that legitimately runs longer than one lease is not reclaimed
// underneath itself.
func (m *Manager) heartbeat() {
	defer m.wg.Done()
	interval := m.cfg.LeaseDuration / 3
	if interval <= 0 {
		interval = time.Second
	}
	for {
		select {
		case <-m.jobsCtx.Done():
			// Only when the handlers themselves are cut: while any job is
			// still running its lease has to keep being renewed, or another
			// replica takes it over and runs it alongside this one.
			return
		case <-time.After(interval):
			ids := m.inflightIDs()
			if len(ids) == 0 {
				if m.ctx.Err() != nil {
					return
				}
				continue
			}
			hbCtx, hbCancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := m.cfg.Store.Heartbeat(hbCtx, m.cfg.Owner, ids, m.cfg.LeaseDuration, time.Now())
			hbCancel()
			if err != nil {
				m.logger.Error("sqlprovider: heartbeat failed", "error", err, "jobs", len(ids))
			}
		}
	}
}

// reap retires abandoned, exhausted jobs on a timer. It used to run on every
// claim of every worker, which is a write over every expired-lease row per
// worker per poll — permanent load on a table that is usually idle.
func (m *Manager) reap() {
	defer m.wg.Done()
	interval := m.cfg.LeaseDuration
	if interval < time.Second {
		interval = time.Second
	}
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-time.After(interval):
			n, err := m.cfg.Store.ReapAbandoned(m.ctx, time.Now())
			if err != nil {
				m.logger.Error("sqlprovider: reaping abandoned jobs failed", "error", err)
				continue
			}
			if n > 0 {
				m.logger.Warn("sqlprovider: retired abandoned jobs that were out of attempts", "jobs", n)
			}
		}
	}
}

func (m *Manager) markInflight(id string, on bool) {
	m.inflightMu.Lock()
	defer m.inflightMu.Unlock()
	if on {
		m.inflight[id] = struct{}{}
		return
	}
	delete(m.inflight, id)
}

func (m *Manager) inflightIDs() []string {
	m.inflightMu.Lock()
	defer m.inflightMu.Unlock()
	ids := make([]string, 0, len(m.inflight))
	for id := range m.inflight {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// EnqueueJSON and friends implement the client half of tasks.Manager.

func (m *Manager) EnqueueJSON(taskType string, payload any) (string, error) {
	return m.EnqueueJSONCtxWithPolicy(context.Background(), taskType, payload, tasks.DefaultEnqueuePolicy())
}

func (m *Manager) EnqueueJSONCtx(ctx context.Context, taskType string, payload any) (string, error) {
	return m.EnqueueJSONCtxWithPolicy(ctx, taskType, payload, tasks.DefaultEnqueuePolicy())
}

func (m *Manager) EnqueueJSONWithPolicy(taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	return m.EnqueueJSONCtxWithPolicy(context.Background(), taskType, payload, policy)
}

func (m *Manager) EnqueueJSONCtxWithPolicy(ctx context.Context, taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	if taskType == "" {
		return "", tasks.ErrTaskTypeRequired
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("sqlprovider: encode payload: %w", err)
	}
	job := jobFromPolicy(taskType, data, policy, time.Now().UTC())
	if err := m.cfg.Store.Enqueue(ctx, job); err != nil {
		var dup *DuplicateError
		if errors.As(err, &dup) {
			// Collapsed into the job already queued. The caller gets its id,
			// because "this work is already scheduled" is a success for
			// whoever asked, not a failure to report.
			return dup.ExistingID, nil
		}
		return "", err
	}
	jobstelemetry.Enqueued(ctx, providerName, job.Queue, taskType)
	return job.ID, nil
}

// EnqueueTx writes the job inside the caller's transaction: the job exists
// exactly when the work that asked for it commits, and never if it rolls back.
// It is the durable queue's answer to the oldest bug in background work —
// enqueueing something that refers to a row the transaction then abandons.
func (m *Manager) EnqueueTx(ctx context.Context, tx *sql.Tx, taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	if taskType == "" {
		return "", tasks.ErrTaskTypeRequired
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("sqlprovider: encode payload: %w", err)
	}
	job := jobFromPolicy(taskType, data, policy, time.Now().UTC())
	if err := m.cfg.Store.EnqueueTx(ctx, tx, job); err != nil {
		return "", err
	}
	return job.ID, nil
}

func jobFromPolicy(taskType string, payload []byte, policy tasks.EnqueuePolicy, now time.Time) Job {
	queue := policy.Queue
	if queue == "" {
		queue = "default"
	}
	maxAttempts := policy.MaxRetry + 1
	if policy.MaxRetry < 0 {
		maxAttempts = defaultMaxAttempts
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	available := now
	if policy.ProcessIn > 0 {
		available = now.Add(policy.ProcessIn)
	}
	return Job{
		ID:          uuid.NewString(),
		UniqueKey:   policy.UniqueKey,
		Queue:       queue,
		TaskType:    taskType,
		Payload:     payload,
		Status:      StatusPending,
		MaxAttempts: maxAttempts,
		Timeout:     policy.Timeout,
		BackoffBase: policy.BackoffBase,
		BackoffMax:  policy.BackoffMax,
		AvailableAt: available,
		CreatedAt:   now,
	}
}
