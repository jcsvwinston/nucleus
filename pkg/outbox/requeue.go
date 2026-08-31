package outbox

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RequeueFailed returns messages in the "failed" state to "pending" so the
// dispatcher picks them up again (NF-8: a message that exhausted MaxAttempts
// stayed failed forever, and the only remedy was hand-written SQL).
//
// With no ids, every failed message is requeued; with ids, only those — an
// id that is not currently failed is left untouched (delivered or in-flight
// messages must not be re-run by a typo'd id).
//
// A requeued message gets its full retry budget back (attempts reset to 0)
// and becomes available immediately. last_error is preserved until the next
// attempt overwrites it, so the reason it failed remains inspectable right
// up to the retry.
//
// Returns the number of messages actually requeued.
func (s *Store) RequeueFailed(ctx context.Context, ids ...string) (int64, error) {
	if s == nil {
		return 0, ErrNilStore
	}

	now := time.Now().UTC()
	args := []any{string(StatusPending), now, string(StatusFailed)}
	query := fmt.Sprintf(
		`UPDATE %s
		SET status = %s, attempts = 0, available_at = %s, delivered_at = NULL, lease_owner = NULL, lease_until = NULL
		WHERE status = %s`,
		s.quotedTable(),
		s.placeholder(1),
		s.placeholder(2),
		s.placeholder(3),
	)

	cleaned := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			cleaned = append(cleaned, id)
		}
	}
	if len(cleaned) > 0 {
		placeholders := make([]string, len(cleaned))
		for i, id := range cleaned {
			placeholders[i] = s.placeholder(4 + i)
			args = append(args, id)
		}
		query += " AND id IN (" + strings.Join(placeholders, ", ") + ")"
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("outbox: requeue failed messages: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		// The UPDATE ran; a driver that cannot report the count is not a
		// requeue failure. Report zero-with-error so the caller can decide.
		return 0, fmt.Errorf("outbox: requeue executed but rows-affected is unavailable: %w", err)
	}
	return affected, nil
}

// RequeueFailed forwards to the managed store — see Store.RequeueFailed.
func (m *ManagedOutbox) RequeueFailed(ctx context.Context, ids ...string) (int64, error) {
	if m == nil || m.store == nil {
		return 0, ErrNilStore
	}
	return m.store.RequeueFailed(ctx, ids...)
}
