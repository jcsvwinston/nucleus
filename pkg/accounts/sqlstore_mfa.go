package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SQLStore implements MFAStore over two more tables. They are created by
// ensureMFASchema, called from NewSQLStore, so an existing deployment gains
// them on the next start rather than on a migration nobody ran.

func (s *SQLStore) factorsTable() string       { return s.prefix + "account_factors" }
func (s *SQLStore) recoveryCodesTable() string { return s.prefix + "account_recovery_codes" }

func (s *SQLStore) ensureMFASchema(ctx context.Context) error {
	var stmts []string
	switch s.flavor {
	case FlavorPostgres:
		stmts = []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				account_id TEXT NOT NULL,
				kind TEXT NOT NULL,
				secret TEXT NOT NULL,
				confirmed BOOLEAN NOT NULL DEFAULT FALSE,
				last_step BIGINT NOT NULL DEFAULT 0,
				created_at TIMESTAMPTZ NOT NULL,
				PRIMARY KEY (account_id, kind))`, s.factorsTable()),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				account_id TEXT NOT NULL,
				code_hash TEXT NOT NULL,
				used_at TIMESTAMPTZ NULL,
				PRIMARY KEY (account_id, code_hash))`, s.recoveryCodesTable()),
		}
	case FlavorMySQL:
		stmts = []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				account_id VARCHAR(64) NOT NULL,
				kind VARCHAR(32) NOT NULL,
				secret VARCHAR(512) NOT NULL,
				confirmed BOOLEAN NOT NULL DEFAULT FALSE,
				last_step BIGINT NOT NULL DEFAULT 0,
				created_at DATETIME(6) NOT NULL,
				PRIMARY KEY (account_id, kind))`, s.factorsTable()),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				account_id VARCHAR(64) NOT NULL,
				code_hash VARCHAR(64) NOT NULL,
				used_at DATETIME(6) NULL,
				PRIMARY KEY (account_id, code_hash))`, s.recoveryCodesTable()),
		}
	default:
		stmts = []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				account_id TEXT NOT NULL,
				kind TEXT NOT NULL,
				secret TEXT NOT NULL,
				confirmed INTEGER NOT NULL DEFAULT 0,
				last_step INTEGER NOT NULL DEFAULT 0,
				created_at TIMESTAMP NOT NULL,
				PRIMARY KEY (account_id, kind))`, s.factorsTable()),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				account_id TEXT NOT NULL,
				code_hash TEXT NOT NULL,
				used_at TIMESTAMP NULL,
				PRIMARY KEY (account_id, code_hash))`, s.recoveryCodesTable()),
		}
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("accounts: ensure MFA schema: %w", err)
		}
	}
	return nil
}

// PutFactor implements MFAStore. It replaces any factor of the same kind:
// re-enrolling is how somebody who lost their phone starts over, and it
// must not need a delete first.
func (s *SQLStore) PutFactor(ctx context.Context, factor Factor) error {
	del := s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE account_id = ? AND kind = ?`, s.factorsTable()))
	if _, err := s.db.ExecContext(ctx, del, factor.AccountID, string(factor.Kind)); err != nil {
		return fmt.Errorf("accounts: replace factor: %w", err)
	}
	insert := s.rebind(fmt.Sprintf(
		`INSERT INTO %s (account_id, kind, secret, confirmed, last_step, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		s.factorsTable()))
	if _, err := s.db.ExecContext(ctx, insert,
		factor.AccountID, string(factor.Kind), factor.Secret, factor.Confirmed, factor.LastStep, factor.CreatedAt); err != nil {
		return fmt.Errorf("accounts: insert factor: %w", err)
	}
	return nil
}

// GetFactor implements MFAStore.
func (s *SQLStore) GetFactor(ctx context.Context, accountID string, kind FactorKind) (Factor, error) {
	query := s.rebind(fmt.Sprintf(
		`SELECT account_id, kind, secret, confirmed, last_step, created_at FROM %s WHERE account_id = ? AND kind = ?`,
		s.factorsTable()))
	var f Factor
	var kindText string
	err := s.db.QueryRowContext(ctx, query, accountID, string(kind)).
		Scan(&f.AccountID, &kindText, &f.Secret, &f.Confirmed, &f.LastStep, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Factor{}, ErrMFANotEnrolled
	}
	if err != nil {
		return Factor{}, fmt.Errorf("accounts: select factor: %w", err)
	}
	f.Kind = FactorKind(kindText)
	return f, nil
}

// DeleteFactor implements MFAStore.
func (s *SQLStore) DeleteFactor(ctx context.Context, accountID string, kind FactorKind) error {
	query := s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE account_id = ? AND kind = ?`, s.factorsTable()))
	if _, err := s.db.ExecContext(ctx, query, accountID, string(kind)); err != nil {
		return fmt.Errorf("accounts: delete factor: %w", err)
	}
	return nil
}

// UpdateFactorStep implements MFAStore. The predicate refuses to move the
// step backwards, so two requests racing with the same code cannot both
// find it unused: the second one updates zero rows.
func (s *SQLStore) UpdateFactorStep(ctx context.Context, accountID string, kind FactorKind, step uint64) error {
	query := s.rebind(fmt.Sprintf(
		`UPDATE %s SET last_step = ? WHERE account_id = ? AND kind = ? AND last_step < ?`,
		s.factorsTable()))
	res, err := s.db.ExecContext(ctx, query, step, accountID, string(kind), step)
	if err != nil {
		return fmt.Errorf("accounts: update factor step: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrInvalidCode
	}
	return nil
}

// ReplaceRecoveryCodes implements MFAStore.
func (s *SQLStore) ReplaceRecoveryCodes(ctx context.Context, accountID string, hashes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("accounts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	del := s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE account_id = ?`, s.recoveryCodesTable()))
	if _, err := tx.ExecContext(ctx, del, accountID); err != nil {
		return fmt.Errorf("accounts: clear recovery codes: %w", err)
	}
	insert := s.rebind(fmt.Sprintf(`INSERT INTO %s (account_id, code_hash) VALUES (?, ?)`, s.recoveryCodesTable()))
	for _, hash := range hashes {
		if _, err := tx.ExecContext(ctx, insert, accountID, hash); err != nil {
			return fmt.Errorf("accounts: insert recovery code: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("accounts: commit: %w", err)
	}
	return nil
}

// ConsumeRecoveryCode implements MFAStore atomically, the same shape as
// ConsumeToken: the UPDATE carries the conditions, so one code cannot be
// spent twice by two concurrent requests.
func (s *SQLStore) ConsumeRecoveryCode(ctx context.Context, accountID, hash string) (int, error) {
	query := s.rebind(fmt.Sprintf(
		`UPDATE %s SET used_at = ? WHERE account_id = ? AND code_hash = ? AND used_at IS NULL`,
		s.recoveryCodesTable()))
	res, err := s.db.ExecContext(ctx, query, time.Now().UTC(), accountID, hash)
	if err != nil {
		return 0, fmt.Errorf("accounts: consume recovery code: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return 0, ErrInvalidCode
	}
	return s.CountRecoveryCodes(ctx, accountID)
}

// CountRecoveryCodes implements MFAStore, counting the unused ones.
func (s *SQLStore) CountRecoveryCodes(ctx context.Context, accountID string) (int, error) {
	query := s.rebind(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE account_id = ? AND used_at IS NULL`, s.recoveryCodesTable()))
	var count int
	if err := s.db.QueryRowContext(ctx, query, accountID).Scan(&count); err != nil {
		return 0, fmt.Errorf("accounts: count recovery codes: %w", err)
	}
	return count, nil
}
