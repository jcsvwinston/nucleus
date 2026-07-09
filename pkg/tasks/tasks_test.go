package tasks

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestDefaultEnqueuePolicy(t *testing.T) {
	policy := DefaultEnqueuePolicy()
	if policy.MaxRetry != -1 {
		t.Errorf("Expected MaxRetry=-1, got %d", policy.MaxRetry)
	}
}

func TestDecodeJSONPayload(t *testing.T) {
	t.Run("valid payload", func(t *testing.T) {
		payload := map[string]string{"key": "value"}
		data, _ := json.Marshal(payload)
		
		task := &mockTask{payload: data}
		var result map[string]string
		err := DecodeJSONPayload(task, &result)
		if err != nil {
			t.Fatalf("DecodeJSONPayload failed: %v", err)
		}
		if result["key"] != "value" {
			t.Errorf("Expected key=value, got key=%s", result["key"])
		}
	})

	t.Run("nil task", func(t *testing.T) {
		var result map[string]string
		err := DecodeJSONPayload(nil, &result)
		if err == nil {
			t.Error("Expected error for nil task")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		task := &mockTask{payload: []byte("{invalid json}")}
		var result map[string]string
		err := DecodeJSONPayload(task, &result)
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})
}

func TestEnqueuePolicy(t *testing.T) {
	policy := EnqueuePolicy{
		Queue:     "test-queue",
		MaxRetry:  3,
		Timeout:   30 * time.Second,
		ProcessIn: 5 * time.Minute,
		Retention: 24 * time.Hour,
	}

	if policy.Queue != "test-queue" {
		t.Errorf("Expected test-queue, got %s", policy.Queue)
	}
	if policy.MaxRetry != 3 {
		t.Errorf("Expected MaxRetry=3, got %d", policy.MaxRetry)
	}
	if policy.Timeout != 30*time.Second {
		t.Errorf("Expected 30s timeout, got %v", policy.Timeout)
	}
}

func TestConfig(t *testing.T) {
	config := Config{
		RedisURL:       "redis://localhost:6379",
		Concurrency:    10,
		Queues:         map[string]int{"default": 1, "high": 5},
		StrictPriority: true,
	}

	if config.RedisURL != "redis://localhost:6379" {
		t.Errorf("Expected redis://localhost:6379, got %s", config.RedisURL)
	}
	if config.Concurrency != 10 {
		t.Errorf("Expected Concurrency=10, got %d", config.Concurrency)
	}
	if len(config.Queues) != 2 {
		t.Errorf("Expected 2 queues, got %d", len(config.Queues))
	}
	if !config.StrictPriority {
		t.Error("Expected StrictPriority=true")
	}
}

// mockTask implements Task interface for testing
type mockTask struct {
	payload []byte
	taskType string
}

func (m *mockTask) Type() string {
	if m.taskType != "" {
		return m.taskType
	}
	return "test-task"
}

func (m *mockTask) Payload() []byte {
	return m.payload
}

func TestManagerInterface(t *testing.T) {
	// This test just verifies the Manager interface is defined correctly
	// Actual implementations are tested in provider-specific tests
	var _ Manager = (*mockManager)(nil)
}

func TestInspectorInterface(t *testing.T) {
	// This test just verifies the Inspector interface is defined correctly
	var _ Inspector = (*mockInspector)(nil)
}

func TestSchedulerInterface(t *testing.T) {
	// This test just verifies the Scheduler interface is defined correctly
	var _ Scheduler = (*mockScheduler)(nil)
}

// mockManager implements Manager for testing
type mockManager struct{}

func (m *mockManager) HandleFunc(taskType string, handler HandlerFunc) error {
	return nil
}

func (m *mockManager) Run(ctx context.Context) error {
	return nil
}

func (m *mockManager) Close() error {
	return nil
}

func (m *mockManager) EnqueueJSON(taskType string, payload any) (string, error) {
	return "task-id", nil
}

func (m *mockManager) EnqueueJSONCtx(ctx context.Context, taskType string, payload any) (string, error) {
	return "task-id", nil
}

func (m *mockManager) EnqueueJSONWithPolicy(taskType string, payload any, policy EnqueuePolicy) (string, error) {
	return "task-id", nil
}

func (m *mockManager) EnqueueJSONCtxWithPolicy(ctx context.Context, taskType string, payload any, policy EnqueuePolicy) (string, error) {
	return "task-id", nil
}

// mockInspector implements Inspector for testing
type mockInspector struct{}

func (m *mockInspector) InspectRuntime() RuntimeSnapshot {
	return RuntimeSnapshot{}
}

func (m *mockInspector) OperateQueue(queue, action string) (QueueActionResult, error) {
	return QueueActionResult{}, nil
}

// mockScheduler implements Scheduler for testing
type mockScheduler struct{}

func (m *mockScheduler) RegisterJSON(spec, taskType string, payload any, policy EnqueuePolicy) (string, error) {
	return "schedule-id", nil
}

func (m *mockScheduler) Unregister(entryID string) error {
	return nil
}

func (m *mockScheduler) Start() error {
	return nil
}

func (m *mockScheduler) Close() error {
	return nil
}
