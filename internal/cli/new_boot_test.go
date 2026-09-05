package cli

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunNewScaffoldBootsWithoutWarningsAndAnswers404 boots what `nucleus
// new` writes, untouched, for both starter templates and demands over
// real HTTP what the quickstart promises of a clean project:
//
//   - the boot log carries zero WARN lines (the jwt line is INFO outside
//     production; the Prometheus line already was);
//   - a path nobody serves answers 404 — GET and POST — instead of the
//     403/419 the pre-routing gates used to answer, and on the mvc
//     template a policy row for a route nothing serves does not change
//     that.
//
// Gated behind -short like the other scaffold builds.
func TestRunNewScaffoldBootsWithoutWarningsAndAnswers404(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and boots the scaffold; skipped with -short")
	}
	repoRoot := repoRootForTest(t)

	for _, tmpl := range []string{"api", "mvc"} {
		tmpl := tmpl
		t.Run(tmpl, func(t *testing.T) {
			outDir := t.TempDir()
			port := freeLoopbackPort(t)

			var stdout, stderr bytes.Buffer
			args := []string{"bootcheck", "--out", outDir, "--template", tmpl, "--module", "example.com/bootcheck", "--port", fmt.Sprint(port)}
			if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatalf("runNew(%s): %v\nstderr: %s", tmpl, err, stderr.String())
			}
			projectDir := filepath.Join(outDir, "bootcheck")
			pinGoModToLocalNucleus(t, projectDir, repoRoot)
			runGoCommand(t, projectDir, "mod", "tidy")
			runGoCommand(t, projectDir, "build", "-o", "app", ".")

			app := exec.Command(filepath.Join(projectDir, "app"))
			app.Dir = projectDir
			var appLog bytes.Buffer
			app.Stdout = &appLog
			app.Stderr = &appLog
			if err := app.Start(); err != nil {
				t.Fatal(err)
			}
			// exec.Cmd.Wait (not os.Process.Wait) also waits for the
			// goroutine copying the process output into appLog, so the
			// buffer is only read once nothing writes to it.
			stopped := false
			stop := func() {
				if stopped {
					return
				}
				stopped = true
				_ = app.Process.Kill()
				_ = app.Wait()
			}
			t.Cleanup(stop)

			base := fmt.Sprintf("http://127.0.0.1:%d", port)
			client := &http.Client{Timeout: 2 * time.Second}
			waitForHealthz(t, client, base, func() string { stop(); return appLog.String() })

			probes := []struct {
				method string
				path   string
				want   int
			}{
				{http.MethodGet, "/healthz", http.StatusOK},
				{http.MethodGet, "/nope", http.StatusNotFound},
				{http.MethodPost, "/nope", http.StatusNotFound},
			}
			if tmpl == "mvc" {
				// rbac_policy.csv grants anonymous /notes, but the clean
				// scaffold mounts no notes module: the grant must not turn
				// a missing route into anything but the mux's 404.
				probes = append(probes, struct {
					method string
					path   string
					want   int
				}{http.MethodGet, "/notes", http.StatusNotFound})
			}
			for _, p := range probes {
				req, err := http.NewRequest(p.method, base+p.path, strings.NewReader(""))
				if err != nil {
					t.Fatal(err)
				}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("%s %s: %v", p.method, p.path, err)
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != p.want {
					t.Errorf("%s %s on the clean %s scaffold: want %d, got %d body=%s", p.method, p.path, tmpl, p.want, resp.StatusCode, body)
				}
			}

			stop()
			log := appLog.String()
			if n := strings.Count(log, "level=WARN"); n != 0 {
				t.Errorf("the clean %s scaffold booted with %d WARN line(s); want 0:\n%s", tmpl, n, log)
			}
			if !strings.Contains(log, "level=INFO") {
				t.Errorf("expected the boot log of the %s scaffold to be captured; got:\n%s", tmpl, log)
			}
		})
	}
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// waitForHealthz polls /healthz; on timeout it calls logAfterStop, which
// must stop the process before reading its log.
func waitForHealthz(t *testing.T, client *http.Client, base string, logAfterStop func() string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("app did not come up on %s\nlog:\n%s", base, logAfterStop())
		}
		resp, err := client.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}
