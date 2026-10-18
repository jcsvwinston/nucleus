package asynqprovider

import (
	"fmt"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

var (
	ErrNilScheduler     = fmt.Errorf("tasks: scheduler is nil")
	ErrCronSpecRequired = fmt.Errorf("tasks: cron spec is required")
)

// SchedulerConfig configures periodic task scheduling backed by Asynq.
type SchedulerConfig struct {
	RedisURL          string
	HeartbeatInterval time.Duration
	Location          *time.Location
}

// PeriodicTask describes one explicit cron registration.
type PeriodicTask struct {
	Spec     string
	TaskType string
	Payload  any
	Policy   tasks.EnqueuePolicy
}

// Scheduler wraps Asynq's scheduler with a smaller, explicit framework API.
type Scheduler struct {
	scheduler *asynq.Scheduler
}

func (t PeriodicTask) Validate() error {
	if strings.TrimSpace(t.Spec) == "" {
		return ErrCronSpecRequired
	}
	if strings.TrimSpace(t.TaskType) == "" {
		return ErrTaskTypeRequired
	}
	return nil
}

func NewScheduler(cfg SchedulerConfig) (*Scheduler, error) {
	redisOpts, err := redisClientOptFromURL(cfg.RedisURL)
	if err != nil {
		return nil, err
	}

	opts := &asynq.SchedulerOpts{
		HeartbeatInterval: cfg.HeartbeatInterval,
		Location:          cfg.Location,
	}

	return &Scheduler{
		scheduler: asynq.NewScheduler(redisOpts, opts),
	}, nil
}

func (s *Scheduler) Ping() error {
	if s == nil || s.scheduler == nil {
		return ErrNilScheduler
	}
	return s.scheduler.Ping()
}

func (s *Scheduler) Start() error {
	if s == nil || s.scheduler == nil {
		return ErrNilScheduler
	}
	return s.scheduler.Start()
}

func (s *Scheduler) Run() error {
	if s == nil || s.scheduler == nil {
		return ErrNilScheduler
	}
	return s.scheduler.Run()
}

func (s *Scheduler) Shutdown() {
	if s == nil || s.scheduler == nil {
		return
	}
	s.scheduler.Shutdown()
}

func (s *Scheduler) Close() error {
	if s == nil || s.scheduler == nil {
		return nil
	}
	s.scheduler.Shutdown()
	return nil
}

func (s *Scheduler) Register(task PeriodicTask) (string, error) {
	if s == nil || s.scheduler == nil {
		return "", ErrNilScheduler
	}
	if err := task.Validate(); err != nil {
		return "", err
	}

	asynqTask, err := newJSONTask(task.TaskType, task.Payload, taskCorrelation{})
	if err != nil {
		return "", err
	}
	opts := policyToOptions(task.Policy)
	entryID, err := s.scheduler.Register(strings.TrimSpace(task.Spec), asynqTask, opts...)
	if err != nil {
		return "", fmt.Errorf("tasks.Scheduler.Register: %w", err)
	}
	return entryID, nil
}

func (s *Scheduler) RegisterJSON(spec, taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	return s.Register(PeriodicTask{
		Spec:     spec,
		TaskType: taskType,
		Payload:  payload,
		Policy:   policy,
	})
}

func (s *Scheduler) Unregister(entryID string) error {
	if s == nil || s.scheduler == nil {
		return ErrNilScheduler
	}
	if strings.TrimSpace(entryID) == "" {
		return fmt.Errorf("tasks: scheduler entry id is required")
	}
	if err := s.scheduler.Unregister(strings.TrimSpace(entryID)); err != nil {
		return fmt.Errorf("tasks.Scheduler.Unregister: %w", err)
	}
	return nil
}
