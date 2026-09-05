package memoryprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

var (
	ErrTaskTypeRequired = errors.New("memoryprovider: task type cannot be empty")
	ErrNilHandler       = errors.New("memoryprovider: handler cannot be nil")
	// ErrUnsupportedQueue reports an EnqueuePolicy.Queue this provider has
	// no queue for: it runs a single in-process queue. Naming another one
	// used to be accepted and ignored (NU-12).
	ErrUnsupportedQueue = errors.New("memoryprovider: named queues are not supported (only the default queue exists)")
)

// defaultMaxRetry is what MaxRetry -1 (the DefaultEnqueuePolicy value,
// "provider default") means here. asynq's own default is 25; an in-process
// queue that retries a failing handler twenty-five times only hides it.
const defaultMaxRetry = 3

// retryBackoff is the wait before attempt n (0-based) is retried:
// 100ms, 200ms, 400ms … capped at 5s.
func retryBackoff(attempt int) time.Duration {
	d := 100 * time.Millisecond << uint(attempt)
	if d > 5*time.Second || d <= 0 {
		return 5 * time.Second
	}
	return d
}

type Task struct {
	taskType string
	payload  []byte
}

func (t *Task) Type() string    { return t.taskType }
func (t *Task) Payload() []byte { return t.payload }

type enqueuedTask struct {
	id     string
	task   *Task
	policy tasks.EnqueuePolicy
	ctx    context.Context
}

type Manager struct {
	logger      *slog.Logger
	concurrency int
	handlers    map[string]tasks.HandlerFunc
	mu          sync.RWMutex

	queue   chan enqueuedTask
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	running atomic.Bool

	// Stats
	processed atomic.Int64
	failed    atomic.Int64
	retried   atomic.Int64
}

// Retried reports how many handler attempts were retried after a failure.
func (m *Manager) Retried() int64 { return m.retried.Load() }

func NewManager(cfg tasks.Config, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		logger:      logger,
		concurrency: concurrency,
		handlers:    make(map[string]tasks.HandlerFunc),
		queue:       make(chan enqueuedTask, 10000),
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

func (m *Manager) HandleFunc(taskType string, handler tasks.HandlerFunc) error {
	if taskType == "" {
		return ErrTaskTypeRequired
	}
	if handler == nil {
		return ErrNilHandler
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[taskType] = handler
	return nil
}

func (m *Manager) Run(ctx context.Context) error {
	if !m.running.CompareAndSwap(false, true) {
		return errors.New("memoryprovider: manager is already running")
	}

	for i := 0; i < m.concurrency; i++ {
		m.wg.Add(1)
		go m.worker()
	}

	<-ctx.Done()
	m.Close()
	return nil
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case et := <-m.queue:
			m.mu.RLock()
			handler, ok := m.handlers[et.task.Type()]
			m.mu.RUnlock()

			if !ok {
				m.logger.Error("memoryprovider: no handler for task type", "type", et.task.Type())
				m.failed.Add(1)
				continue
			}

			m.execute(et, handler)
		}
	}
}

// execute runs one task honouring the enqueue policy the caller gave it
// (NU-12): the handler gets a context bounded by Timeout when one is set,
// a failure is retried up to MaxRetry times with exponential backoff, and
// only then is the task counted as failed. A handler that used to fail
// once and be logged now gets the retries the policy asked for.
func (m *Manager) execute(et enqueuedTask, handler tasks.HandlerFunc) {
	base := et.ctx
	if base == nil {
		base = context.Background()
	}
	maxRetry := et.policy.MaxRetry
	if maxRetry < 0 {
		maxRetry = defaultMaxRetry
	}
	for attempt := 0; ; attempt++ {
		ctx, cancel := base, context.CancelFunc(func() {})
		if et.policy.Timeout > 0 {
			ctx, cancel = context.WithTimeout(base, et.policy.Timeout)
		}
		err := handler(ctx, et.task)
		cancel()
		if err == nil {
			m.processed.Add(1)
			return
		}
		if attempt >= maxRetry {
			m.logger.Error("memoryprovider: task failed", "error", err, "type", et.task.Type(), "attempts", attempt+1)
			m.failed.Add(1)
			return
		}
		m.retried.Add(1)
		wait := retryBackoff(attempt)
		m.logger.Warn("memoryprovider: task failed, retrying", "error", err, "type", et.task.Type(), "attempt", attempt+1, "max_retry", maxRetry, "retry_in", wait)
		select {
		case <-time.After(wait):
		case <-m.ctx.Done():
			m.failed.Add(1)
			return
		}
	}
}

func (m *Manager) Close() error {
	m.cancel()
	m.wg.Wait()
	return nil
}

func (m *Manager) EnqueueJSON(taskType string, payload any) (string, error) {
	return m.EnqueueJSONCtxWithPolicy(context.Background(), taskType, payload, tasks.DefaultEnqueuePolicy())
}

func (m *Manager) EnqueueJSONCtx(ctx context.Context, taskType string, payload any) (string, error) {
	return m.EnqueueJSONCtxWithPolicy(ctx, taskType, payload, tasks.DefaultEnqueuePolicy())
}

func (m *Manager) EnqueueJSONWithPolicy(taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	return m.EnqueueJSONCtxWithPolicy(context.Background(), taskType, payload, policy)
}

func (m *Manager) EnqueueJSONCtxWithPolicy(ctx context.Context, taskType string, payload any, policy tasks.EnqueuePolicy) (string, error) {
	if taskType == "" {
		return "", ErrTaskTypeRequired
	}
	// Retention is about stored results, and this provider stores none:
	// there is nothing to retain, so it is documented as a no-op rather
	// than refused. A named queue is a different matter: the caller
	// expects isolation that does not exist.
	if q := policy.Queue; q != "" && q != "default" {
		return "", fmt.Errorf("%w: %q", ErrUnsupportedQueue, q)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	id := uuid.NewString()
	t := &Task{
		taskType: taskType,
		payload:  data,
	}

	et := enqueuedTask{
		id:     id,
		task:   t,
		policy: policy,
		ctx:    ctx,
	}

	if policy.ProcessIn > 0 {
		// A delayed task waits for room instead of being dropped when the
		// queue is full at the moment it comes due (NU-12): the caller was
		// told it was accepted.
		go func() {
			select {
			case <-time.After(policy.ProcessIn):
				select {
				case m.queue <- et:
				case <-m.ctx.Done():
					m.logger.Error("memoryprovider: delayed task dropped at shutdown", "type", taskType)
				}
			case <-m.ctx.Done():
			}
		}()
		return id, nil
	}

	select {
	case m.queue <- et:
		return id, nil
	default:
		return "", errors.New("memoryprovider: queue is full")
	}
}
