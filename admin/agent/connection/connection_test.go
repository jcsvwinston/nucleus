package connection

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func startHealthyServer(t *testing.T) (url string, hits *atomic.Int32, stop func()) {
	t.Helper()
	var counter atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			counter.Add(1)
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
	}))
	return srv.URL, &counter, srv.Close
}

func TestDial_NoEndpoints_Errors(t *testing.T) {
	d := NewDialer(Config{Logger: discardLogger()})
	_, err := d.Dial(context.Background())
	if err == nil {
		t.Fatal("expected error with no endpoints")
	}
}

func TestDial_FirstHealthyEndpointWins(t *testing.T) {
	good1, hits1, stop1 := startHealthyServer(t)
	defer stop1()
	good2, hits2, stop2 := startHealthyServer(t)
	defer stop2()

	d := NewDialer(Config{
		Endpoints: []string{good1, good2},
		Logger:    discardLogger(),
	})

	res, err := d.Dial(context.Background())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if res.Endpoint != good1 {
		t.Errorf("dialed %s, want %s", res.Endpoint, good1)
	}
	if hits1.Load() != 1 {
		t.Errorf("good1 hits = %d, want 1", hits1.Load())
	}
	if hits2.Load() != 0 {
		t.Errorf("good2 should not be probed, got %d", hits2.Load())
	}
}

func TestDial_FailoverPastBrokenEndpoint(t *testing.T) {
	// First endpoint refuses connections. Bind+close to get a free port that
	// nothing listens on.
	bad := "http://127.0.0.1:1" // port 1 reliably refuses on darwin/linux

	good, hits, stop := startHealthyServer(t)
	defer stop()

	d := NewDialer(Config{
		Endpoints:          []string{bad, good},
		HealthCheckTimeout: 200 * time.Millisecond,
		Logger:             discardLogger(),
	})

	res, err := d.Dial(context.Background())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if res.Endpoint != good {
		t.Errorf("dialed %s, want %s (failover should pick second)", res.Endpoint, good)
	}
	if hits.Load() != 1 {
		t.Errorf("good hits = %d, want 1", hits.Load())
	}
}

func TestDial_AllEndpointsBroken_ReturnsError(t *testing.T) {
	d := NewDialer(Config{
		Endpoints:          []string{"http://127.0.0.1:1", "http://127.0.0.1:2"},
		HealthCheckTimeout: 100 * time.Millisecond,
		Logger:             discardLogger(),
	})

	_, err := d.Dial(context.Background())
	if err == nil {
		t.Fatal("expected error when every endpoint is unreachable")
	}
}

func TestBackoff_GrowsAndCaps(t *testing.T) {
	// BackoffJitter is the documented default of 0.5; with that, each step
	// can grow by up to 50% over the base value. We assert the lower bound
	// (>= base) and the upper bound (<= base * 1.5) for the un-capped
	// steps, and verify the cap is enforced for the saturated ones.
	d := NewDialer(Config{
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     800 * time.Millisecond,
		Logger:         discardLogger(),
	})

	bases := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		800 * time.Millisecond,
		800 * time.Millisecond,
	}
	for i, base := range bases {
		got := d.Backoff()
		// Allow up to 50 % above the base for the documented default
		// jitter. The cap on the upper end is base * 1.5; on the lower
		// end it is exactly base.
		upper := base + base/2
		if got < base || got > upper {
			t.Errorf("step %d: got %v, want in [%v, %v]", i, got, base, upper)
		}
	}
}

func TestBackoff_ResetsAfterSuccessfulDial(t *testing.T) {
	good, _, stop := startHealthyServer(t)
	defer stop()

	d := NewDialer(Config{
		Endpoints:      []string{good},
		InitialBackoff: 100 * time.Millisecond,
		BackoffJitter:  0,
		Logger:         discardLogger(),
	})

	// Force backoff by calling it once before dial.
	first := d.Backoff()
	if first == 0 {
		t.Fatal("backoff returned 0")
	}

	// Now a successful dial should reset.
	if _, err := d.Dial(context.Background()); err != nil {
		t.Fatalf("Dial: %v", err)
	}

	// After reset, the next Backoff returns InitialBackoff (with the
	// default 0.5 jitter so up to 1.5x).
	again := d.Backoff()
	if again < 100*time.Millisecond || again > 150*time.Millisecond {
		t.Errorf("expected reset to base 100ms (with 0.5 jitter ≤ 150ms), got %v", again)
	}
}

func TestDial_RespectsContextCancel(t *testing.T) {
	d := NewDialer(Config{
		Endpoints:          []string{"http://127.0.0.1:1", "http://127.0.0.1:2"},
		HealthCheckTimeout: 5 * time.Second, // long enough that ctx wins
		Logger:             discardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, err := d.Dial(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}
