package app

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected into a pipe and returns
// what was written. app.New builds its logger on os.Stdout, so this is the
// only seam a test has on the boot log without a logger option.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() {
			os.Stdout = orig
			_ = w.Close()
		}()
		fn()
	}()
	return <-done
}

// TestAppNew_NoSigningMaterialIsInfoOutsideProduction: a fresh scaffold
// has no jwt_secret and issues no tokens. In development that is the
// normal state of a new project and boots at INFO, so the clean scaffold's
// boot log carries zero WARN lines; production keeps the WARN because an
// unset secret there is a deployment gap.
func TestAppNew_NoSigningMaterialIsInfoOutsideProduction(t *testing.T) {
	const line = "jwt: no signing material configured"
	for _, tc := range []struct {
		env       string
		wantLevel string
	}{
		{"development", "level=INFO"},
		{"production", "level=WARN"},
	} {
		t.Run(tc.env, func(t *testing.T) {
			cfg := testAppConfig()
			cfg.Env = tc.env
			cfg.LogFormat = "text"
			cfg.LogLevel = "info"
			out := captureStdout(t, func() {
				a, err := New(cfg)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				_ = a.Shutdown(context.Background())
			})
			var found string
			for _, l := range strings.Split(out, "\n") {
				if strings.Contains(l, line) {
					found = l
					break
				}
			}
			if found == "" {
				t.Fatalf("boot log never mentioned %q:\n%s", line, out)
			}
			if !strings.Contains(found, tc.wantLevel) {
				t.Fatalf("env=%s: the no-signing-material line must be logged at %s, got:\n%s", tc.env, tc.wantLevel, found)
			}
		})
	}
}
