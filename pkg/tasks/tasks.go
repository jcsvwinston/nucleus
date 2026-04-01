// Package tasks provides background job enqueueing and worker runtime backed by Asynq.
package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/hibiken/asynq"
)

var (
	ErrRedisURLRequired = errors.New("tasks: redis url is required")
	ErrTaskTypeRequired = errors.New("tasks: task type is required")
	ErrNilHandler       = errors.New("tasks: handler is required")
	ErrNilManager       = errors.New("tasks: manager is nil")
)

// Config configures task enqueueing and worker runtime.
type Config struct {
	RedisURL       string
	Concurrency    int
	Queues         map[string]int
	StrictPriority bool
}

// Manager owns Asynq client/server instances and task handler registrations.
type Manager struct {
	logger    *slog.Logger
	client    *asynq.Client
	server    *asynq.Server
	mux       *asynq.ServeMux
	closeOnce sync.Once
}

// NewManager initializes a Manager from framework config.
func NewManager(cfg Config, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	redisOpts, err := redisClientOptFromURL(cfg.RedisURL)
	if err != nil {
		return nil, err
	}

	asynqCfg := asynq.Config{
		Concurrency: cfg.Concurrency,
	}
	if asynqCfg.Concurrency <= 0 {
		asynqCfg.Concurrency = 10
	}
	if len(cfg.Queues) > 0 {
		asynqCfg.Queues = cfg.Queues
	}
	if cfg.StrictPriority {
		asynqCfg.StrictPriority = true
	}

	return &Manager{
		logger: logger,
		client: asynq.NewClient(redisOpts),
		server: asynq.NewServer(redisOpts, asynqCfg),
		mux:    asynq.NewServeMux(),
	}, nil
}

// HandleFunc registers a task handler for a given task type.
func (m *Manager) HandleFunc(taskType string, handler asynq.HandlerFunc) error {
	if m == nil {
		return ErrNilManager
	}
	if strings.TrimSpace(taskType) == "" {
		return ErrTaskTypeRequired
	}
	if handler == nil {
		return ErrNilHandler
	}
	m.mux.HandleFunc(taskType, handler)
	return nil
}

// EnqueueJSON marshals payload as JSON and enqueues the task.
func (m *Manager) EnqueueJSON(taskType string, payload any, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if m == nil {
		return nil, ErrNilManager
	}
	task, err := NewJSONTask(taskType, payload)
	if err != nil {
		return nil, err
	}
	info, err := m.client.Enqueue(task, opts...)
	if err != nil {
		return nil, fmt.Errorf("tasks.Manager.EnqueueJSON: %w", err)
	}
	return info, nil
}

// Run starts the worker loop and blocks until shutdown.
func (m *Manager) Run(ctx context.Context) error {
	if m == nil {
		return ErrNilManager
	}
	if ctx == nil {
		ctx = context.Background()
	}

	go func() {
		<-ctx.Done()
		m.server.Shutdown()
	}()

	m.logger.Info("tasks worker starting")
	if err := m.server.Run(m.mux); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("tasks.Manager.Run: %w", err)
	}
	return nil
}

// Close closes client connections and stops worker resources.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	var closeErr error
	m.closeOnce.Do(func() {
		m.server.Shutdown()
		if err := m.client.Close(); err != nil {
			closeErr = fmt.Errorf("tasks.Manager.Close: %w", err)
		}
	})
	return closeErr
}

// NewJSONTask builds an Asynq task with JSON payload.
func NewJSONTask(taskType string, payload any) (*asynq.Task, error) {
	if strings.TrimSpace(taskType) == "" {
		return nil, ErrTaskTypeRequired
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("tasks.NewJSONTask: %w", err)
	}
	return asynq.NewTask(taskType, body), nil
}

func redisClientOptFromURL(raw string) (asynq.RedisClientOpt, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return asynq.RedisClientOpt{}, ErrRedisURLRequired
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return asynq.RedisClientOpt{}, fmt.Errorf("tasks.redisClientOptFromURL parse: %w", err)
	}
	if u.Scheme != "redis" {
		return asynq.RedisClientOpt{}, fmt.Errorf("tasks.redisClientOptFromURL: unsupported scheme %q", u.Scheme)
	}

	addr := u.Host
	if !strings.Contains(addr, ":") {
		addr += ":6379"
	}

	db := 0
	path := strings.TrimPrefix(strings.TrimSpace(u.Path), "/")
	if path != "" {
		v, convErr := strconv.Atoi(path)
		if convErr != nil {
			return asynq.RedisClientOpt{}, fmt.Errorf("tasks.redisClientOptFromURL: invalid db %q", path)
		}
		db = v
	}

	password, _ := u.User.Password()
	return asynq.RedisClientOpt{
		Addr:     addr,
		Password: password,
		DB:       db,
	}, nil
}
