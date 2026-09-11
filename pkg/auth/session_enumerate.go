package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SessionInfo is a decoded snapshot of one stored session, returned by
// SessionManager.ActiveSessions. It is distinct from the lifecycle-event payload
// observability/hooks.SessionInfo — this is an enumeration/admin snapshot.
//
// SECURITY: SessionInfo carries live secrets. Token is the raw session token, a
// bearer credential — anyone who obtains it can impersonate the session until it
// expires. Values is the full decoded session map and MAY hold user identifiers,
// CSRF tokens, and flash data. ActiveSessions is intended ONLY for a trusted,
// in-process operator/admin surface (orbit). NEVER serialize Token or Values to
// an untrusted response, an access log, or any sink that leaves the process
// without deliberate redaction. The raw Token is retained (not hashed) on
// purpose: an operator tool acts on a session — e.g. revokes it through the
// store — which needs the exact token.
type SessionInfo struct {
	// Token is the raw session token (the store key) — a bearer credential.
	Token string
	// Deadline is the absolute expiry the codec recorded for the session.
	Deadline time.Time
	// Values are the decoded session key/value pairs (may be sensitive).
	Values map[string]any
}

// ErrSessionStoreNotIterable is returned by ActiveSessions when the configured
// session store does not support enumeration (a custom store implementing
// neither All nor AllCtx; the built-in stores all support it since the
// non-functional cookie store was removed in v0.12.0, DEP-2026-006).
var ErrSessionStoreNotIterable = errors.New("auth: session store does not support enumeration")

// ErrNilSessionManager is returned by ActiveSessions when called on a nil or
// zero-initialised SessionManager (one not created via NewSessionManager).
var ErrNilSessionManager = errors.New("auth: nil session manager")

// ActiveSessions returns a decoded snapshot of every session currently held by
// the store, for a session-management/observability surface (e.g. an admin
// "active sessions" view). It requires a store that supports enumeration — the
// memory, SQL, Redis and Memcached stores all do; a custom store implementing
// neither All nor AllCtx yields ErrSessionStoreNotIterable.
//
// A payload that fails to decode is skipped rather than failing the whole call,
// so one corrupt entry cannot blind the operator to every other session. The
// returned slice is a point-in-time snapshot, unordered; callers sort/filter as
// needed.
//
// Cost: O(N) in the number of stored sessions — it loads and decodes the whole
// store into memory in one pass (scs stores expose no server-side pagination),
// so cap what you render at the call site for a very large store.
//
// This packages a capability already reachable through the SCS() escape hatch
// (Store + Codec) into one typed call; it exposes no scs type on the API. The
// returned Values/Token are sensitive — see SessionInfo (SECURITY).
func (s *SessionManager) ActiveSessions(ctx context.Context) ([]SessionInfo, error) {
	if s == nil || s.scs == nil {
		return nil, ErrNilSessionManager
	}

	var (
		raw map[string][]byte
		err error
	)
	switch store := s.scs.Store.(type) {
	case interface {
		AllCtx(context.Context) (map[string][]byte, error)
	}:
		raw, err = store.AllCtx(ctx)
	case interface {
		All() (map[string][]byte, error)
	}:
		raw, err = store.All()
	default:
		return nil, ErrSessionStoreNotIterable
	}
	if err != nil {
		return nil, fmt.Errorf("auth: ActiveSessions: %w", err)
	}

	// s.scs.Codec is always non-nil — scs.New sets the default GobCodec and no
	// pkg/auth path clears it.
	out := make([]SessionInfo, 0, len(raw))
	for token, payload := range raw {
		deadline, values, decErr := s.scs.Codec.Decode(payload)
		if decErr != nil {
			continue // skip a corrupt/undecodable payload, keep the rest
		}
		out = append(out, SessionInfo{Token: token, Deadline: deadline, Values: values})
	}
	return out, nil
}

// Revoke deletes one stored session by its token, ending it everywhere it
// was in use — the operation behind "sign out my other devices" and behind
// an operator ending a session that should not be open.
//
// It is separate from Destroy and Invalidate, which act on the session in
// the REQUEST context and therefore can only end the caller's own. That
// asymmetry is why the framework could enumerate sessions and not act on
// them: the store has always known how to delete a token, and nothing
// exposed it.
//
// Revoking a token that is not in the store is not an error: the outcome a
// caller asked for — that session is gone — already holds, and reporting a
// miss would leak whether a token existed to whoever can call this.
func (s *SessionManager) Revoke(ctx context.Context, token string) error {
	if s == nil || s.scs == nil {
		return ErrNilSessionManager
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("auth: cannot revoke an empty session token")
	}

	switch store := s.scs.Store.(type) {
	case interface {
		DeleteCtx(context.Context, string) error
	}:
		if err := store.DeleteCtx(ctx, token); err != nil {
			return fmt.Errorf("auth: Revoke: %w", err)
		}
	default:
		if err := s.scs.Store.Delete(token); err != nil {
			return fmt.Errorf("auth: Revoke: %w", err)
		}
	}
	return nil
}

// RevokeWhere deletes every stored session the predicate accepts and
// returns how many it ended. It is the shape "sign out everywhere except
// here" actually needs: the caller keeps its own token and revokes the
// rest.
//
//	current := sm.Token(ctx)
//	n, err := sm.RevokeWhere(ctx, func(s auth.SessionInfo) bool {
//	    return s.Values["user_id"] == userID && s.Token != current
//	})
//
// A predicate is used rather than a user id because the framework does not
// own the key an application stores identity under; the caller does.
func (s *SessionManager) RevokeWhere(ctx context.Context, match func(SessionInfo) bool) (int, error) {
	if s == nil || s.scs == nil {
		return 0, ErrNilSessionManager
	}
	if match == nil {
		return 0, errors.New("auth: RevokeWhere needs a predicate")
	}

	sessions, err := s.ActiveSessions(ctx)
	if err != nil {
		return 0, err
	}
	revoked := 0
	for _, info := range sessions {
		if !match(info) {
			continue
		}
		if err := s.Revoke(ctx, info.Token); err != nil {
			// Report what was ended before the failure: a caller that
			// retries needs to know the operation was partial.
			return revoked, err
		}
		revoked++
	}
	return revoked, nil
}

// Token returns the token of the session in this context, or empty when
// there is none yet. A caller needs it to exclude its own session from a
// bulk revocation.
func (s *SessionManager) Token(ctx context.Context) string {
	if s == nil || s.scs == nil {
		return ""
	}
	return s.scs.Token(ctx)
}
