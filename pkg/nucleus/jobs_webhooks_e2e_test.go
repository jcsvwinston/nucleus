package nucleus

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// freeLocalPort asks the OS for a currently-free TCP port. The tiny window
// between closing the probe listener and Run binding it is acceptable for a
// test.
func freeLocalPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probing for a free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// TestRun_JobsAndWebhooksEndToEnd boots a real application through the
// public Run surface — no internal shortcuts — with one module that declares
// both surfaces, and proves over the wire that:
//
//  1. the module's interval job is scheduled and executed repeatedly by the
//     jobs runtime (memory provider);
//  2. its webhook route is mounted under the webhooks prefix and a
//     correctly-signed POST reaches the handler (200) — with csrf_enabled
//     true, which also proves the automatic CSRF exemption of the prefix,
//     since an unexempted cookie-less POST would be rejected by the token
//     check;
//  3. a wrong signature is 401 and a wrong method 405, both without the
//     handler running;
//  4. SIGTERM shuts the whole thing down cleanly (Run returns nil).
func TestRun_JobsAndWebhooksEndToEnd(t *testing.T) {
	port := freeLocalPort(t)

	var jobRuns atomic.Int64
	var webhookHits atomic.Int64

	modDef := Module[struct{}]{
		Name: "e2e",
		Jobs: func(j JobRegistry, _ struct{}) {
			_ = j.Register("tick", JobSpec{
				Every:   time.Second,
				Handler: func(context.Context) error { jobRuns.Add(1); return nil },
			})
		},
		Webhooks: func(w WebhookRegistry, _ struct{}) {
			_ = w.Register("/github", WebhookSpec{
				Secret: "e2e-secret",
				Handler: func(rw http.ResponseWriter, r *http.Request) {
					webhookHits.Add(1)
					rw.WriteHeader(http.StatusOK)
				},
			})
		},
	}

	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.CSRFEnabled = true
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "e2e.db")},
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(App{
			Config:  cfg,
			Options: []app.Option{app.WithoutDefaults()},
			Modules: map[string]ModuleSpec{"e2e": modDef.Build()},
		})
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}

	// Wait for the server to accept requests.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("server did not come up within 10s")
		}
		resp, err := client.Get(base + "/")
		if err == nil {
			resp.Body.Close()
			break
		}
		select {
		case err := <-runDone:
			t.Fatalf("Run exited during startup: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
	}

	hookURL := base + "/webhooks/e2e/github"
	body := `{"event":"push"}`

	post := func(sig string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, hookURL, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if sig != "" {
			req.Header.Set(WebhookSignatureHeader, sig)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", hookURL, err)
		}
		resp.Body.Close()
		return resp
	}

	// Correctly signed POST → 200. csrf_enabled is on and this request has
	// no CSRF token, so a 200 also proves the prefix exemption.
	if resp := post(SignWebhookBody("e2e-secret", []byte(body))); resp.StatusCode != http.StatusOK {
		t.Fatalf("signed webhook: want 200, got %d", resp.StatusCode)
	}
	if webhookHits.Load() != 1 {
		t.Fatalf("handler must have run once, got %d", webhookHits.Load())
	}

	// Wrong signature → 401, handler untouched.
	if resp := post(SignWebhookBody("wrong-secret", []byte(body))); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("mis-signed webhook: want 401, got %d", resp.StatusCode)
	}
	// Missing signature → 401.
	if resp := post(""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned webhook: want 401, got %d", resp.StatusCode)
	}
	// Wrong method → 405.
	getResp, err := client.Get(hookURL)
	if err != nil {
		t.Fatalf("GET %s: %v", hookURL, err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET webhook: want 405, got %d", getResp.StatusCode)
	}
	if webhookHits.Load() != 1 {
		t.Fatalf("rejected requests must not reach the handler, got %d hits", webhookHits.Load())
	}

	// The interval job ticks for real (memory provider floor is 1s).
	waitForRuns(t, &jobRuns, 2, 10*time.Second)

	// SIGTERM → graceful shutdown, Run returns nil.
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run after SIGTERM: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not shut down within 15s of SIGTERM")
	}
}
