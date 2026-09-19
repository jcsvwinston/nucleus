// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// DefaultLeaderScope is the row the election contends on. Two applications
// sharing one database must use distinct job tables (the scope is derived from
// the table name), which they should anyway.
const DefaultLeaderScope = "scheduler"

// SchedulerConfig configures the cron scheduler.
type SchedulerConfig struct {
	// Manager enqueues what the schedule fires.
	Manager *Manager
	// Store is where the leadership lease lives.
	Store *Store
	// Location for cron expressions; nil uses the local zone, as the
	// in-process scheduler does.
	Location *time.Location
	// LeaderTTL overrides DefaultLeaderTTL.
	LeaderTTL time.Duration
	// Owner identifies this replica in the lease. Empty derives it from the
	// hostname and pid.
	Owner  string
	Logger *slog.Logger
}

// Scheduler ticks cron entries on exactly ONE replica.
//
// Every replica runs this, and they all contend for a lease row in the
// application's own database; the one that holds it ticks, the others wait and
// take over within one TTL if it dies. Without an election each replica fires
// every entry on every tick — the defect NF-1 fixed for asynq with a Redis
// lock, and the reason the SQL provider refused cron until now.
type Scheduler struct {
	cfg    SchedulerConfig
	cron   *cron.Cron
	logger *slog.Logger
	owner  string

	mu      sync.Mutex
	entries map[string]cron.EntryID

	leading atomic.Bool
	dropped atomic.Int64

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
}

// NewScheduler builds the scheduler. It does not contend for leadership until
// Start.
func NewScheduler(cfg SchedulerConfig) (*Scheduler, error) {
	if cfg.Manager == nil {
		return nil, errors.New("sqlprovider: scheduler needs a manager")
	}
	if cfg.Store == nil {
		return nil, errors.New("sqlprovider: scheduler needs a store for the leadership lease")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.LeaderTTL <= 0 {
		cfg.LeaderTTL = DefaultLeaderTTL
	}
	owner := cfg.Owner
	if owner == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "unknown-host"
		}
		owner = fmt.Sprintf("%s-%d", host, os.Getpid())
	}
	location := cfg.Location
	if location == nil {
		location = time.Local
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		cfg:     cfg,
		cron:    cron.New(cron.WithLocation(location)),
		logger:  cfg.Logger,
		owner:   owner,
		entries: map[string]cron.EntryID{},
		ctx:     ctx,
		cancel:  cancel,
	}, nil
}

// Dropped reports scheduled ticks whose enqueue failed.
func (s *Scheduler) Dropped() int64 { return s.dropped.Load() }

// Leading reports whether this replica currently holds the lease. It is what a
// test — or an operator's status page — asks instead of guessing from logs.
func (s *Scheduler) Leading() bool { return s.leading.Load() }

// RegisterJSON adds an entry. Every replica registers the same entries; only
// the leader's fire.
func (s *Scheduler) RegisterJSON(spec, taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	if s == nil {
		return "", errors.New("sqlprovider: scheduler is nil")
	}
	if taskType == "" {
		return "", tasks.ErrTaskTypeRequired
	}
	id, err := s.cron.AddFunc(spec, func() {
		// The check is per TICK, not per process: leadership can move while
		// the schedule is running, and a replica that lost it must stop
		// firing immediately rather than at the next restart.
		if !s.leading.Load() {
			return
		}
		if _, err := s.cfg.Manager.EnqueueJSONWithPolicy(taskType, payload, policy); err != nil {
			s.dropped.Add(1)
			s.logger.Error("sqlprovider: scheduled tick could not be enqueued",
				"error", err, "type", taskType, "spec", spec)
		}
	})
	if err != nil {
		return "", fmt.Errorf("sqlprovider: invalid schedule %q: %w", spec, err)
	}
	entryID := fmt.Sprintf("%d", id)
	s.mu.Lock()
	s.entries[entryID] = id
	s.mu.Unlock()
	return entryID, nil
}

// Unregister removes an entry.
func (s *Scheduler) Unregister(entryID string) error {
	if s == nil {
		return errors.New("sqlprovider: scheduler is nil")
	}
	s.mu.Lock()
	id, ok := s.entries[entryID]
	if ok {
		delete(s.entries, entryID)
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("sqlprovider: no scheduled entry %q", entryID)
	}
	s.cron.Remove(id)
	return nil
}

// Start begins the cron loop and the election. The cron loop runs on every
// replica; what it does on a tick depends on who holds the lease.
func (s *Scheduler) Start() error {
	if s == nil {
		return errors.New("sqlprovider: scheduler is nil")
	}
	s.cron.Start()
	s.wg.Add(1)
	go s.elect()
	return nil
}

// elect contends for the lease and keeps it renewed.
func (s *Scheduler) elect() {
	defer s.wg.Done()
	interval := s.cfg.LeaderTTL / 3
	if interval <= 0 {
		interval = time.Second
	}
	for {
		s.contend()
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (s *Scheduler) contend() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := s.cfg.Store.AcquireLeadership(ctx, s.scope(), s.owner, s.cfg.LeaderTTL, time.Now())
	if err != nil {
		// Losing the database is losing the right to fire: another replica
		// that can still reach it will take the lease over, and two replicas
		// ticking the same schedule is the thing this exists to prevent.
		if s.leading.CompareAndSwap(true, false) {
			s.logger.Error("sqlprovider: stepping down as scheduler leader; the lease could not be renewed",
				"error", err, "owner", s.owner)
		}
		return
	}
	if got {
		if s.leading.CompareAndSwap(false, true) {
			s.logger.Info("sqlprovider: this replica is the scheduler leader", "owner", s.owner, "ttl", s.cfg.LeaderTTL)
		}
		return
	}
	if s.leading.CompareAndSwap(true, false) {
		s.logger.Warn("sqlprovider: another replica took the scheduler lease", "owner", s.owner)
	}
}

func (s *Scheduler) scope() string { return s.cfg.Store.table + ":" + DefaultLeaderScope }

// Close stops ticking and gives the lease up, so the next replica does not
// have to wait out the TTL.
func (s *Scheduler) Close() error {
	if s == nil {
		return errors.New("sqlprovider: scheduler is nil")
	}
	s.once.Do(func() {
		s.cancel()
		<-s.cron.Stop().Done()
		s.wg.Wait()
		if s.leading.Load() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.cfg.Store.ReleaseLeadership(ctx, s.scope(), s.owner); err != nil {
				s.logger.Warn("sqlprovider: could not release the scheduler lease; it expires on its own",
					"error", err, "ttl", s.cfg.LeaderTTL)
			}
			s.leading.Store(false)
		}
	})
	return nil
}
