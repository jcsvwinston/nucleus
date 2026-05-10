package hooks

import (
	"log/slog"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"
)

// TestSessionRecorder_NoSubscribers_NoEmit verifies the hot-path gate.
func TestSessionRecorder_NoSubscribers_NoEmit(t *testing.T) {
	bus := observability.NewBus(slog.New(slog.DiscardHandler))
	rec := NewSessionRecorder(SessionRecorderConfig{Bus: bus, NodeID: "n"})

	rec.Created(SessionInfo{TokenShort: "abcd1234", UserID: "u-1"})
	rec.Touched(SessionInfo{TokenShort: "abcd1234"})
	rec.Destroyed(SessionInfo{TokenShort: "abcd1234"})

	if got := bus.Stats(observability.KindSessionChange); got.Emitted != 0 {
		t.Fatalf("emitted = %d, want 0", got.Emitted)
	}
}

// TestSessionRecorder_Emits verifies all three lifecycle methods.
func TestSessionRecorder_Emits(t *testing.T) {
	bus := observability.NewBus(slog.New(slog.DiscardHandler))
	sub, cancel := bus.Subscribe(observability.Filter{Kinds: []observability.EventKind{observability.KindSessionChange}}, nil)
	defer func() {
		cancel()
		for {
			select {
			case ev := <-sub.Ch():
				ev.Release()
			default:
				return
			}
		}
	}()

	rec := NewSessionRecorder(SessionRecorderConfig{Bus: bus, NodeID: "node-y"})

	rec.Created(SessionInfo{TokenShort: "ab12cd34", UserID: "u-1", IP: "1.2.3.4", LastRoute: "/login"})
	rec.Touched(SessionInfo{TokenShort: "ab12cd34", UserID: "u-1", IP: "1.2.3.4", LastRoute: "/dashboard"})
	rec.Destroyed(SessionInfo{TokenShort: "ab12cd34", UserID: "u-1"})

	want := []observability.SessionChangeKind{
		observability.SessionChangeCreated,
		observability.SessionChangeTouched,
		observability.SessionChangeDestroyed,
	}

	for i, w := range want {
		select {
		case ev := <-sub.Ch():
			s, ok := ev.(*observability.SessionChangeEvent)
			if !ok {
				t.Fatalf("got %T", ev)
			}
			if s.Change != w {
				t.Errorf("event[%d].Change = %s, want %s", i, s.Change, w)
			}
			if s.NodeID() != "node-y" {
				t.Errorf("event[%d].NodeID = %q", i, s.NodeID())
			}
			if s.TokenShort != "ab12cd34" {
				t.Errorf("event[%d].TokenShort = %q", i, s.TokenShort)
			}
			s.Release()
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
}

// TestSessionRecorder_SuppressedWithoutIdentifier verifies that an event
// with neither token nor user is silently dropped (not interesting to ship).
func TestSessionRecorder_SuppressedWithoutIdentifier(t *testing.T) {
	bus := observability.NewBus(slog.New(slog.DiscardHandler))
	_, cancel := bus.Subscribe(observability.Filter{}, nil)
	defer cancel()

	rec := NewSessionRecorder(SessionRecorderConfig{Bus: bus, NodeID: "n"})
	rec.Touched(SessionInfo{IP: "1.2.3.4"}) // no token, no user

	if got := bus.Stats(observability.KindSessionChange); got.Emitted != 0 {
		t.Fatalf("emitted = %d, want 0 (suppressed)", got.Emitted)
	}
}

// TestSessionRecorder_NilSafe verifies a nil recorder doesn't panic.
func TestSessionRecorder_NilSafe(t *testing.T) {
	var rec *SessionRecorder
	rec.Created(SessionInfo{TokenShort: "x"}) // must not panic
	rec.Touched(SessionInfo{TokenShort: "x"})
	rec.Destroyed(SessionInfo{TokenShort: "x"})
}
