package cli

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// TestRunServe_RejectsPositionalArgs covers the argument-validation guard for
// both the default and --without-defaults branches (R3 / ADR-013): the new bool
// flag parses in either position, and serve still rejects stray positional
// arguments before it ever attempts to construct or run the server. The
// happy-path branches both end in a blocking a.Run, so the behavioural
// difference of WithoutDefaults() is covered at the pkg/app and pkg/nucleus
// layers rather than here.
func TestRunServe_RejectsPositionalArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"default", []string{"stray"}},
		{"without-defaults", []string{"--without-defaults", "stray"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			err := runServe(tc.args, strings.NewReader(""), &out, &errOut)
			if err == nil {
				t.Fatal("expected an error for positional arguments")
			}
			if !strings.Contains(err.Error(), "positional") {
				t.Fatalf("expected a positional-arg error, got %v", err)
			}
		})
	}
}

// TestRunServe_RejectsUnknownFlag confirms flag parsing is strict. Paired with
// the without-defaults case above, it pins that --without-defaults is a *known*
// flag: an unknown flag errors, the known one parses and only trips the
// positional-arg guard.
func TestRunServe_RejectsUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	err := runServe([]string{"--definitely-not-a-flag"}, strings.NewReader(""), &out, &errOut)
	if err == nil {
		t.Fatal("expected a parse error for an unknown flag")
	}
}

// writeServeConfig writes a configuration for a core-only application on
// 127.0.0.1:port and returns its path.
func writeServeConfig(t *testing.T, port int) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	cfg := fmt.Sprintf(
		"host: 127.0.0.1\nport: %d\ndatabase_default: default\ndatabases:\n  default:\n    url: sqlite://%s\nlog_level: error\nlog_format: text\n",
		port, filepath.Join(dir, "app.db"),
	)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config %s: %v", cfgPath, err)
	}
	return cfgPath
}

// TestRunServe_PortTakenPrintsNoListeningLine pins NU-115 for the CLI: serve
// printed "Nucleus server listening on …" before the server bound its port,
// so a taken port printed the line and then failed.
func TestRunServe_PortTakenPrintsNoListeningLine(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	cfgPath := writeServeConfig(t, taken.Addr().(*net.TCPAddr).Port)

	var out, errOut bytes.Buffer
	if err := runServe([]string{"--config", cfgPath, "--without-defaults"}, strings.NewReader(""), &out, &errOut); err == nil {
		t.Fatal("serve on a taken port must fail")
	}
	if strings.Contains(out.String(), "listening") {
		t.Fatalf("serve said it was listening although the bind failed:\n%s", out.String())
	}
}

// dialOnServeLine is the stdout of serve as a tool that waits for the
// listening line sees it: the moment the line is written, it dials the
// address the line names.
type dialOnServeLine struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	line    string
	dialErr error
}

func (w *dialOnServeLine) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	for _, line := range strings.Split(string(p), "\n") {
		rest, ok := strings.CutPrefix(line, "Nucleus server listening on http://")
		if !ok {
			continue
		}
		w.line = line
		conn, err := net.DialTimeout("tcp", rest, 2*time.Second)
		if err == nil {
			_ = conn.Close()
		}
		w.dialErr = err
	}
	return len(p), nil
}

func (w *dialOnServeLine) snapshot() (line, all string, dialErr error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.line, w.buf.String(), w.dialErr
}

// TestServeApp_ListeningLineFollowsTheBind: the line serve prints is there
// to be waited for, so whoever dials on it connects — with port 0 too, which
// the line reports as the port the system assigned.
func TestServeApp_ListeningLineFollowsTheBind(t *testing.T) {
	for _, port := range []int{0, freeServePort(t)} {
		t.Run(fmt.Sprintf("port %d", port), func(t *testing.T) {
			cfg, err := loadConfig(writeServeConfig(t, port))
			if err != nil {
				t.Fatal(err)
			}
			a, err := app.New(cfg, app.WithoutDefaults())
			if err != nil {
				t.Fatal(err)
			}
			stdout := &dialOnServeLine{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- serveApp(ctx, a, stdout) }()

			deadline := time.After(10 * time.Second)
			for {
				line, all, dialErr := stdout.snapshot()
				if line != "" {
					if dialErr != nil {
						t.Fatalf("dialing on %q: %v", line, dialErr)
					}
					if strings.HasSuffix(line, ":0") {
						t.Fatalf("the line must name the bound port, not 0: %q", line)
					}
					if port != 0 && !strings.HasSuffix(line, fmt.Sprintf("127.0.0.1:%d", port)) {
						t.Fatalf("the line must name the configured address: %q", line)
					}
					break
				}
				select {
				case err := <-done:
					t.Fatalf("serve returned before printing the listening line: %v\n%s", err, all)
				case <-deadline:
					t.Fatalf("serve never printed the listening line:\n%s", all)
				case <-time.After(10 * time.Millisecond):
				}
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("serve returned error: %v", err)
			}
		})
	}
}

func freeServePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
