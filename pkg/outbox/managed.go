package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ManagedOutbox wraps the outbox components with lifecycle management.
//
// This type provides a high-level interface for managing the outbox pattern
// within an application. It combines the store, dispatcher, bridge registry,
// and topic router into a single managed component that can be started and stopped.
//
// The managed outbox is typically created by the app.App when outbox is enabled
// in the configuration, but can also be created manually for custom use cases.
//
// Example usage:
//
//	managed, err := outbox.NewManagedOutbox(outbox.ManagedConfig{
//	    DB:        sqlDB,
//	    TableName: "nucleus_outbox",
//	    Flavor:    outbox.FlavorSQLite,
//	    Logger:    logger,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Register bridges
//	managed.RegisterBridge(webhookBridge)
//	managed.AddRoute("notifications.*", "webhook")
//
//	// Start the dispatcher
//	if err := managed.Start(context.Background()); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Enqueue messages
//	managed.Enqueue(ctx, outbox.Entry{
//	    Topic:   "notifications.email",
//	    Payload: map[string]any{"to": "user@example.com"},
//	})
type ManagedOutbox struct {
	store      *Store
	dispatcher *Dispatcher
	registry   *BridgeRegistry
	router     *Router
	running    bool
	cancel     context.CancelFunc
	done       chan struct{}
	// soft asks the dispatcher to finish the pass in flight and exit (see
	// Dispatcher.RunGraceful); softOnce keeps a repeated Stop from closing
	// it twice.
	soft     chan struct{}
	softOnce *sync.Once
	mu       sync.Mutex
	logger   *slog.Logger
}

// ManagedConfig configures a managed outbox instance.
//
// DB is the SQL database connection used for the outbox table.
// TableName is the name of the outbox table (defaults to "nucleus_outbox").
// Flavor is the database flavor (SQLite, Postgres, MySQL) for SQL dialect differences.
// LeaseOwner is a unique identifier for this instance (used for distributed locking).
// LeaseDuration is how long a message lease is held before it can be claimed by another instance.
// PollInterval is how often the dispatcher polls for new messages.
// BatchSize is the maximum number of messages to process in one poll cycle.
// MaxAttempts is the maximum number of delivery attempts before marking as failed.
// BaseDelay is the initial retry delay for exponential backoff.
// MaxDelay is the maximum retry delay.
// Logger is the structured logger for operational events.
//
// MissingRoutePolicy controls what happens when a leased message's topic has
// no registered bridge (QCD-FW-5). The default, MissingRouteError, fails the
// message — correct for a homogeneous fleet where every instance registers
// every bridge. In a deliberately heterogeneous fleet (only some processes
// register bridges for some topics), set MissingRouteIgnore so an instance
// leaves unrouted messages for the instance that can deliver them, instead
// of leasing and failing them (stealing attempts from the real deliverer).
type ManagedConfig struct {
	DB                 *sql.DB
	TableName          string
	Flavor             Flavor
	LeaseOwner         string
	LeaseDuration      time.Duration
	PollInterval       time.Duration
	BatchSize          int
	MaxAttempts        int
	BaseDelay          time.Duration
	MaxDelay           time.Duration
	Logger             *slog.Logger
	MissingRoutePolicy MissingRoutePolicy
}

// NewManagedOutbox creates a new managed outbox instance.
//
// This method initializes the store, bridge registry, topic router, and dispatcher.
// The dispatcher is configured with a fallback handler that returns an error if
// no bridges are configured for a message topic.
//
// Returns an error if the database connection is nil or if store/dispatcher creation fails.
func NewManagedOutbox(cfg ManagedConfig) (*ManagedOutbox, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("managed outbox: database is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	store, err := NewStore(cfg.DB, Config{
		TableName: cfg.TableName,
		Flavor:    cfg.Flavor,
	})
	if err != nil {
		return nil, fmt.Errorf("managed outbox: create store: %w", err)
	}

	registry := NewBridgeRegistry()
	router := NewRouter()

	// Create dispatcher with bridge support but no handler initially
	// The dispatcher will use bridges when configured
	// QCD-FW-5: the policy is a knob, not a constant. Empty keeps the
	// historical default (error on unrouted topics).
	policy := cfg.MissingRoutePolicy
	if policy == "" {
		policy = MissingRouteError
	}

	dispatcherCfg := DispatcherConfig{
		LeaseOwner:         cfg.LeaseOwner,
		LeaseDuration:      cfg.LeaseDuration,
		PollInterval:       cfg.PollInterval,
		BatchSize:          cfg.BatchSize,
		MaxAttempts:        cfg.MaxAttempts,
		BaseDelay:          cfg.BaseDelay,
		MaxDelay:           cfg.MaxDelay,
		Registry:           registry,
		Router:             router,
		MissingRoutePolicy: policy,
	}

	// Create a no-op handler for now - will be replaced by bridge routing
	dispatcher, err := NewDispatcher(store, func(ctx context.Context, msg Message) error {
		// This should not be called when bridges are configured
		return fmt.Errorf("no handler configured and no bridges matched topic %q", msg.Topic)
	}, dispatcherCfg)
	if err != nil {
		return nil, fmt.Errorf("managed outbox: create dispatcher: %w", err)
	}

	return &ManagedOutbox{
		store:      store,
		dispatcher: dispatcher,
		registry:   registry,
		router:     router,
		logger:     cfg.Logger,
	}, nil
}

// RegisterBridge adds a bridge to the registry.
//
// This method is thread-safe and can be called before or after Start().
// Bridges registered after Start() will be used for subsequent message dispatches.
func (m *ManagedOutbox) RegisterBridge(bridge Bridge) error {
	return m.registry.Register(bridge)
}

// AddRoute adds a topic routing rule.
//
// Messages matching the pattern will be sent to all specified bridges.
// This method is thread-safe and can be called before or after Start().
// Example: AddRoute("billing.*", "kafka-billing", "webhook-alerts")
func (m *ManagedOutbox) AddRoute(pattern string, bridgeNames ...string) {
	m.router.AddRoute(pattern, bridgeNames...)
}

// Enqueue adds a message to the outbox.
//
// The message will be stored in the outbox table and will be dispatched
// by the background dispatcher when it becomes available.
// Returns the created message with its ID and status.
func (m *ManagedOutbox) Enqueue(ctx context.Context, entry Entry) (Message, error) {
	return m.store.Enqueue(ctx, entry)
}

// EnqueueTx adds a message to the outbox within a transaction.
//
// This is the recommended method for transactional outbox pattern usage.
// The message is enqueued within the provided transaction, ensuring
// atomicity with other database operations. If the transaction is rolled back,
// the message will not be persisted.
//
// Example:
//
//	tx, _ := db.BeginTx(ctx, nil)
//	// ... perform domain writes ...
//	managed.EnqueueTx(ctx, tx, outbox.Entry{Topic: "order.created", Payload: ...})
//	tx.Commit()
func (m *ManagedOutbox) EnqueueTx(ctx context.Context, tx *sql.Tx, entry Entry) (Message, error) {
	return m.store.EnqueueTx(ctx, tx, entry)
}

// Snapshot returns the current outbox state.
//
// This method queries the outbox table and returns counts of messages
// by status (pending, processing, delivered, failed) along with
// timestamps for the oldest pending and last delivered messages.
// Useful for monitoring and health checks.
func (m *ManagedOutbox) Snapshot(ctx context.Context) RuntimeSnapshot {
	return m.store.Snapshot(ctx)
}

// GracefulStopTimeout bounds how long Stop waits for the dispatch pass in
// flight to finish before it cancels the run context. It applies when the
// caller's Stop context carries no deadline of its own — a graceful stop
// must not become an unbounded one, because a pass can be waiting on a
// bridge that is not answering.
const GracefulStopTimeout = 5 * time.Second

// forcedStopTimeout bounds the wait after the run context is cancelled.
// Reaching it means the dispatcher ignored cancellation, which is a bug
// worth an error rather than a silent leak.
const forcedStopTimeout = 5 * time.Second

// waitStopped waits for done, giving up when ctx is done or after limit.
// It reports whether done was observed closed.
func waitStopped(ctx context.Context, done <-chan struct{}, limit time.Duration) bool {
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

// Start begins the dispatcher in a background goroutine.
//
// The dispatcher will poll the outbox table for pending messages and
// deliver them to configured bridges. This method is non-blocking and
// returns immediately after starting the goroutine.
//
// The context is used to stop the dispatcher when the application shuts down.
// Canceling the context will cause the dispatcher to stop gracefully.
//
// Returns an error if the dispatcher is already running.
func (m *ManagedOutbox) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("managed outbox: already running")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	soft := make(chan struct{})

	m.running = true
	m.cancel = cancel
	m.done = done
	m.soft = soft
	m.softOnce = &sync.Once{}

	go func() {
		defer func() {
			m.mu.Lock()
			m.running = false
			if m.done == done {
				m.cancel = nil
			}
			m.mu.Unlock()
			close(done)
		}()

		if err := m.dispatcher.RunGraceful(runCtx, soft); err != nil && runCtx.Err() == nil {
			m.logger.Error("outbox dispatcher stopped", "error", err)
		}
	}()

	m.logger.Info("outbox dispatcher started")
	return nil
}

// Stop gracefully shuts down the outbox.
//
// This method stops the dispatcher and closes all registered bridges.
// It is safe to call multiple times. If the dispatcher is not running,
// this method returns nil immediately.
func (m *ManagedOutbox) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running && m.done == nil {
		m.mu.Unlock()
		return nil
	}
	cancel := m.cancel
	done := m.done
	soft := m.soft
	softOnce := m.softOnce
	m.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	// Graceful first: let the dispatcher finish the pass it is in. Only if
	// that takes longer than the caller can wait — its context deadline, or
	// GracefulStopTimeout when it has none — is the run context cancelled,
	// which aborts the statement in flight.
	if soft != nil && softOnce != nil {
		softOnce.Do(func() { close(soft) })
	}
	if done != nil {
		if !waitStopped(ctx, done, GracefulStopTimeout) {
			m.logger.Warn("outbox: dispatch pass did not finish within the graceful window; cancelling it",
				"graceful_timeout", GracefulStopTimeout)
			if cancel != nil {
				cancel()
			}
			if !waitStopped(context.Background(), done, forcedStopTimeout) {
				return fmt.Errorf("managed outbox: dispatcher did not stop within %s after cancellation", forcedStopTimeout)
			}
		}
	}

	// Close all bridges
	if err := m.registry.Close(); err != nil {
		m.logger.Error("outbox: error closing bridges", "error", err)
		return err
	}

	m.mu.Lock()
	if m.done == done {
		m.done = nil
		m.cancel = nil
		m.soft = nil
		m.softOnce = nil
		m.running = false
	}
	m.mu.Unlock()

	m.logger.Info("outbox stopped")
	return nil
}

// Store returns the underlying store for direct access if needed.
//
// This provides access to the low-level store interface for advanced use cases
// that require direct database operations. Most applications should use the
// Enqueue/EnqueueTx methods instead.
func (m *ManagedOutbox) Store() *Store {
	return m.store
}

// Registry returns the bridge registry.
//
// This provides access to the bridge registry for advanced use cases
// such as dynamically adding/removing bridges at runtime.
func (m *ManagedOutbox) Registry() *BridgeRegistry {
	return m.registry
}

// Router returns the topic router.
//
// This provides access to the topic router for advanced use cases
// such as dynamically adding/removing routing rules at runtime.
func (m *ManagedOutbox) Router() *Router {
	return m.router
}
