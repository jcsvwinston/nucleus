package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

var ErrLeaseOwnerRequired = fmt.Errorf("outbox: lease owner is required")

// HandlerFunc delivers one claimed outbox message.
type HandlerFunc func(context.Context, Message) error

// DispatcherConfig configures delivery attempts and polling behaviour.
type DispatcherConfig struct {
	LeaseOwner    string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	BatchSize     int
	MaxAttempts   int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
}

// DispatchResult summarizes one dispatcher pass.
type DispatchResult struct {
	Attempted int `json:"attempted"`
	Delivered int `json:"delivered"`
	Retried   int `json:"retried"`
	Failed    int `json:"failed"`
}

// Dispatcher polls the outbox table, leases pending messages, and delivers them through a handler.
type Dispatcher struct {
	store   *Store
	handler HandlerFunc
	cfg     DispatcherConfig
}

func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		LeaseOwner:    "goframe-outbox",
		LeaseDuration: 30 * time.Second,
		PollInterval:  time.Second,
		BatchSize:     10,
		MaxAttempts:   5,
		BaseDelay:     time.Second,
		MaxDelay:      time.Minute,
	}
}

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

func (d *Dispatcher) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
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
		case <-ticker.C:
			if _, err := d.RunOnce(ctx); err != nil {
				return err
			}
		}
	}
}

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
		err := d.handler(ctx, msg)
		if err == nil {
			if updateErr := d.markDelivered(ctx, msg.ID, time.Now().UTC()); updateErr != nil {
				return result, updateErr
			}
			result.Delivered++
			continue
		}

		if msg.Attempts >= d.cfg.MaxAttempts {
			if updateErr := d.markFailed(ctx, msg.ID, err, time.Now().UTC()); updateErr != nil {
				return result, updateErr
			}
			result.Failed++
			continue
		}

		nextAvailable := time.Now().UTC().Add(dispatchBackoff(d.cfg, msg.Attempts))
		if updateErr := d.markRetry(ctx, msg.ID, err, nextAvailable); updateErr != nil {
			return result, updateErr
		}
		result.Retried++
	}
	return result, nil
}

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
	return cfg
}

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

func nullIfEmpty(raw string) any {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	return value
}
