package asynqprovider

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// DefaultSchedulerLockKey is the Redis key the leader election contends on.
// Every replica of one application shares it (they share the jobs Redis),
// which is exactly the property the lock rides on.
const DefaultSchedulerLockKey = "nucleus:jobs:scheduler:leader"

// DefaultSchedulerLockTTL is the lease duration of the leader lock. A
// leader renews at a third of this, so a crashed leader is replaced after
// at most one TTL without any coordination.
const DefaultSchedulerLockTTL = 30 * time.Second

// renewScript extends the lease only while this instance still owns it —
// a plain PEXPIRE would happily renew a lock another replica took over.
var renewScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("pexpire", KEYS[1], ARGV[2])
end
return 0`)

// releaseScript deletes the lease only if this instance owns it.
var releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0`)

// LeaderSchedulerConfig configures a LeaderScheduler.
type LeaderSchedulerConfig struct {
	// Scheduler is the configuration of the underlying asynq scheduler
	// (RedisURL is also where the lock lives).
	Scheduler SchedulerConfig

	// LockKey overrides DefaultSchedulerLockKey. Two applications sharing
	// one Redis DB must set distinct keys (or better, distinct DBs).
	LockKey string

	// LockTTL overrides DefaultSchedulerLockTTL. Non-positive uses the
	// default.
	LockTTL time.Duration

	// Owner identifies this instance in the lock value. Empty derives
	// "<hostname>-<pid>" — leases expire, so identity does not need to
	// survive a restart.
	Owner string

	// Logger receives leadership transitions. Nil falls back to
	// slog.Default().
	Logger *slog.Logger
}

// LeaderScheduler wraps the asynq Scheduler with leader election over a
// Redis lock (SET NX + TTL), so that in a multi-replica deployment exactly
// one process ticks the cron entries (NF-1: without it every replica ran
// its own scheduler and every job fired once per replica).
//
// Registrations are recorded and replayed onto a fresh inner scheduler
// each time this instance wins the election, because an asynq scheduler
// cannot be restarted after Shutdown. Until leadership is won the entries
// are dormant — the workers on every replica keep consuming the queue
// regardless, which is the half of the runtime that IS safe to replicate.
type LeaderScheduler struct {
	cfg     LeaderSchedulerConfig
	client  *redis.Client
	logger  *slog.Logger
	owner   string
	lockKey string
	lockTTL time.Duration

	mu      sync.Mutex
	entries []leaderEntry
	inner   *Scheduler
	leader  bool
	started bool
	stop    chan struct{}
	done    chan struct{}
}

type leaderEntry struct {
	id       string
	spec     string
	taskType string
	payload  any
	policy   tasks.EnqueuePolicy
	realID   string
	removed  bool
}

// NewLeaderScheduler builds the leader-elected scheduler wrapper. It
// validates the Redis URL eagerly (both the scheduler and the lock live
// there) but opens no connection until Start.
func NewLeaderScheduler(cfg LeaderSchedulerConfig) (*LeaderScheduler, error) {
	opts, err := redis.ParseURL(cfg.Scheduler.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("tasks: leader scheduler redis url: %w", err)
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	owner := cfg.Owner
	if owner == "" {
		host, herr := os.Hostname()
		if herr != nil || host == "" {
			host = "unknown-host"
		}
		owner = fmt.Sprintf("%s-%d", host, os.Getpid())
	}
	lockKey := cfg.LockKey
	if lockKey == "" {
		lockKey = DefaultSchedulerLockKey
	}
	lockTTL := cfg.LockTTL
	if lockTTL <= 0 {
		lockTTL = DefaultSchedulerLockTTL
	}
	return &LeaderScheduler{
		cfg:     cfg,
		client:  redis.NewClient(opts),
		logger:  logger,
		owner:   owner,
		lockKey: lockKey,
		lockTTL: lockTTL,
	}, nil
}

// RegisterJSON records the registration for replay on leadership. The
// returned id is synthetic and stable across leadership changes.
func (s *LeaderScheduler) RegisterJSON(spec, taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	if s == nil {
		return "", ErrNilScheduler
	}
	if err := (PeriodicTask{Spec: spec, TaskType: taskType, Payload: payload, Policy: policy}).Validate(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("leader-entry-%d", len(s.entries))
	entry := leaderEntry{id: id, spec: spec, taskType: taskType, payload: payload, policy: policy}
	if s.inner != nil {
		realID, err := s.inner.RegisterJSON(spec, taskType, payload, policy)
		if err != nil {
			return "", err
		}
		entry.realID = realID
	}
	s.entries = append(s.entries, entry)
	return id, nil
}

// Unregister removes a recorded registration (and, while leading, the live
// asynq entry backing it).
func (s *LeaderScheduler) Unregister(entryID string) error {
	if s == nil {
		return ErrNilScheduler
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.entries {
		if s.entries[i].id != entryID || s.entries[i].removed {
			continue
		}
		s.entries[i].removed = true
		if s.inner != nil && s.entries[i].realID != "" {
			return s.inner.Unregister(s.entries[i].realID)
		}
		return nil
	}
	return fmt.Errorf("tasks: leader scheduler: unknown entry id %q", entryID)
}

// Ping checks the Redis connection the lock (and scheduler) depend on.
func (s *LeaderScheduler) Ping() error {
	if s == nil || s.client == nil {
		return ErrNilScheduler
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.client.Ping(ctx).Err()
}

// IsLeader reports whether this instance currently holds the lock and is
// ticking the schedule.
func (s *LeaderScheduler) IsLeader() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leader
}

// Start launches the election loop. It returns immediately; the schedule
// begins ticking on this instance only once the lock is won.
func (s *LeaderScheduler) Start() error {
	if s == nil {
		return ErrNilScheduler
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("tasks: leader scheduler already started")
	}
	s.started = true
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	s.mu.Unlock()

	go s.electionLoop()
	return nil
}

// Close stops the election loop, shuts the inner scheduler down and
// releases the lock if held.
func (s *LeaderScheduler) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		if s.client != nil {
			return s.client.Close()
		}
		return nil
	}
	stop, done := s.stop, s.done
	s.mu.Unlock()

	close(stop)
	<-done
	return s.client.Close()
}

// electionLoop contends for the lock, holds it while leading, and steps
// down cleanly on Close.
func (s *LeaderScheduler) electionLoop() {
	defer close(s.done)
	retry := s.lockTTL / 3
	if retry <= 0 {
		retry = time.Second
	}
	for {
		acquired, err := s.tryAcquire()
		if err != nil {
			s.logger.Warn("nucleus: jobs scheduler leader election: lock attempt failed; retrying",
				"error", err, "lock_key", s.lockKey)
		}
		if acquired {
			s.lead()
			// lead() returns on lost leadership or on stop.
		}
		select {
		case <-s.stop:
			return
		case <-time.After(retry):
		}
	}
}

func (s *LeaderScheduler) tryAcquire() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.client.SetNX(ctx, s.lockKey, s.owner, s.lockTTL).Result()
}

// lead builds a fresh inner scheduler, replays the recorded registrations,
// starts ticking, and renews the lease until it is lost or Close is
// called. On any failure it releases the lock so another replica can lead.
func (s *LeaderScheduler) lead() {
	inner, err := NewScheduler(s.cfg.Scheduler)
	if err != nil {
		s.logger.Error("nucleus: jobs scheduler leader election: building scheduler failed; releasing lock", "error", err)
		s.release()
		return
	}

	s.mu.Lock()
	for i := range s.entries {
		if s.entries[i].removed {
			continue
		}
		realID, rerr := inner.RegisterJSON(s.entries[i].spec, s.entries[i].taskType, s.entries[i].payload, s.entries[i].policy)
		if rerr != nil {
			s.mu.Unlock()
			s.logger.Error("nucleus: jobs scheduler leader election: replaying registration failed; releasing lock",
				"task_type", s.entries[i].taskType, "error", rerr)
			inner.Shutdown()
			s.release()
			return
		}
		s.entries[i].realID = realID
	}
	s.mu.Unlock()

	if err := inner.Start(); err != nil {
		s.logger.Error("nucleus: jobs scheduler leader election: starting scheduler failed; releasing lock", "error", err)
		s.release()
		return
	}

	s.mu.Lock()
	s.inner = inner
	s.leader = true
	s.mu.Unlock()
	s.logger.Info("nucleus: jobs scheduler leadership ACQUIRED — this replica ticks the cron entries",
		"owner", s.owner, "lock_key", s.lockKey, "ttl", s.lockTTL)

	renewEvery := s.lockTTL / 3
	if renewEvery <= 0 {
		renewEvery = time.Second
	}
	ticker := time.NewTicker(renewEvery)
	defer ticker.Stop()

	stepDown := func(reason string) {
		s.mu.Lock()
		s.inner = nil
		s.leader = false
		s.mu.Unlock()
		inner.Shutdown()
		s.logger.Warn("nucleus: jobs scheduler leadership LOST — scheduler stopped on this replica; contending again",
			"owner", s.owner, "reason", reason)
	}

	for {
		select {
		case <-s.stop:
			s.mu.Lock()
			s.inner = nil
			s.leader = false
			s.mu.Unlock()
			inner.Shutdown()
			s.release()
			s.logger.Info("nucleus: jobs scheduler leadership released on shutdown", "owner", s.owner)
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			renewed, err := renewScript.Run(ctx, s.client, []string{s.lockKey}, s.owner, s.lockTTL.Milliseconds()).Int64()
			cancel()
			if err != nil {
				// One failed renewal is survivable (the TTL outlasts the
				// renew interval three to one); the ownership check on the
				// next tick decides.
				s.logger.Warn("nucleus: jobs scheduler leadership renew failed; will retry", "error", err)
				continue
			}
			if renewed == 0 {
				stepDown("lease expired or taken over")
				return
			}
		}
	}
}

func (s *LeaderScheduler) release() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := releaseScript.Run(ctx, s.client, []string{s.lockKey}, s.owner).Err(); err != nil && err != redis.Nil {
		s.logger.Warn("nucleus: jobs scheduler leader election: releasing lock failed (the TTL will expire it)", "error", err)
	}
}
