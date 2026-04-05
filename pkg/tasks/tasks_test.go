package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/jcsvwinston/GoFrame/pkg/observe"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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
	task, err := NewJSONTask("emails.send_welcome", map[string]string{"email": "alice@example.com"})
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
	_, err := NewJSONTask("", map[string]string{"x": "1"})
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
