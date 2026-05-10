package hooks

import (
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"
)

// SessionRecorderConfig configures NewSessionRecorder.
type SessionRecorderConfig struct {
	Bus    *observability.Bus
	NodeID string
}

// SessionRecorder is a small façade the framework's session manager calls
// at the three lifecycle points: created, touched, destroyed. It is a
// helper rather than middleware because session lifecycle is driven by
// pkg/auth (which doesn't itself know about HTTP) and we want a typed,
// minimal API surface.
//
// Hooks gate event construction on HasSubscribers(KindSessionChange).
type SessionRecorder struct {
	bus    *observability.Bus
	nodeID string
}

// NewSessionRecorder returns a recorder. If cfg.Bus is nil the recorder is
// a no-op (every method returns immediately).
func NewSessionRecorder(cfg SessionRecorderConfig) *SessionRecorder {
	return &SessionRecorder{bus: cfg.Bus, nodeID: cfg.NodeID}
}

// SessionInfo is the minimal cross-cutting info every session-change event
// needs. The caller is responsible for sanitizing the token (TokenShort
// must already be truncated to a non-reversible prefix).
type SessionInfo struct {
	TokenShort string
	UserID     string
	IP         string
	UserAgent  string
	LastRoute  string
	TraceID    string
}

// Created records that a new session was just created.
func (s *SessionRecorder) Created(info SessionInfo) {
	s.emit(observability.SessionChangeCreated, info)
}

// Touched records that an existing session was observed (request landed,
// session metadata refreshed).
func (s *SessionRecorder) Touched(info SessionInfo) {
	s.emit(observability.SessionChangeTouched, info)
}

// Destroyed records that a session was destroyed (logout, expiration,
// admin revocation).
func (s *SessionRecorder) Destroyed(info SessionInfo) {
	s.emit(observability.SessionChangeDestroyed, info)
}

func (s *SessionRecorder) emit(kind observability.SessionChangeKind, info SessionInfo) {
	if s == nil || s.bus == nil {
		return
	}
	if !s.bus.HasSubscribers(observability.KindSessionChange) {
		return
	}
	if strings.TrimSpace(info.TokenShort) == "" && strings.TrimSpace(info.UserID) == "" {
		// No useful identifier; suppress.
		return
	}

	ev := observability.AcquireSessionChangeEvent(time.Now().UTC(), s.nodeID)
	ev.Change = kind
	ev.TokenShort = strings.TrimSpace(info.TokenShort)
	ev.UserID = strings.TrimSpace(info.UserID)
	ev.IP = strings.TrimSpace(info.IP)
	ev.UserAgent = truncate(strings.TrimSpace(info.UserAgent), 320)
	ev.LastRoute = truncate(strings.TrimSpace(info.LastRoute), 240)
	ev.TraceID = strings.TrimSpace(info.TraceID)

	s.bus.Emit(ev)
}
