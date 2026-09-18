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
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// Task is one job handed to a handler.
type Task struct {
	taskType string
	payload  []byte
}

func (t *Task) Type() string    { return t.taskType }
func (t *Task) Payload() []byte { return t.payload }

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
}

// Manager is the tasks.Manager implementation backed by SQL.
type Manager struct {
	cfg    ManagerConfig
	logger *slog.Logger

	mu       sync.RWMutex
	handlers map[string]tasks.HandlerFunc

	ctx     context.Context
	cancel  context.CancelFunc
	running bool

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
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		cfg:      cfg,
		logger:   logger,
		handlers: map[string]tasks.HandlerFunc{},
		ctx:      ctx,
		cancel:   cancel,
		inflight: map[string]struct{}{},
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
	m.lifecycle.Unlock()

	<-ctx.Done()
	return m.Close()
}

// Close stops the workers and releases what they still hold, so the jobs this
// process was running are available to the next one immediately instead of
// waiting out their leases.
func (m *Manager) Close() error {
	m.cancel()
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.wg.Wait()

	ids := m.inflightIDs()
	if len(ids) == 0 {
		return nil
	}
	// A short, independent context: the manager's is already cancelled, and
	// this last write is what keeps an orderly shutdown from costing a lease
	// duration of latency.
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
	if _, err := m.cfg.Store.ReapAbandoned(m.ctx, now); err != nil {
		return 0, err
	}
	jobs, err := m.cfg.Store.Claim(m.ctx, m.cfg.Owner, m.cfg.Queues, 1, m.cfg.LeaseDuration, now)
	if err != nil || len(jobs) == 0 {
		return 0, err
	}
	job := jobs[0]
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
		if err := m.cfg.Store.Release(m.ctx, m.cfg.Owner, []string{job.ID}, time.Now().Add(m.cfg.PollInterval)); err != nil {
			m.logger.Error("sqlprovider: could not release an unhandled job", "error", err, "type", job.TaskType)
		}
		return
	}

	ctx := context.WithoutCancel(m.ctx)
	var cancel context.CancelFunc = func() {}
	if job.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, job.Timeout)
	}
	err := handler(ctx, &Task{taskType: job.TaskType, payload: job.Payload})
	cancel()

	// The finishing write uses a context of its own: the manager's may already
	// be cancelled by a shutdown, and a job that RAN has to be recorded as
	// having run, or the next process runs it again.
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()

	if err == nil {
		if err := m.cfg.Store.Succeed(writeCtx, job.ID, time.Now()); err != nil {
			m.logger.Error("sqlprovider: could not mark a job done", "error", err, "id", job.ID)
		}
		return
	}
	dead, markErr := m.cfg.Store.Retry(writeCtx, job, err, time.Now())
	if markErr != nil {
		m.logger.Error("sqlprovider: could not record a job failure", "error", markErr, "id", job.ID)
		return
	}
	if dead {
		m.logger.Error("sqlprovider: job is out of attempts and went to the dead letter",
			"error", err, "type", job.TaskType, "id", job.ID, "attempts", job.Attempts)
		return
	}
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
		case <-m.ctx.Done():
			return
		case <-time.After(interval):
			ids := m.inflightIDs()
			if len(ids) == 0 {
				continue
			}
			if err := m.cfg.Store.Heartbeat(m.ctx, m.cfg.Owner, ids, m.cfg.LeaseDuration, time.Now()); err != nil {
				m.logger.Error("sqlprovider: heartbeat failed", "error", err, "jobs", len(ids))
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
		return "", err
	}
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
