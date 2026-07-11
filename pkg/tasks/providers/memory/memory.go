package memoryprovider

import (
	"context"
	"encoding/json"
	"errors"
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
)

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
}

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

			ctx := et.ctx
			if ctx == nil {
				ctx = context.Background()
			}

			err := handler(ctx, et.task)
			if err != nil {
				m.logger.Error("memoryprovider: task failed", "error", err, "type", et.task.Type())
				m.failed.Add(1)
			} else {
				m.processed.Add(1)
			}
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
		go func() {
			select {
			case <-time.After(policy.ProcessIn):
				select {
				case m.queue <- et:
				default:
					m.logger.Error("memoryprovider: queue is full for delayed task", "type", taskType)
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
