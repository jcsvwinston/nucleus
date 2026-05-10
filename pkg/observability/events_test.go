package observability

import (
	"testing"
	"time"
)

// TestAcquireRelease_RoundTrip exercises the basic pool lifecycle for every
// concrete event type. After Release, refs must be 0 and the event is
// poolable.
func TestAcquireRelease_RoundTrip(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		acq  func() Event
	}{
		{"http", func() Event { return AcquireHTTPRequestEvent(now, "n") }},
		{"sql", func() Event { return AcquireSQLStatementEvent(now, "n") }},
		{"session", func() Event { return AcquireSessionChangeEvent(now, "n") }},
		{"custom", func() Event { return AcquireCustomEvent(now, "n") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.acq()
			if e.Kind() == KindUnknown {
				t.Fatal("kind unknown")
			}
			if !e.EmittedAt().Equal(now) {
				t.Fatalf("EmittedAt mismatch: %v vs %v", e.EmittedAt(), now)
			}
			if e.NodeID() != "n" {
				t.Fatalf("NodeID = %q, want n", e.NodeID())
			}

			// One ref expected.
			ref := e.(interface{ refsLoad() int32 }).refsLoad()
			if ref != 1 {
				t.Fatalf("refs after Acquire = %d, want 1", ref)
			}

			e.Release()

			ref = e.(interface{ refsLoad() int32 }).refsLoad()
			if ref != 0 {
				t.Fatalf("refs after Release = %d, want 0", ref)
			}
		})
	}
}

// TestRelease_PanicOnUnderflow verifies the over-release detection.
func TestRelease_PanicOnUnderflow(t *testing.T) {
	e := AcquireHTTPRequestEvent(time.Now(), "n")
	e.Release()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on second Release")
		}
	}()
	e.Release()
}

// TestSQLArgs_Preserved verifies that the Args slice survives the
// pool-reset and is reusable on next Acquire.
func TestSQLArgs_Preserved(t *testing.T) {
	first := AcquireSQLStatementEvent(time.Now(), "n")
	first.Args = append(first.Args, "string(5):***", "int:42")
	first.Release()

	second := AcquireSQLStatementEvent(time.Now(), "n")
	if cap(second.Args) < 2 {
		t.Fatalf("Args capacity not preserved across pool: cap=%d", cap(second.Args))
	}
	if len(second.Args) != 0 {
		t.Fatalf("Args length not zeroed across pool: len=%d", len(second.Args))
	}
	second.Release()
}

// TestEventKind_String covers the stringer for stable metric labels.
func TestEventKind_String(t *testing.T) {
	cases := map[EventKind]string{
		KindUnknown:       "unknown",
		KindHTTPRequest:   "http_request",
		KindSQLStatement:  "sql_statement",
		KindSessionChange: "session_change",
		KindCustom:        "custom",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d) = %q, want %q", k, got, want)
		}
	}
}

// TestSessionChangeKind_String covers the inner enum stringer.
func TestSessionChangeKind_String(t *testing.T) {
	cases := map[SessionChangeKind]string{
		SessionChangeUnspecified: "unspecified",
		SessionChangeCreated:     "created",
		SessionChangeTouched:     "touched",
		SessionChangeDestroyed:   "destroyed",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d) = %q, want %q", k, got, want)
		}
	}
}
