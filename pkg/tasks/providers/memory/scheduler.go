package memoryprovider

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
	"github.com/robfig/cron/v3"
)

var (
	ErrNilScheduler = errors.New("memoryprovider: scheduler is nil")
)

type SchedulerConfig struct {
	Manager  *Manager
	Location *time.Location
	// Logger receives the ticks that could not be enqueued; nil uses
	// the manager's logger.
	Logger *slog.Logger
}

type Scheduler struct {
	manager *Manager
	cron    *cron.Cron
	entries map[string]cron.EntryID
	mu      sync.Mutex
	logger  *slog.Logger
	// dropped counts scheduler ticks whose enqueue failed (queue full,
	// unsupported policy): before NU-12 the error was discarded and a
	// silent scheduler looked like a healthy one.
	dropped atomic.Int64
}

// Dropped reports how many scheduled ticks failed to enqueue.
func (s *Scheduler) Dropped() int64 { return s.dropped.Load() }

func NewScheduler(cfg SchedulerConfig) (*Scheduler, error) {
	if cfg.Manager == nil {
		return nil, errors.New("memoryprovider: manager is required for scheduler")
	}

	opts := []cron.Option{cron.WithSeconds()}
	if cfg.Location != nil {
		opts = append(opts, cron.WithLocation(cfg.Location))
	} else {
		opts = append(opts, cron.WithLocation(time.UTC))
	}

	logger := cfg.Logger
	if logger == nil {
		logger = cfg.Manager.logger
	}
	return &Scheduler{
		manager: cfg.Manager,
		cron:    cron.New(opts...),
		entries: make(map[string]cron.EntryID),
		logger:  logger,
	}, nil
}

func (s *Scheduler) RegisterJSON(spec, taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	if s == nil {
		return "", ErrNilScheduler
	}

	id := uuid.NewString()

	entryID, err := s.cron.AddFunc(spec, func() {
		if _, err := s.manager.EnqueueJSONWithPolicy(taskType, payload, policy); err != nil {
			s.dropped.Add(1)
			if s.logger != nil {
				s.logger.Error("memoryprovider: scheduled tick not enqueued", "type", taskType, "spec", spec, "error", err)
			}
		}
	})
	if err != nil {
		return "", fmt.Errorf("memoryprovider.Scheduler.RegisterJSON: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id] = entryID

	return id, nil
}

func (s *Scheduler) Unregister(entryID string) error {
	if s == nil {
		return ErrNilScheduler
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if cronID, ok := s.entries[entryID]; ok {
		s.cron.Remove(cronID)
		delete(s.entries, entryID)
	}

	return nil
}

func (s *Scheduler) Start() error {
	if s == nil {
		return ErrNilScheduler
	}
	s.cron.Start()
	return nil
}

func (s *Scheduler) Close() error {
	if s == nil {
		return ErrNilScheduler
	}
	ctx := s.cron.Stop()
	<-ctx.Done()
	return nil
}
