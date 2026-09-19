// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlprovider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// DefaultLeaderTTL is how long a leadership lease lasts. The leader renews at
// a third of it, so a leader that dies is replaced after at most one TTL
// without any coordination between replicas.
const DefaultLeaderTTL = 30 * time.Second

// leaderTable is where the lease lives: one row per scope, in the same
// database as the queue. That is the whole point — the election needs no
// Redis, no etcd and no consensus protocol, because every replica already
// shares this database and the database already serialises writes to a row.
func (s *Store) leaderTable() string { return s.quoted(s.table + "_leader") }

func (s *Store) ensureLeaderSchema(ctx context.Context) error {
	var create string
	switch s.flavor {
	case FlavorPostgres:
		create = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			scope TEXT PRIMARY KEY,
			owner TEXT NOT NULL,
			lease_until TIMESTAMPTZ NOT NULL
		)`, s.leaderTable())
	case FlavorMySQL:
		create = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			scope VARCHAR(191) PRIMARY KEY,
			owner VARCHAR(191) NOT NULL,
			lease_until DATETIME(6) NOT NULL
		)`, s.leaderTable())
	default:
		create = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			scope TEXT PRIMARY KEY,
			owner TEXT NOT NULL,
			lease_until DATETIME NOT NULL
		)`, s.leaderTable())
	}
	if _, err := s.db.ExecContext(ctx, create); err != nil {
		return fmt.Errorf("sqlprovider: prepare the leader table: %w", err)
	}
	return nil
}

// AcquireLeadership takes or renews the lease for scope, and reports whether
// this owner holds it.
//
// The election is three statements and no protocol: try to take a free or
// expired lease, renew one this owner already holds, or insert the row the
// first time. Whoever's UPDATE affects the row is the leader, because the
// database serialises them; everyone else reads zero rows affected and waits.
//
// Time is bound from Go in UTC — never the engine's clock, which would make
// leadership depend on which server a replica happens to talk to.
func (s *Store) AcquireLeadership(ctx context.Context, scope, owner string, ttl time.Duration, now time.Time) (bool, error) {
	if scope == "" || owner == "" {
		return false, errors.New("sqlprovider: leadership needs a scope and an owner")
	}
	if ttl <= 0 {
		ttl = DefaultLeaderTTL
	}
	now = now.UTC()
	until := now.Add(ttl)

	// Renew or take over: the row exists, and either it is ours or its lease
	// has expired.
	renew := s.rebind(fmt.Sprintf(
		`UPDATE %s SET owner = ?, lease_until = ? WHERE scope = ? AND (owner = ? OR lease_until <= ?)`,
		s.leaderTable()))
	res, err := s.db.ExecContext(ctx, renew, owner, until, scope, owner, now)
	if err != nil {
		return false, fmt.Errorf("sqlprovider: renew leadership: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		return true, nil
	}

	// No row yet: whoever inserts first leads. A duplicate key means another
	// replica got there in the same instant, which is a lost election, not an
	// error to surface.
	insert := s.rebind(fmt.Sprintf(
		`INSERT INTO %s (scope, owner, lease_until) VALUES (?, ?, ?)`, s.leaderTable()))
	if _, err := s.db.ExecContext(ctx, insert, scope, owner, until); err != nil {
		if isDuplicate(err) {
			return false, nil
		}
		// The row may also have appeared between the update and here; one
		// more renew attempt tells us whether we can have it.
		res, retryErr := s.db.ExecContext(ctx, renew, owner, until, scope, owner, now)
		if retryErr != nil {
			return false, fmt.Errorf("sqlprovider: claim leadership: %w", err)
		}
		if n, rowsErr := res.RowsAffected(); rowsErr == nil && n > 0 {
			return true, nil
		}
		return false, nil
	}
	return true, nil
}

// ReleaseLeadership gives the lease up, so a replica that stops cleanly does
// not make everyone else wait out its TTL. Fenced by owner: a former leader
// cannot release the lease of the one that replaced it.
func (s *Store) ReleaseLeadership(ctx context.Context, scope, owner string) error {
	query := s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE scope = ? AND owner = ?`, s.leaderTable()))
	if _, err := s.db.ExecContext(ctx, query, scope, owner); err != nil {
		return fmt.Errorf("sqlprovider: release leadership: %w", err)
	}
	return nil
}

// isDuplicate reports a unique-violation error without naming a driver: the
// framework's own classifier lives in pkg/db and the drivers are separate
// modules, so this package matches on the text every engine puts in it.
func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	for _, needle := range []string{
		"UNIQUE constraint failed", // sqlite
		"duplicate key",            // postgres
		"Duplicate entry",          // mysql
		"SQLSTATE 23505",           // postgres, wrapped
	} {
		if containsFold(text, needle) {
			return true
		}
	}
	return false
}

func containsFold(haystack, needle string) bool {
	h, n := []rune(haystack), []rune(needle)
	if len(n) == 0 || len(h) < len(n) {
		return len(n) == 0
	}
	lower := func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}
	for i := 0; i+len(n) <= len(h); i++ {
		ok := true
		for j := range n {
			if lower(h[i+j]) != lower(n[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

var _ = sql.ErrNoRows
