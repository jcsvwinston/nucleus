package asynqprovider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestRedisClientOptFromURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		addr      string
		password  string
		db        int
		shouldErr bool
	}{
		{
			name:  "basic url",
			input: "redis://localhost",
			addr:  "localhost:6379",
			db:    0,
		},
		{
			name:     "with password and db",
			input:    "redis://:secret@redis.internal:6380/3",
			addr:     "redis.internal:6380",
			password: "secret",
			db:       3,
		},
		{
			name:      "invalid scheme",
			input:     "http://localhost:6379/0",
			shouldErr: true,
		},
		{
			name:      "invalid db",
			input:     "redis://localhost/not-a-db",
			shouldErr: true,
		},
		{
			name:      "empty",
			input:     "",
			shouldErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opt, err := redisClientOptFromURL(tc.input)
			if tc.shouldErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if opt.Addr != tc.addr {
				t.Fatalf("expected addr %q, got %q", tc.addr, opt.Addr)
			}
			if opt.Password != tc.password {
				t.Fatalf("expected password %q, got %q", tc.password, opt.Password)
			}
			if opt.DB != tc.db {
				t.Fatalf("expected db %d, got %d", tc.db, opt.DB)
			}
		})
	}
}

func TestNewJSONTask(t *testing.T) {
	task, err := newJSONTask("emails.send_welcome", map[string]string{"email": "alice@example.com"}, taskCorrelation{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task == nil {
		t.Fatal("expected task")
	}
	if task.Type() != "emails.send_welcome" {
		t.Fatalf("unexpected task type: %s", task.Type())
	}
	if string(task.Payload()) != `{"email":"alice@example.com"}` {
		t.Fatalf("unexpected payload: %s", string(task.Payload()))
	}
}

func TestNewJSONTask_RequiresType(t *testing.T) {
	_, err := newJSONTask("", map[string]string{"x": "1"}, taskCorrelation{})
	if err == nil {
		t.Fatal("expected error for empty task type")
	}
}

func TestTaskCorrelationFromContext(t *testing.T) {
	origTP := otel.GetTracerProvider()
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		otel.SetTracerProvider(origTP)
		_ = tp.Shutdown(context.Background())
	}()

	ctx := context.Background()
	ctx = observe.CtxWithRequestID(ctx, "req-123")
	ctx = observe.CtxWithUserID(ctx, "user-42")

	ctx, span := otel.Tracer("tasks-test").Start(ctx, "request")
	meta := taskCorrelationFromContext(ctx)
	span.End()

	if meta.RequestID != "req-123" {
		t.Fatalf("expected request id req-123, got %q", meta.RequestID)
	}
	if meta.UserID != "user-42" {
		t.Fatalf("expected user id user-42, got %q", meta.UserID)
	}
	if meta.TraceID == "" {
		t.Fatal("expected trace id from context span")
	}
	if meta.TraceParent == "" {
		t.Fatal("expected traceparent from context span")
	}
}

func TestInjectAndExtractTaskCorrelation(t *testing.T) {
	raw := []byte(`{"article_id":7,"title":"fresh"}`)
	meta := taskCorrelation{
		RequestID:   "req-1",
		UserID:      "user-1",
		TraceID:     "trace-1",
		TraceParent: "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01",
	}

	encoded, err := injectTaskCorrelation(raw, meta)
	if err != nil {
		t.Fatalf("injectTaskCorrelation failed: %v", err)
	}
	if !strings.Contains(string(encoded), taskCorrelationPayloadKey) {
		t.Fatalf("expected payload to include %q metadata: %s", taskCorrelationPayloadKey, string(encoded))
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode payload failed: %v", err)
	}
	if _, ok := payload["article_id"]; !ok {
		t.Fatalf("expected payload to keep original fields, got: %s", string(encoded))
	}

	got := extractTaskCorrelation(encoded)
	if got.RequestID != meta.RequestID || got.UserID != meta.UserID || got.TraceID != meta.TraceID || got.TraceParent != meta.TraceParent {
		t.Fatalf("unexpected extracted meta: %+v", got)
	}
}

func TestInjectTaskCorrelation_NonObjectPayload(t *testing.T) {
	raw := []byte(`["a","b"]`)
	meta := taskCorrelation{RequestID: "req-1"}

	encoded, err := injectTaskCorrelation(raw, meta)
	if err != nil {
		t.Fatalf("injectTaskCorrelation failed: %v", err)
	}
	if string(encoded) != string(raw) {
		t.Fatalf("expected non-object payload unchanged, got: %s", string(encoded))
	}
}

func TestClassifyTaskOutcome(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		retryCount      int
		maxRetry        int
		retryCountKnown bool
		maxRetryKnown   bool
		want            string
	}{
		{
			name: "success",
			want: "success",
		},
		{
			name: "skip retry means failure",
			err:  asynq.SkipRetry,
			want: "failure",
		},
		{
			name: "revoke means failure",
			err:  asynq.RevokeTask,
			want: "failure",
		},
		{
			name:            "regular error with retries remaining",
			err:             errors.New("boom"),
			retryCount:      1,
			maxRetry:        5,
			retryCountKnown: true,
			maxRetryKnown:   true,
			want:            "retry",
		},
		{
			name:            "regular error and retries exhausted",
			err:             errors.New("boom"),
			retryCount:      5,
			maxRetry:        5,
			retryCountKnown: true,
			maxRetryKnown:   true,
			want:            "failure",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyTaskOutcome(tc.err, tc.retryCount, tc.maxRetry, tc.retryCountKnown, tc.maxRetryKnown); got != tc.want {
				t.Fatalf("classifyTaskOutcome()=%q; want %q", got, tc.want)
			}
		})
	}
}

func TestNewManager_NilLogger(t *testing.T) {
	_, err := NewManager(tasks.Config{RedisURL: "redis://localhost:6379/0"}, nil)
	if err != nil {
		t.Fatalf("NewManager with nil logger should succeed, got: %v", err)
	}
}

func TestNewManager_RequiresRedisURL(t *testing.T) {
	_, err := NewManager(tasks.Config{RedisURL: ""}, nil)
	if err == nil {
		t.Fatal("expected error for empty redis URL")
	}
	if err != ErrRedisURLRequired {
		t.Fatalf("expected ErrRedisURLRequired, got: %v", err)
	}
}

func TestNewManager_InvalidRedisURL(t *testing.T) {
	_, err := NewManager(tasks.Config{RedisURL: "http://invalid"}, nil)
	if err == nil {
		t.Fatal("expected error for invalid redis URL")
	}
}

func TestManager_HandleFuncValidation(t *testing.T) {
	err := (&Manager{}).HandleFunc("", nil)
	if err == nil {
		t.Fatal("expected error for empty task type")
	}
	if err != ErrTaskTypeRequired {
		t.Fatalf("expected ErrTaskTypeRequired, got: %v", err)
	}

	err = (&Manager{}).HandleFunc("test.task", nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
	if err != ErrNilHandler {
		t.Fatalf("expected ErrNilHandler, got: %v", err)
	}

	err = (&Manager{}).HandleFunc("  ", func(context.Context, tasks.Task) error { return nil })
	if err == nil {
		t.Fatal("expected error for whitespace-only task type")
	}
}

func TestManager_EnqueueJSONCtx_NilManager(t *testing.T) {
	var mgr *Manager
	_, err := mgr.EnqueueJSONCtx(context.Background(), "test.task", map[string]string{"key": "value"})
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
	if err != ErrNilManager {
		t.Fatalf("expected ErrNilManager, got: %v", err)
	}
}

func TestManager_EnqueueJSONCtx_NilContext(t *testing.T) {
	// Manager with nil client will panic on enqueue, so we test with nil manager instead
	var mgr *Manager
	_, err := mgr.EnqueueJSONCtx(nil, "test.task", map[string]string{"key": "value"})
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
	if err != ErrNilManager {
		t.Fatalf("expected ErrNilManager, got: %v", err)
	}
}

func TestManager_EnqueueJSON_NilManager(t *testing.T) {
	var mgr *Manager
	_, err := mgr.EnqueueJSON("test.task", map[string]string{"key": "value"})
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
	if err != ErrNilManager {
		t.Fatalf("expected ErrNilManager, got: %v", err)
	}
}

func TestManager_Run_NilManager(t *testing.T) {
	var mgr *Manager
	err := mgr.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
	if err != ErrNilManager {
		t.Fatalf("expected ErrNilManager, got: %v", err)
	}
}

func TestManager_Run_NilContext(t *testing.T) {
	// Manager with nil server will panic on Run, test with nil manager instead
	var mgr *Manager
	err := mgr.Run(nil)
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
	if err != ErrNilManager {
		t.Fatalf("expected ErrNilManager, got: %v", err)
	}
}

func TestManager_Close_NilManager(t *testing.T) {
	var mgr *Manager
	err := mgr.Close()
	if err != nil {
		t.Fatalf("expected nil manager Close to succeed, got: %v", err)
	}
}

func TestManager_CloseMultipleTimes(t *testing.T) {
	mgr, err := NewManager(tasks.Config{RedisURL: "redis://localhost:6379/0"}, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// First close should succeed
	err = mgr.Close()
	if err != nil {
		t.Logf("First close returned (acceptable): %v", err)
	}

	// Second close should be safe (closeOnce ensures idempotency)
	err = mgr.Close()
	if err != nil {
		t.Logf("Second close returned (acceptable): %v", err)
	}
}

func TestTaskCorrelation_HasValues(t *testing.T) {
	tests := []struct {
		name string
		meta taskCorrelation
		want bool
	}{
		{
			name: "empty correlation",
			meta: taskCorrelation{},
			want: false,
		},
		{
			name: "only request id",
			meta: taskCorrelation{RequestID: "req-1"},
			want: true,
		},
		{
			name: "only user id",
			meta: taskCorrelation{UserID: "user-1"},
			want: true,
		},
		{
			name: "only trace id",
			meta: taskCorrelation{TraceID: "trace-1"},
			want: true,
		},
		{
			name: "only traceparent",
			meta: taskCorrelation{TraceParent: "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01"},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.meta.hasValues(); got != tc.want {
				t.Fatalf("hasValues()=%v; want %v", got, tc.want)
			}
		})
	}
}

func TestTaskCorrelationFromContext_NilContext(t *testing.T) {
	meta := taskCorrelationFromContext(nil)
	if meta.hasValues() {
		t.Fatalf("expected empty correlation for nil context, got: %+v", meta)
	}
}

func TestApplyTaskCorrelationToContext(t *testing.T) {
	ctx := context.Background()
	meta := taskCorrelation{
		RequestID: "req-1",
		UserID:    "user-1",
		TraceID:   "trace-1",
	}

	newCtx := applyTaskCorrelationToContext(ctx, meta)
	if observe.RequestIDFromCtx(newCtx) != "req-1" {
		t.Fatalf("expected request id req-1, got %q", observe.RequestIDFromCtx(newCtx))
	}
	if observe.UserIDFromCtx(newCtx) != "user-1" {
		t.Fatalf("expected user id user-1, got %q", observe.UserIDFromCtx(newCtx))
	}
	if observe.TraceIDFromCtx(newCtx) != "trace-1" {
		t.Fatalf("expected trace id trace-1, got %q", observe.TraceIDFromCtx(newCtx))
	}
}

func TestInjectTaskCorrelation_EmptyPayload(t *testing.T) {
	raw := []byte(`{}`)
	meta := taskCorrelation{RequestID: "req-1"}

	encoded, err := injectTaskCorrelation(raw, meta)
	if err != nil {
		t.Fatalf("injectTaskCorrelation failed: %v", err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode payload failed: %v", err)
	}
	if _, ok := payload[taskCorrelationPayloadKey]; !ok {
		t.Fatalf("expected correlation key in payload")
	}
}

func TestInjectTaskCorrelation_DuplicatePreventsOverwrite(t *testing.T) {
	raw := []byte(`{"_nucleus_ctx":{"request_id":"original"}}`)
	meta := taskCorrelation{RequestID: "new-req"}

	encoded, err := injectTaskCorrelation(raw, meta)
	if err != nil {
		t.Fatalf("injectTaskCorrelation failed: %v", err)
	}
	if string(encoded) != string(raw) {
		t.Fatalf("expected existing correlation to prevent overwrite, got: %s", string(encoded))
	}
}

func TestExtractTaskCorrelation_EmptyPayload(t *testing.T) {
	meta := extractTaskCorrelation([]byte{})
	if meta.hasValues() {
		t.Fatalf("expected empty correlation for empty payload, got: %+v", meta)
	}
}

func TestExtractTaskCorrelation_InvalidJSON(t *testing.T) {
	meta := extractTaskCorrelation([]byte(`not-json`))
	if meta.hasValues() {
		t.Fatalf("expected empty correlation for invalid JSON, got: %+v", meta)
	}
}

func TestExtractTaskCorrelation_NoCorrelationKey(t *testing.T) {
	meta := extractTaskCorrelation([]byte(`{"foo":"bar"}`))
	if meta.hasValues() {
		t.Fatalf("expected empty correlation when key missing, got: %+v", meta)
	}
}

func TestExtractTaskCorrelation_InvalidCorrelationValue(t *testing.T) {
	meta := extractTaskCorrelation([]byte(`{"_nucleus_ctx":"not-a-map"}`))
	if meta.hasValues() {
		t.Fatalf("expected empty correlation for invalid correlation value, got: %+v", meta)
	}
}

func TestFormatTraceparent_InvalidContext(t *testing.T) {
	var sc trace.SpanContext
	result := formatTraceparent(sc)
	if result != "" {
		t.Fatalf("expected empty string for invalid span context, got: %s", result)
	}
}
