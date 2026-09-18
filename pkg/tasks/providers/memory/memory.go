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
	// counted is true for a job that has already been counted as failed once
	// — it comes back from the hold store. TotalFailed counts JOBS that
	// failed, not dispatch attempts, so requeueing a held job must not
	// inflate it every time an operator presses the button.
	counted bool
}

type Manager struct {
	logger      *slog.Logger
	concurrency int
	// holds keeps the jobs this provider would otherwise drop: the ones no
	// handler claims yet, and the ones that died. See hold.go.
	holds    *holdStore
	handlers map[string]tasks.HandlerFunc
	mu       sync.RWMutex

	queue chan enqueuedTask
	// lifecycle serializes starting the workers against stopping them: a
	// WaitGroup whose Add runs concurrently with another goroutine's Wait is
	// a data race, and `go Run(ctx)` followed by Close() is exactly that.
	lifecycle sync.Mutex
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	running   atomic.Bool

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
		holds:       newHoldStore(defaultHoldCapacity),
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
	m.handlers[taskType] = handler
	m.mu.Unlock()

	// A handler arriving is what the jobs held for this type were waiting
	// for: a worker deployed after its producer stops losing the work that
	// was enqueued in between.
	m.releaseWaiting(taskType)
	return nil
}

func (m *Manager) Run(ctx context.Context) error {
	if !m.running.CompareAndSwap(false, true) {
		return errors.New("memoryprovider: manager is already running")
	}

	m.lifecycle.Lock()
	if m.ctx.Err() != nil {
		// Already stopped: starting workers now would add them behind a Close
		// that has already waited.
		m.lifecycle.Unlock()
		return ErrManagerStopped
	}
	for i := 0; i < m.concurrency; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	m.lifecycle.Unlock()

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
				m.holdUnhandled(et)
				continue
			}

			m.execute(et, handler)
		}
	}
}

// holdUnhandled keeps a job whose type nobody handles in this process, instead
// of counting it failed and dropping it (NU-80). A producer deployed ahead of
// its consumer, or a mistyped task type, used to lose the work outright.
//
// It closes the lost-wakeup race with HandleFunc by re-checking the handler
// map AFTER the job is in the store: a handler registered in the window
// between the failed lookup and the hold would have found the store empty, and
// the job would have waited for a consumer that had already arrived. Taking it
// back out is atomic, so the registering side and this one cannot both
// dispatch the same job.
//
// The counter is still incremented, and deliberately: TotalFailed is a
// published, frozen field (total_failed_today). An operator may have an alert
// hanging off it, and this change is about not LOSING the job — not about
// quietly moving a number they already watch.
func (m *Manager) holdUnhandled(et enqueuedTask) {
	job := heldJob{
		id:      et.id,
		task:    et.task,
		policy:  et.policy,
		ctx:     detachContext(et.ctx),
		reason:  reasonNoHandler,
		counted: true,
		heldAt:  time.Now(),
	}
	// Held BEFORE the counter moves: a reader that sees the failure must be
	// able to find the job. InspectRuntime reads the counters first for the
	// same reason — see inspector.go.
	if evicted := m.holds.hold(kindWaiting, job); evicted != nil {
		m.logger.Error("memoryprovider: waiting job evicted, the store is full",
			"type", evicted.task.Type(), "id", evicted.id, "capacity", m.holds.capacity)
	}
	if !et.counted {
		m.failed.Add(1)
	}
	m.logger.Warn("memoryprovider: no handler for task type, job held until one registers",
		"type", et.task.Type(), "id", et.id)

	// The handler may have arrived while this job was being put away.
	m.mu.RLock()
	_, ok := m.handlers[et.task.Type()]
	m.mu.RUnlock()
	if ok {
		m.releaseWaiting(et.task.Type())
	}
}

// releaseWaiting hands every job waiting for taskType back to the queue. A job
// is taken out of the store first and only stays out if it reaches the queue:
// if the queue is full or the manager is stopping, it goes back where it was,
// because dropping it here is the failure this session exists to remove.
func (m *Manager) releaseWaiting(taskType string) int {
	jobs := m.holds.takeByType(kindWaiting, taskType)
	if len(jobs) == 0 {
		return 0
	}
	requeued, returned := m.requeue(jobs)
	if evicted := m.holds.putBack(kindWaiting, returned); evicted > 0 {
		m.logger.Error("memoryprovider: held jobs evicted while being put back",
			"type", taskType, "evicted", evicted, "capacity", m.holds.capacity)
	}
	if requeued > 0 {
		m.logger.Info("memoryprovider: handler registered, held jobs requeued",
			"type", taskType, "requeued", requeued, "still_held", len(returned))
	}
	if len(returned) > 0 {
		// The queue had no room. They stay held, and the way back is the
		// retry-archived queue action — nothing retries this on its own.
		m.logger.Warn("memoryprovider: held jobs could not be requeued, the queue is full",
			"type", taskType, "still_held", len(returned))
	}
	return requeued
}

// requeue puts held jobs back on the queue. It returns how many made it and
// which ones did not, so the caller can hold those rather than lose them.
func (m *Manager) requeue(jobs []heldJob) (requeued int, returned []heldJob) {
	for i, job := range jobs {
		if m.ctx.Err() != nil {
			// The manager is stopping: a job pushed now goes onto a channel
			// nobody will read again.
			return requeued, append(returned, jobs[i:]...)
		}
		et := enqueuedTask{
			id:     job.id,
			task:   job.task,
			policy: job.policy,
			// The held context lost its cancellation so it could outlive the
			// request that enqueued it; tie the requeued one to the manager,
			// so a job put back can still be stopped when the process stops.
			ctx:     m.tieToManager(job.ctx),
			counted: job.counted,
		}
		select {
		case m.queue <- et:
			requeued++
		default:
			return requeued, append(returned, jobs[i:]...)
		}
	}
	return requeued, returned
}

// holdDead keeps a job that will not run again in this process, with the
// reason and what the last attempt said.
func (m *Manager) holdDead(et enqueuedTask, reason holdReason, attempts int, err error) {
	job := heldJob{
		id:       et.id,
		task:     et.task,
		policy:   et.policy,
		ctx:      detachContext(et.ctx),
		reason:   reason,
		counted:  true,
		attempts: attempts,
		heldAt:   time.Now(),
	}
	if err != nil {
		job.lastErr = err.Error()
	}
	if evicted := m.holds.hold(kindDead, job); evicted != nil {
		m.logger.Error("memoryprovider: dead job evicted, the dead letter is full",
			"type", evicted.task.Type(), "id", evicted.id, "capacity", m.holds.capacity)
	}
}

// tieToManager returns a context with the given one's values that is also
// cancelled when the manager stops. Held jobs deliberately have no
// cancellation of their own (see detachContext); without this, a requeued job
// would be the one job in the provider that nothing can stop.
func (m *Manager) tieToManager(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	tied, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	// Release the AfterFunc registration when the job's context is done, so a
	// long-lived manager does not accumulate one per requeued job.
	context.AfterFunc(tied, func() { stop() })
	return tied
}

// detachContext keeps the VALUES of the context a job was enqueued with and
// drops its cancellation. The values matter: this framework carries the
// request scope — site, tenant, database alias — in them, so a job requeued
// with a background context would run against the wrong tenant. The
// cancellation must go: the request that enqueued the job is over.
func detachContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
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
			// Held before the counter moves (see holdUnhandled).
			m.holdDead(et, reasonRetriesExhausted, attempt+1, err)
			m.logger.Error("memoryprovider: task failed, job held in the dead letter",
				"error", err, "type", et.task.Type(), "id", et.id, "attempts", attempt+1)
			if !et.counted {
				m.failed.Add(1)
			}
			return
		}
		m.retried.Add(1)
		wait := retryBackoff(attempt)
		m.logger.Warn("memoryprovider: task failed, retrying", "error", err, "type", et.task.Type(), "attempt", attempt+1, "max_retry", maxRetry, "retry_in", wait)
		select {
		case <-time.After(wait):
		case <-m.ctx.Done():
			// Cancelled between attempts: the job had retries left and never
			// got them. It is held with that reason rather than counted as a
			// failure nobody can look at.
			m.holdDead(et, reasonShutdown, attempt+1, err)
			if !et.counted {
				m.failed.Add(1)
			}
			return
		}
	}
}

func (m *Manager) Close() error {
	m.cancel()
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.wg.Wait()
	m.drain()
	return nil
}

// drain empties what is left in the channel into the dead letter, after the
// workers have stopped, so a shutdown does not silently swallow jobs the
// caller was told had been accepted.
//
// It is a RECEIPT, not durability: this store dies with the process, and the
// durable queue is what A7's next session brings. What it buys today is that
// the loss is counted and named instead of invisible. The store is bounded,
// so a full channel (10 000) overflows a 1 000-slot store; the log says how
// many were kept and how many the bound pushed out, rather than implying the
// numbers are the same.
//
// Delayed jobs (ProcessIn) are not in the channel yet, so they are not drained
// here: each one holds itself when the manager stops, from the goroutine that
// is waiting out its delay.
func (m *Manager) drain() {
	kept, evicted := 0, 0
	for {
		select {
		case et := <-m.queue:
			before := m.holds.evictedCount(kindDead)
			m.holdDead(et, reasonQueueClosed, 0, nil)
			if m.holds.evictedCount(kindDead) > before {
				evicted++
			}
			kept++
		default:
			if kept > 0 {
				m.logger.Warn("memoryprovider: manager stopped with jobs still queued",
					"held", kept-evicted, "evicted", evicted, "capacity", m.holds.capacity)
			}
			return
		}
	}
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

	if m.ctx.Err() != nil {
		// A job accepted now would go onto a channel nobody reads again, and
		// it would also let a concurrent producer feed drain() forever while
		// Close waits on it.
		return "", ErrManagerStopped
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
					// Came due as the manager stopped: held, like anything
					// else that was accepted and never ran.
					m.holdDead(et, reasonShutdown, 0, nil)
				}
			case <-m.ctx.Done():
				// Still waiting for its delay when the manager stopped. It was
				// accepted, so it is held rather than dropped — otherwise a
				// delayed job would be the one kind the shutdown receipt does
				// not account for.
				m.holdDead(et, reasonQueueClosed, 0, nil)
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
