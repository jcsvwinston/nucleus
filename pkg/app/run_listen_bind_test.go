// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// listenLine is the message Run logs once the server's socket is bound.
const listenLine = "nucleus: server listening"

// dialOnListen is a slog.Handler that, the moment Run logs the listening
// line, dials the address the line carries: synchronously, inside the
// logging call, which is the earliest a tool that waits for the line can
// connect. NU-115: the line used to be logged before the port was bound,
// so that dial got "connection refused".
type dialOnListen struct {
	mu       sync.Mutex
	messages []string
	seen     bool
	addr     string
	url      string
	dialErr  error
}

func (h *dialOnListen) Enabled(context.Context, slog.Level) bool { return true }
func (h *dialOnListen) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *dialOnListen) WithGroup(string) slog.Handler            { return h }

func (h *dialOnListen) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, r.Message)
	if r.Message != listenLine {
		return nil
	}
	h.seen = true
	r.Attrs(func(attr slog.Attr) bool {
		switch attr.Key {
		case "addr":
			h.addr = attr.Value.String()
		case "url":
			h.url = attr.Value.String()
		}
		return true
	})
	conn, err := net.DialTimeout("tcp", h.addr, 2*time.Second)
	if err == nil {
		_ = conn.Close()
	}
	h.dialErr = err
	return nil
}

func (h *dialOnListen) snapshot() (seen bool, addr, url string, dialErr error, messages []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seen, h.addr, h.url, h.dialErr, append([]string(nil), h.messages...)
}

// newListenTestApp builds a core-only application on 127.0.0.1:port whose
// logger is a dialOnListen.
func newListenTestApp(t *testing.T, port int, mutate func(*Config)) (*App, *dialOnListen) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.Databases = map[string]DatabaseConfig{"default": {URL: "sqlite://:memory:"}}
	if mutate != nil {
		mutate(&cfg)
	}
	a, err := New(&cfg, WithoutDefaults())
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	h := &dialOnListen{}
	a.Logger = slog.New(h)
	return a, h
}

// startRun runs a in the background and returns the channel its result
// arrives on.
func startRun(ctx context.Context, a *App) <-chan error {
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	return done
}

// waitListening waits until h has seen the listening line, failing the test
// if Run returns first or nothing is logged in time.
func waitListening(t *testing.T, h *dialOnListen, done <-chan error) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if seen, _, _, _, _ := h.snapshot(); seen {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("Run returned before logging %q: %v", listenLine, err)
		case <-deadline:
			_, _, _, _, messages := h.snapshot()
			t.Fatalf("Run never logged %q; it logged %q", listenLine, messages)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// stopRun cancels the run and requires a clean return.
func stopRun(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// reservePort finds a free loopback port and releases it.
func reservePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// TestRunLogsListeningOnlyOnceBound pins NU-115: whoever reads the
// listening line and dials the address it carries, immediately, connects.
// With port 0 the line also reports the port the system assigned, not ":0".
func TestRunLogsListeningOnlyOnceBound(t *testing.T) {
	for _, tc := range []struct {
		name string
		port int
	}{
		{"configured port", reservePort(t)},
		{"port 0", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, h := newListenTestApp(t, tc.port, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := startRun(ctx, a)
			waitListening(t, h, done)

			_, addr, url, dialErr, _ := h.snapshot()
			if dialErr != nil {
				t.Fatalf("dialing %s the moment %q was logged: %v", addr, listenLine, dialErr)
			}
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				t.Fatalf("addr %q: %v", addr, err)
			}
			if host != "127.0.0.1" {
				t.Fatalf("addr %q must keep the configured host", addr)
			}
			if tc.port != 0 && port != fmt.Sprint(tc.port) {
				t.Fatalf("addr %q must carry the configured port %d", addr, tc.port)
			}
			if port == "0" {
				t.Fatalf("addr %q must carry the port the system assigned, not 0", addr)
			}
			if url != "http://"+addr {
				t.Fatalf("url %q does not match addr %q", url, addr)
			}

			resp, err := http.Get(url + "/livez")
			if err != nil {
				t.Fatalf("GET %s/livez: %v", url, err)
			}
			_ = resp.Body.Close()
			stopRun(t, cancel, done)
		})
	}
}

// TestRunDoesNotLogListeningWhenThePortIsTaken pins the other half of NU-115:
// a bind that fails is an error from Run and never a listening line.
func TestRunDoesNotLogListeningWhenThePortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port

	a, h := newListenTestApp(t, port, nil)
	select {
	case err := <-startRun(context.Background(), a):
		if err == nil {
			t.Fatal("Run on a taken port must return an error")
		}
		var opErr *net.OpError
		if !errors.As(err, &opErr) || opErr.Op != "listen" {
			t.Fatalf("want the listen error, got %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run on a taken port did not return")
	}
	if seen, _, _, _, messages := h.snapshot(); seen {
		t.Fatalf("Run logged %q although the bind failed; it logged %q", listenLine, messages)
	}
}

// TestRunTLSLogsListeningOnlyOnceBound is NU-115 for the TLS path: the line
// says https, follows the bind, and the server answers TLS on it.
func TestRunTLSLogsListeningOnlyOnceBound(t *testing.T) {
	certFile, keyFile := writeSelfSignedCert(t)
	a, h := newListenTestApp(t, 0, func(c *Config) {
		c.TLSCertFile = certFile
		c.TLSKeyFile = keyFile
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startRun(ctx, a)
	waitListening(t, h, done)

	_, addr, url, dialErr, _ := h.snapshot()
	if dialErr != nil {
		t.Fatalf("dialing %s the moment %q was logged: %v", addr, listenLine, dialErr)
	}
	if url != "https://"+addr {
		t.Fatalf("url %q must be https on addr %q", url, addr)
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed test certificate
			ForceAttemptHTTP2: true,
		},
	}
	resp, err := client.Get(url + "/livez")
	if err != nil {
		t.Fatalf("GET %s/livez: %v", url, err)
	}
	_ = resp.Body.Close()
	if resp.TLS == nil {
		t.Fatal("the server did not answer over TLS")
	}
	// ListenAndServeTLS negotiated HTTP/2; serving the bound listener must
	// keep doing so.
	if resp.ProtoMajor != 2 {
		t.Fatalf("the server answered %s over TLS, want HTTP/2", resp.Proto)
	}
	client.CloseIdleConnections()
	stopRun(t, cancel, done)
}

// TestRunTLSDoesNotLogListeningWithAnUnreadableCertificate: a key pair that
// does not load fails Run before anything is said to listen.
func TestRunTLSDoesNotLogListeningWithAnUnreadableCertificate(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	for _, f := range []string{certFile, keyFile} {
		if err := os.WriteFile(f, []byte("not a PEM block\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a, h := newListenTestApp(t, 0, func(c *Config) {
		c.TLSCertFile = certFile
		c.TLSKeyFile = keyFile
	})
	select {
	case err := <-startRun(context.Background(), a):
		if err == nil || !strings.Contains(err.Error(), "Run serve") {
			t.Fatalf("Run with an unreadable key pair must fail as before, got %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run with an unreadable key pair did not return")
	}
	if seen, _, _, _, messages := h.snapshot(); seen {
		t.Fatalf("Run logged %q although the key pair did not load; it logged %q", listenLine, messages)
	}
}

// writeSelfSignedCert writes a self-signed certificate for 127.0.0.1 and its
// key, PEM-encoded, into a temporary directory.
func writeSelfSignedCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}
