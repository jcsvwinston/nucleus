package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

var (
	ErrNilHandler       = fmt.Errorf("tasks: handler is nil")
	ErrNilManager       = fmt.Errorf("tasks: manager is nil")
	ErrRedisURLRequired = fmt.Errorf("tasks: redis_url is required")
	ErrTaskTypeRequired = fmt.Errorf("tasks: task type is required")
)

// Task represents a generic unit of work.
type Task interface {
	Type() string
	Payload() []byte
}

// HandlerFunc is a function that processes a Task.
type HandlerFunc func(ctx context.Context, task Task) error

// EnqueuePolicy describes the supported explicit enqueue-policy subset.
// MaxRetry uses -1 to keep provider defaults; 0 disables retries.
type EnqueuePolicy struct {
	Queue     string
	MaxRetry  int
	Timeout   time.Duration
	ProcessIn time.Duration
	Retention time.Duration
}

func DefaultEnqueuePolicy() EnqueuePolicy {
	return EnqueuePolicy{
		MaxRetry: -1,
	}
}

// Manager is the unified interface for a Task Queue Provider.
type Manager interface {
	// Worker methods
	HandleFunc(taskType string, handler HandlerFunc) error
	Run(ctx context.Context) error
	Close() error

	// Client methods
	EnqueueJSON(taskType string, payload any) (string, error)
	EnqueueJSONCtx(ctx context.Context, taskType string, payload any) (string, error)
	EnqueueJSONWithPolicy(taskType string, payload any, policy EnqueuePolicy) (string, error)
	EnqueueJSONCtxWithPolicy(ctx context.Context, taskType string, payload any, policy EnqueuePolicy) (string, error)
}

// Config configures task enqueueing and worker runtime.
type Config struct {
	RedisURL       string         // Used if Provider needs Redis (e.g. Asynq)
	Concurrency    int            // Number of concurrent workers
	Queues         map[string]int // Queue names and their priority weights
	StrictPriority bool           // If true, strict priority ordering is enforced
}

// DecodeJSONPayload unmarshals a generic JSON task payload into dst.
func DecodeJSONPayload(task Task, dst any) error {
	if task == nil {
		return fmt.Errorf("tasks: task is nil")
	}
	if err := json.Unmarshal(task.Payload(), dst); err != nil {
		return fmt.Errorf("tasks.DecodeJSONPayload: %w", err)
	}
	return nil
}

// Inspector defines an interface for queue introspection and operations.
type Inspector interface {
	InspectRuntime() RuntimeSnapshot
	OperateQueue(queue, action string) (QueueActionResult, error)
}

// Scheduler provides an interface for periodic background tasks.
type Scheduler interface {
	RegisterJSON(spec, taskType string, payload any, policy EnqueuePolicy) (string, error)
	Unregister(entryID string) error
	Start() error
	Close() error
}
