package auth

import (
	"context"
	"errors"
	"fmt"
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
// session store does not support enumeration (for example the cookie store,
// which keeps no server-side copy to iterate).
var ErrSessionStoreNotIterable = errors.New("auth: session store does not support enumeration")

// ErrNilSessionManager is returned by ActiveSessions when called on a nil or
// zero-initialised SessionManager (one not created via NewSessionManager).
var ErrNilSessionManager = errors.New("auth: nil session manager")

// ActiveSessions returns a decoded snapshot of every session currently held by
// the store, for a session-management/observability surface (e.g. an admin
// "active sessions" view). It requires a store that supports enumeration — the
// memory, SQL, Redis and Memcached stores do; a store that does not (the cookie
// store, or any custom store implementing neither All nor AllCtx) yields
// ErrSessionStoreNotIterable.
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
