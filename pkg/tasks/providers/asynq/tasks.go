// Package tasks provides background job enqueueing and worker runtime backed by Asynq.
package asynqprovider

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
	"time"

	"github.com/hibiken/asynq"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrRedisURLRequired = errors.New("tasks: redis url is required")
	ErrTaskTypeRequired = errors.New("tasks: task type is required")
	ErrNilHandler       = errors.New("tasks: handler is required")
	ErrNilManager       = errors.New("tasks: manager is nil")
	ErrNilTask          = errors.New("tasks: task is nil")
)

const taskCorrelationPayloadKey = "_nucleus_ctx"

var (
	taskTelemetryOnce sync.Once
	taskTracer        trace.Tracer

	taskEnqueueTotal      metric.Int64Counter
	taskEnqueueErrors     metric.Int64Counter
	taskProcessStarted    metric.Int64Counter
	taskProcessSucceeded  metric.Int64Counter
	taskProcessRetried    metric.Int64Counter
	taskProcessFailed     metric.Int64Counter
	taskProcessDurationMs metric.Float64Histogram
)

// Manager owns Asynq client/server instances and task handler registrations.
type Manager struct {
	logger    *slog.Logger
	client    *asynq.Client
	server    *asynq.Server
	mux       *asynq.ServeMux
	closeOnce sync.Once
	// closed is closed by Close so a context-driven Run unblocks when the
	// manager is closed directly rather than via ctx cancellation.
	closed chan struct{}
}

// NewManager initializes a Manager from framework config.
func NewManager(cfg tasks.Config, logger *slog.Logger) (*Manager, error) {
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

	manager := &Manager{
		logger: logger,
		client: asynq.NewClient(redisOpts),
		server: asynq.NewServer(redisOpts, asynqCfg),
		mux:    asynq.NewServeMux(),
		closed: make(chan struct{}),
	}
	manager.mux.Use(taskTelemetryMiddleware(logger))
	return manager, nil
}

// HandleFunc registers a task handler for a given task type.
func (m *Manager) HandleFunc(taskType string, handler tasks.HandlerFunc) error {
	if m == nil {
		return ErrNilManager
	}
	if strings.TrimSpace(taskType) == "" {
		return ErrTaskTypeRequired
	}
	if handler == nil {
		return ErrNilHandler
	}
	m.mux.HandleFunc(taskType, wrapHandler(handler))
	return nil
}

// EnqueueJSON marshals payload as JSON and enqueues the task with default policy.
func (m *Manager) EnqueueJSON(taskType string, payload any) (string, error) {
	return m.EnqueueJSONCtx(context.Background(), taskType, payload)
}

// EnqueueJSONCtx marshals payload as JSON and enqueues the task while preserving context metadata.
func (m *Manager) EnqueueJSONCtx(ctx context.Context, taskType string, payload any) (string, error) {
	return m.EnqueueJSONCtxWithPolicy(ctx, taskType, payload, tasks.DefaultEnqueuePolicy())
}

// EnqueueJSONWithPolicy marshals payload as JSON and enqueues the task with a custom policy.
func (m *Manager) EnqueueJSONWithPolicy(taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	return m.EnqueueJSONCtxWithPolicy(context.Background(), taskType, payload, policy)
}

// EnqueueJSONCtxWithPolicy marshals payload as JSON and enqueues the task with a custom policy while preserving context metadata.
func (m *Manager) EnqueueJSONCtxWithPolicy(ctx context.Context, taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	if m == nil {
		return "", ErrNilManager
	}
	if ctx == nil {
		ctx = context.Background()
	}

	opts := policyToOptions(policy)

	initTaskTelemetry()
	ctx, span := taskTracer.Start(ctx, "task.enqueue "+strings.TrimSpace(taskType), trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	correlation := taskCorrelationFromContext(ctx)
	task, err := newJSONTask(taskType, payload, correlation)
	if err != nil {
		if taskEnqueueErrors != nil {
			taskEnqueueErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("task.type", strings.TrimSpace(taskType))))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	info, err := m.client.Enqueue(task, opts...)
	if err != nil {
		if taskEnqueueErrors != nil {
			taskEnqueueErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("task.type", strings.TrimSpace(taskType))))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", fmt.Errorf("tasks.Manager.EnqueueJSON: %w", err)
	}

	attrs := []attribute.KeyValue{attribute.String("task.type", strings.TrimSpace(taskType))}
	if strings.TrimSpace(info.Queue) != "" {
		attrs = append(attrs, attribute.String("task.queue", info.Queue))
	}
	if taskEnqueueTotal != nil {
		taskEnqueueTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	span.SetAttributes(attrs...)
	if strings.TrimSpace(info.ID) != "" {
		span.SetAttributes(attribute.String("task.id", info.ID))
	}
	span.SetStatus(codes.Ok, "enqueued")

	return info.ID, nil
}

// Run starts the worker loop and blocks until ctx is cancelled or Close is
// called, then shuts the server down gracefully.
//
// It deliberately uses asynq's Start/Shutdown pair rather than Server.Run:
// Server.Run blocks in an OS-signal wait (SIGTERM/SIGINT/SIGTSTP) that an
// external Shutdown call does NOT unblock, so an embedded worker whose
// lifecycle is context-driven — the framework's module-jobs runtime, any
// test — could never be stopped through this API. Surfaced by the first
// in-process execution of module jobs against this provider.
func (m *Manager) Run(ctx context.Context) error {
	if m == nil {
		return ErrNilManager
	}
	if ctx == nil {
		ctx = context.Background()
	}

	m.logger.Info("tasks worker starting")
	if err := m.server.Start(m.mux); err != nil {
		return fmt.Errorf("tasks.Manager.Run: %w", err)
	}
	select {
	case <-ctx.Done():
		m.server.Shutdown()
	case <-m.closed:
		// Close() already shut the server down; nothing further to do.
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
		if m.closed != nil {
			close(m.closed)
		}
		m.server.Shutdown()
		if err := m.client.Close(); err != nil {
			closeErr = fmt.Errorf("tasks.Manager.Close: %w", err)
		}
	})
	return closeErr
}

func newJSONTask(taskType string, payload any, correlation taskCorrelation) (*asynq.Task, error) {
	if strings.TrimSpace(taskType) == "" {
		return nil, ErrTaskTypeRequired
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("tasks.NewJSONTask: %w", err)
	}
	if correlation.hasValues() {
		body, err = injectTaskCorrelation(body, correlation)
		if err != nil {
			return nil, fmt.Errorf("tasks.NewJSONTask correlation: %w", err)
		}
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

type taskCorrelation struct {
	RequestID   string `json:"request_id,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
	TraceParent string `json:"traceparent,omitempty"`
}

func (c taskCorrelation) hasValues() bool {
	return c.RequestID != "" || c.UserID != "" || c.TraceID != "" || c.TraceParent != ""
}

func taskCorrelationFromContext(ctx context.Context) taskCorrelation {
	if ctx == nil {
		return taskCorrelation{}
	}

	meta := taskCorrelation{
		RequestID: observe.RequestIDFromCtx(ctx),
		UserID:    observe.UserIDFromCtx(ctx),
		TraceID:   observe.TraceIDFromCtx(ctx),
	}

	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		if meta.TraceID == "" {
			meta.TraceID = spanCtx.TraceID().String()
		}
		meta.TraceParent = formatTraceparent(spanCtx)
	}

	return meta
}

func applyTaskCorrelationToContext(ctx context.Context, meta taskCorrelation) context.Context {
	if meta.RequestID != "" {
		ctx = observe.CtxWithRequestID(ctx, meta.RequestID)
	}
	if meta.UserID != "" {
		ctx = observe.CtxWithUserID(ctx, meta.UserID)
	}
	if meta.TraceID != "" {
		ctx = observe.CtxWithTraceID(ctx, meta.TraceID)
	}
	return ctx
}

func formatTraceparent(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}

	flags := "00"
	if sc.TraceFlags().IsSampled() {
		flags = "01"
	}
	return "00-" + sc.TraceID().String() + "-" + sc.SpanID().String() + "-" + flags
}

func injectTaskCorrelation(raw []byte, meta taskCorrelation) ([]byte, error) {
	if len(raw) == 0 || !meta.hasValues() {
		return raw, nil
	}

	trimmed := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(trimmed, "{") {
		return raw, nil
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = make(map[string]json.RawMessage)
	}
	if _, exists := payload[taskCorrelationPayloadKey]; exists {
		return raw, nil
	}

	encodedMeta, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	payload[taskCorrelationPayloadKey] = encodedMeta
	return json.Marshal(payload)
}

func extractTaskCorrelation(raw []byte) taskCorrelation {
	if len(raw) == 0 {
		return taskCorrelation{}
	}

	trimmed := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(trimmed, "{") {
		return taskCorrelation{}
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return taskCorrelation{}
	}

	metaRaw, ok := payload[taskCorrelationPayloadKey]
	if !ok {
		return taskCorrelation{}
	}

	var meta taskCorrelation
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return taskCorrelation{}
	}
	return meta
}

func initTaskTelemetry() {
	taskTelemetryOnce.Do(func() {
		meter := otel.Meter("nucleus/tasks")
		taskTracer = otel.Tracer("nucleus/tasks")

		taskEnqueueTotal, _ = meter.Int64Counter("jobs.enqueue.total")
		taskEnqueueErrors, _ = meter.Int64Counter("jobs.enqueue.errors")
		taskProcessStarted, _ = meter.Int64Counter("jobs.process.started")
		taskProcessSucceeded, _ = meter.Int64Counter("jobs.process.succeeded")
		taskProcessRetried, _ = meter.Int64Counter("jobs.process.retried")
		taskProcessFailed, _ = meter.Int64Counter("jobs.process.failed")
		taskProcessDurationMs, _ = meter.Float64Histogram("jobs.process.duration.ms")
	})
}

func taskTelemetryMiddleware(logger *slog.Logger) asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			if ctx == nil {
				ctx = context.Background()
			}
			if next == nil {
				return ErrNilHandler
			}

			initTaskTelemetry()

			meta := extractTaskCorrelation(task.Payload())
			if meta.TraceParent != "" {
				ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{
					"traceparent": meta.TraceParent,
				})
			}
			ctx = applyTaskCorrelationToContext(ctx, meta)

			queueName, _ := asynq.GetQueueName(ctx)
			taskID, _ := asynq.GetTaskID(ctx)
			retryCount, retryOK := asynq.GetRetryCount(ctx)
			maxRetry, maxOK := asynq.GetMaxRetry(ctx)

			attrs := make([]attribute.KeyValue, 0, 4)
			attrs = append(attrs, attribute.String("task.type", task.Type()))
			if queueName != "" {
				attrs = append(attrs, attribute.String("task.queue", queueName))
			}
			if taskID != "" {
				attrs = append(attrs, attribute.String("task.id", taskID))
			}
			if retryOK {
				attrs = append(attrs, attribute.Int("task.retry_count", retryCount))
			}
			if maxOK {
				attrs = append(attrs, attribute.Int("task.max_retry", maxRetry))
			}
			if meta.RequestID != "" {
				attrs = append(attrs, attribute.String("request.id", meta.RequestID))
			}
			if meta.UserID != "" {
				attrs = append(attrs, attribute.String("user.id", meta.UserID))
			}
			if meta.TraceID != "" {
				attrs = append(attrs, attribute.String("request.trace_id", meta.TraceID))
			}

			ctx, span := taskTracer.Start(ctx, "task.process "+task.Type(), trace.WithSpanKind(trace.SpanKindConsumer))
			defer span.End()
			span.SetAttributes(attrs...)
			if observe.TraceIDFromCtx(ctx) == "" && span.SpanContext().TraceID().IsValid() {
				ctx = observe.CtxWithTraceID(ctx, span.SpanContext().TraceID().String())
			}

			metricAttrs := []attribute.KeyValue{attribute.String("task.type", task.Type())}
			if queueName != "" {
				metricAttrs = append(metricAttrs, attribute.String("task.queue", queueName))
			}
			if taskProcessStarted != nil {
				taskProcessStarted.Add(ctx, 1, metric.WithAttributes(metricAttrs...))
			}

			start := time.Now()
			err := next.ProcessTask(ctx, task)
			durationMs := float64(time.Since(start).Nanoseconds()) / 1e6
			outcome := classifyTaskOutcome(err, retryCount, maxRetry, retryOK, maxOK)
			outcomeAttrs := append(metricAttrs, attribute.String("job.outcome", outcome))

			if taskProcessDurationMs != nil {
				taskProcessDurationMs.Record(ctx, durationMs, metric.WithAttributes(outcomeAttrs...))
			}
			switch outcome {
			case "success":
				if taskProcessSucceeded != nil {
					taskProcessSucceeded.Add(ctx, 1, metric.WithAttributes(metricAttrs...))
				}
			case "retry":
				if taskProcessRetried != nil {
					taskProcessRetried.Add(ctx, 1, metric.WithAttributes(metricAttrs...))
				}
			default:
				if taskProcessFailed != nil {
					taskProcessFailed.Add(ctx, 1, metric.WithAttributes(metricAttrs...))
				}
			}

			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, outcome)
				if logger != nil {
					observe.WithContext(ctx, logger).Warn("task processing finished with error",
						"type", task.Type(),
						"queue", queueName,
						"task_id", taskID,
						"outcome", outcome,
						"duration_ms", durationMs,
						"error", err.Error(),
					)
				}
				return err
			}

			span.SetStatus(codes.Ok, "success")
			if logger != nil {
				observe.WithContext(ctx, logger).Debug("task processed",
					"type", task.Type(),
					"queue", queueName,
					"task_id", taskID,
					"duration_ms", durationMs,
				)
			}
			return nil
		})
	}
}

func classifyTaskOutcome(err error, retryCount, maxRetry int, retryCountKnown, maxRetryKnown bool) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, asynq.SkipRetry) || errors.Is(err, asynq.RevokeTask) {
		return "failure"
	}
	if retryCountKnown && maxRetryKnown && retryCount >= maxRetry {
		return "failure"
	}
	return "retry"
}
