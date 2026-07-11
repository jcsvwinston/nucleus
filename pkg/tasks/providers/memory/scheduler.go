package memoryprovider

import (
	"errors"
	"fmt"
	"sync"
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
}

type Scheduler struct {
	manager *Manager
	cron    *cron.Cron
	entries map[string]cron.EntryID
	mu      sync.Mutex
}

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

	return &Scheduler{
		manager: cfg.Manager,
		cron:    cron.New(opts...),
		entries: make(map[string]cron.EntryID),
	}, nil
}

func (s *Scheduler) RegisterJSON(spec, taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	if s == nil {
		return "", ErrNilScheduler
	}

	id := uuid.NewString()

	entryID, err := s.cron.AddFunc(spec, func() {
		_, _ = s.manager.EnqueueJSONWithPolicy(taskType, payload, policy)
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
