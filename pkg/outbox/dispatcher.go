package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrLeaseOwnerRequired = fmt.Errorf("outbox: lease owner is required")
	ErrNoRouteMatched     = fmt.Errorf("outbox: no bridge route matched message topic")
)

// MissingRoutePolicy controls how bridge dispatch handles a message whose
// topic has no configured bridge route.
type MissingRoutePolicy string

const (
	// MissingRouteError keeps the message durable by retrying/failing it.
	MissingRouteError MissingRoutePolicy = "error"
	// MissingRouteIgnore preserves the old drop-on-the-floor behaviour only
	// when an application has explicitly opted into it.
	MissingRouteIgnore MissingRoutePolicy = "ignore"
)

// HandlerFunc delivers one claimed outbox message.
//
// This function type is used for traditional message delivery when
// bridge-based routing is not configured. The function should handle
// the message (e.g., send to an external system) and return an error
// if delivery fails. Errors trigger retry logic in the dispatcher.
type HandlerFunc func(context.Context, Message) error

// DispatcherConfig configures delivery attempts and polling behaviour.
//
// LeaseOwner is a unique identifier for this dispatcher instance, used for
// distributed locking when multiple instances are running.
// LeaseDuration is how long a message lease is held before it can be claimed by another instance.
// PollInterval is how often the dispatcher polls for new messages.
// BatchSize is the maximum number of messages to process in one poll cycle.
// MaxAttempts is the maximum number of delivery attempts before marking as failed.
// BaseDelay is the initial retry delay for exponential backoff.
// MaxDelay is the maximum retry delay.
// Registry is the bridge registry for external message delivery (optional).
// Router is the topic router for determining which bridges receive messages (optional).
// MissingRoutePolicy controls whether an unrouted bridge message is an error or intentionally ignored.
//
// If Registry and Router are configured, the dispatcher will use bridge-based routing.
// Otherwise, it will use the traditional HandlerFunc for message delivery.
type DispatcherConfig struct {
	LeaseOwner         string
	LeaseDuration      time.Duration
	PollInterval       time.Duration
	BatchSize          int
	MaxAttempts        int
	BaseDelay          time.Duration
	MaxDelay           time.Duration
	Registry           *BridgeRegistry
	Router             *Router
	MissingRoutePolicy MissingRoutePolicy
}

// DispatchResult summarizes one dispatcher pass.
//
// Attempted is the total number of messages processed in this pass.
// Delivered is the number of messages successfully delivered.
// Retried is the number of messages that failed and will be retried.
// Failed is the number of messages that exceeded MaxAttempts and were marked as failed.
type DispatchResult struct {
	Attempted int `json:"attempted"`
	Delivered int `json:"delivered"`
	Retried   int `json:"retried"`
	Failed    int `json:"failed"`
}

// Dispatcher polls the outbox table, leases pending messages, and delivers them through a handler.
//
// The dispatcher uses a leasing mechanism to ensure that multiple instances
// can run concurrently without duplicate processing. Messages are claimed
// with a lease duration, and if delivery fails, they are retried with
// exponential backoff.
//
// When Registry and Router are configured, the dispatcher uses bridge-based
// routing instead of the traditional HandlerFunc.
type Dispatcher struct {
	store   *Store
	handler HandlerFunc
	cfg     DispatcherConfig
}

// DefaultDispatcherConfig returns sensible defaults for dispatcher configuration.
//
// These defaults are suitable for development and can be overridden for production.
func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		LeaseOwner:         "nucleus-outbox",
		LeaseDuration:      30 * time.Second,
		PollInterval:       time.Second,
		BatchSize:          10,
		MaxAttempts:        5,
		BaseDelay:          time.Second,
		MaxDelay:           time.Minute,
		MissingRoutePolicy: MissingRouteError,
	}
}

// NewDispatcher creates a new dispatcher with the given store, handler, and configuration.
//
// The store must be non-nil and the handler must be non-nil unless bridge-based
// routing is configured. The configuration is normalized to fill in any missing
// values with defaults.
//
// Returns an error if the store or handler is nil, or if the lease owner is empty.
func NewDispatcher(store *Store, handler HandlerFunc, cfg DispatcherConfig) (*Dispatcher, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	if handler == nil {
		return nil, ErrHandlerMissing
	}
	cfg = normalizeDispatcherConfig(cfg)
	if strings.TrimSpace(cfg.LeaseOwner) == "" {
		return nil, ErrLeaseOwnerRequired
	}
	return &Dispatcher{
		store:   store,
		handler: handler,
		cfg:     cfg,
	}, nil
}

// Run starts the dispatcher and blocks until the context is canceled.
//
// This method performs an initial dispatch pass, then polls at the configured
// interval until the context is canceled. It is designed to be run in a goroutine.
//
// Example:
//
//	go dispatcher.Run(ctx)
//
// Returns an error if the initial dispatch pass fails.
func (d *Dispatcher) Run(ctx context.Context) error {
	return d.RunGraceful(ctx, nil)
}

// RunGraceful is Run with a soft-stop signal: when stopAfterPass is closed,
// the dispatcher finishes the pass it is in and returns, instead of having
// its SQL cancelled mid-statement.
//
// The distinction matters at shutdown. Cancelling the run context is
// abrupt by design: it aborts the in-flight statement, so a pass can end
// having claimed messages it never attempted to deliver (they wait for the
// lease to expire), and the driver tears the result set down under
// cancellation. The soft signal makes the common shutdown path finish its
// work first; the caller keeps the context for the case where waiting is
// no longer an option (see ManagedOutbox.Stop, which escalates on its
// deadline).
//
// A nil stopAfterPass makes this exactly Run: only the context stops it.
func (d *Dispatcher) RunGraceful(ctx context.Context, stopAfterPass <-chan struct{}) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Asked to stop before the first pass: nothing to finish.
	if asked(stopAfterPass) {
		return nil
	}
	if _, err := d.RunOnce(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-stopAfterPass:
			return nil
		case <-ticker.C:
			// A tick and a soft stop can be ready at once, and select would
			// pick either: check the stop explicitly so "finish the current
			// pass" never means "start one more".
			if asked(stopAfterPass) {
				return nil
			}
			if _, err := d.RunOnce(ctx); err != nil {
				return err
			}
		}
	}
}

// asked reports whether a soft-stop channel is closed. A nil channel is
// never asked.
func asked(ch <-chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// RunOnce performs a single dispatch pass.
//
// This method claims available messages, delivers them through the handler or bridges,
// and updates their status based on the result. It returns a summary of the pass.
//
// If bridge-based routing is configured (Registry and Router are non-nil), messages
// are delivered to matching bridges. Otherwise, the traditional HandlerFunc is used.
//
// Messages that fail delivery are retried with exponential backoff until MaxAttempts
// is reached, at which point they are marked as failed.
func (d *Dispatcher) RunOnce(ctx context.Context) (DispatchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	claimed, err := d.claimAvailable(ctx)
	if err != nil {
		return DispatchResult{}, err
	}

	result := DispatchResult{Attempted: len(claimed)}
	for _, msg := range claimed {
		var handlerErr error

		// Use bridge routing if registry and router are configured
		if d.cfg.Registry != nil && d.cfg.Router != nil {
			handlerErr = d.dispatchViaBridges(ctx, msg)
		} else {
			// Fall back to traditional handler
			handlerErr = d.handler(ctx, msg)
		}

		if handlerErr == nil {
			if updateErr := d.markDelivered(ctx, msg.ID, time.Now().UTC()); updateErr != nil {
				return result, updateErr
			}
			result.Delivered++
			continue
		}

		if msg.Attempts >= d.cfg.MaxAttempts {
			if updateErr := d.markFailed(ctx, msg.ID, handlerErr, time.Now().UTC()); updateErr != nil {
				return result, updateErr
			}
			result.Failed++
			continue
		}

		nextAvailable := time.Now().UTC().Add(dispatchBackoff(d.cfg, msg.Attempts))
		if updateErr := d.markRetry(ctx, msg.ID, handlerErr, nextAvailable); updateErr != nil {
			return result, updateErr
		}
		result.Retried++
	}
	return result, nil
}

// dispatchViaBridges delivers a message to all bridges that match its topic.
//
// This method uses the router to find matching bridges, then sends the message
// to each bridge. If no bridges match, MissingRoutePolicy decides whether the
// message is retried/failed or intentionally ignored.
// If any bridge fails to send, all errors are collected and returned.
func (d *Dispatcher) dispatchViaBridges(ctx context.Context, msg Message) error {
	// Find bridges that match this topic
	bridgeNames := d.cfg.Router.Match(msg.Topic)
	if len(bridgeNames) == 0 {
		if d.cfg.MissingRoutePolicy == MissingRouteIgnore {
			return nil
		}
		// No bridges configured for this topic: keep the message visible.
		return fmt.Errorf("%w %q", ErrNoRouteMatched, msg.Topic)
	}

	if len(d.cfg.Registry.List()) == 0 && d.cfg.MissingRoutePolicy != MissingRouteIgnore {
		return fmt.Errorf("outbox: bridge route matched topic %q but registry is empty", msg.Topic)
	}

	// Send to all matching bridges
	var errs []error
	for _, bridgeName := range bridgeNames {
		bridge, ok := d.cfg.Registry.Get(bridgeName)
		if !ok {
			errs = append(errs, fmt.Errorf("bridge %q not found", bridgeName))
			continue
		}

		if err := bridge.Send(ctx, msg); err != nil {
			errs = append(errs, fmt.Errorf("bridge %q: %w", bridgeName, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("dispatch errors: %v", errs)
	}
	return nil
}

func normalizeMissingRoutePolicy(policy MissingRoutePolicy) MissingRoutePolicy {
	switch MissingRoutePolicy(strings.ToLower(strings.TrimSpace(string(policy)))) {
	case MissingRouteIgnore:
		return MissingRouteIgnore
	default:
		return MissingRouteError
	}
}

// normalizeDispatcherConfig fills in missing configuration values with defaults.
//
// This ensures that all required fields have sensible values and that
// MaxDelay is not less than BaseDelay.
func normalizeDispatcherConfig(cfg DispatcherConfig) DispatcherConfig {
	defaults := DefaultDispatcherConfig()
	if strings.TrimSpace(cfg.LeaseOwner) == "" {
		cfg.LeaseOwner = defaults.LeaseOwner
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = defaults.LeaseDuration
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaults.PollInterval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaults.BatchSize
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaults.MaxAttempts
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = defaults.BaseDelay
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = defaults.MaxDelay
	}
	if cfg.MaxDelay < cfg.BaseDelay {
		cfg.MaxDelay = cfg.BaseDelay
	}
	cfg.MissingRoutePolicy = normalizeMissingRoutePolicy(cfg.MissingRoutePolicy)
	return cfg
}

// dispatchBackoff calculates the retry delay using exponential backoff.
//
// The delay starts at BaseDelay and doubles with each attempt, capped at MaxDelay.
// This provides a simple exponential backoff strategy for retrying failed deliveries.
func dispatchBackoff(cfg DispatcherConfig, attempts int) time.Duration {
	if attempts <= 1 {
		return cfg.BaseDelay
	}
	multiplier := math.Pow(2, float64(attempts-1))
	backoff := time.Duration(float64(cfg.BaseDelay) * multiplier)
	if backoff > cfg.MaxDelay {
		return cfg.MaxDelay
	}
	return backoff
}

// claimAvailable queries the outbox table for pending messages and attempts to claim them.
//
// This method selects messages that are pending, available now, and not currently leased.
// It then attempts to claim each message by updating its status to "processing" with
// a lease. Messages that are successfully claimed are returned for delivery.
//
// The claiming is done with an UPDATE that uses a WHERE clause to ensure only
// one dispatcher instance can claim a given message (optimistic locking).
func (d *Dispatcher) claimAvailable(ctx context.Context) ([]Message, error) {
	now := time.Now().UTC()
	query := fmt.Sprintf(
		`SELECT id, topic, payload, status, available_at, created_at, delivered_at, attempts, last_error
		FROM %s
		WHERE status = %s AND available_at <= %s AND (lease_until IS NULL OR lease_until <= %s)
		ORDER BY available_at ASC, created_at ASC
		LIMIT %s`,
		d.store.quotedTable(),
		d.store.placeholder(1),
		d.store.placeholder(2),
		d.store.placeholder(3),
		d.store.placeholder(4),
	)
	rows, err := d.store.db.QueryContext(ctx, query, string(StatusPending), now, now, d.cfg.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("outbox dispatcher select: %w", err)
	}

	candidates := make([]Message, 0, d.cfg.BatchSize)
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("outbox dispatcher scan: %w", err)
		}
		candidates = append(candidates, msg)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("outbox dispatcher rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("outbox dispatcher close rows: %w", err)
	}

	claimed := make([]Message, 0, len(candidates))
	for _, msg := range candidates {
		ok, claimedMsg, err := d.tryClaim(ctx, msg, now)
		if err != nil {
			return nil, err
		}
		if ok {
			claimed = append(claimed, claimedMsg)
		}
	}
	return claimed, nil
}

// tryClaim attempts to claim a single message by updating its status to "processing".
//
// This method uses an UPDATE with a WHERE clause to ensure atomic claiming.
// If the message is still pending and available, the UPDATE will succeed and
// the message is returned as claimed. If another dispatcher instance claimed
// it first, the UPDATE will affect 0 rows and the message is not claimed.
func (d *Dispatcher) tryClaim(ctx context.Context, msg Message, now time.Time) (bool, Message, error) {
	leaseUntil := now.Add(d.cfg.LeaseDuration)
	query := fmt.Sprintf(
		`UPDATE %s
		SET status = %s, lease_owner = %s, lease_until = %s, attempts = attempts + 1
		WHERE id = %s AND status = %s AND available_at <= %s AND (lease_until IS NULL OR lease_until <= %s)`,
		d.store.quotedTable(),
		d.store.placeholder(1),
		d.store.placeholder(2),
		d.store.placeholder(3),
		d.store.placeholder(4),
		d.store.placeholder(5),
		d.store.placeholder(6),
		d.store.placeholder(7),
	)
	result, err := d.store.db.ExecContext(
		ctx,
		query,
		string(StatusProcessing),
		d.cfg.LeaseOwner,
		leaseUntil,
		msg.ID,
		string(StatusPending),
		now,
		now,
	)
	if err != nil {
		return false, Message{}, fmt.Errorf("outbox dispatcher claim %s: %w", msg.ID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, Message{}, fmt.Errorf("outbox dispatcher claim rows %s: %w", msg.ID, err)
	}
	if rowsAffected == 0 {
		return false, Message{}, nil
	}
	msg.Status = StatusProcessing
	msg.Attempts++
	return true, msg, nil
}

// markDelivered updates a message status to "delivered" with the delivery timestamp.
func (d *Dispatcher) markDelivered(ctx context.Context, id string, deliveredAt time.Time) error {
	_, err := d.updateMessageState(
		ctx,
		id,
		string(StatusDelivered),
		deliveredAt,
		nil,
		"",
	)
	if err != nil {
		return fmt.Errorf("outbox dispatcher mark delivered %s: %w", id, err)
	}
	return nil
}

// markRetry updates a message status back to "pending" with a new available timestamp for retry.
func (d *Dispatcher) markRetry(ctx context.Context, id string, handlerErr error, availableAt time.Time) error {
	_, err := d.updateMessageState(
		ctx,
		id,
		string(StatusPending),
		time.Time{},
		&availableAt,
		handlerErr.Error(),
	)
	if err != nil {
		return fmt.Errorf("outbox dispatcher mark retry %s: %w", id, err)
	}
	return nil
}

// markFailed updates a message status to "failed" with the error message and failure timestamp.
func (d *Dispatcher) markFailed(ctx context.Context, id string, handlerErr error, failedAt time.Time) error {
	_, err := d.updateMessageState(
		ctx,
		id,
		string(StatusFailed),
		time.Time{},
		&failedAt,
		handlerErr.Error(),
	)
	if err != nil {
		return fmt.Errorf("outbox dispatcher mark failed %s: %w", id, err)
	}
	return nil
}

// updateMessageState performs the actual SQL UPDATE to change a message's status.
//
// This helper method is used by markDelivered, markRetry, and markFailed.
// It updates the status, delivery timestamp, available timestamp, and error message
// as needed, and clears the lease information.
func (d *Dispatcher) updateMessageState(ctx context.Context, id string, status string, deliveredAt time.Time, availableAt *time.Time, lastError string) (sql.Result, error) {
	var deliveredArg any
	if !deliveredAt.IsZero() {
		deliveredArg = deliveredAt.UTC()
	}
	var availableArg any
	if availableAt != nil {
		availableArg = availableAt.UTC()
	}
	query := fmt.Sprintf(
		`UPDATE %s
		SET status = %s, delivered_at = %s, available_at = COALESCE(%s, available_at), last_error = %s, lease_owner = NULL, lease_until = NULL
		WHERE id = %s`,
		d.store.quotedTable(),
		d.store.placeholder(1),
		d.store.placeholder(2),
		d.store.placeholder(3),
		d.store.placeholder(4),
		d.store.placeholder(5),
	)
	return d.store.db.ExecContext(ctx, query, status, deliveredArg, availableArg, nullIfEmpty(lastError), id)
}

// nullIfEmpty returns nil if the string is empty, otherwise returns the string.
//
// This is used to set SQL NULL values for empty error messages.
func nullIfEmpty(raw string) any {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	return value
}
