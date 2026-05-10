// Package connection establishes and maintains the agent's transport to
// an admin server. It is responsible for endpoint resolution, dial-time
// failover across the configured endpoint list, and exponential backoff
// when every endpoint is unreachable.
//
// The package does NOT own the bidi stream itself; that lives in
// admin/agent/stream. connection.Dialer.Dial returns a connected
// AgentService client and the endpoint URL that succeeded; the stream
// layer owns the call.
package connection

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"

	adminv1connect "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// Config controls how the dialer probes endpoints and times out.
type Config struct {
	// Endpoints is the ordered list of admin server URLs to try. Each
	// entry is a full URL — http://, https://, or h2c:// for unencrypted
	// HTTP/2. The dialer tries them in order on each attempt.
	Endpoints []string

	// Token, if non-empty, is sent on every Connect-RPC call as
	// "Authorization: Bearer <Token>". Used for the simplest auth mode
	// (decision 13: shared bearer token).
	Token string

	// TLSConfig is applied to every https:// endpoint. Pass nil to use the
	// system defaults.
	TLSConfig *tls.Config

	// HealthCheckTimeout caps each endpoint probe (an HTTP GET to /healthz
	// on the same origin). Default 3s.
	HealthCheckTimeout time.Duration

	// InitialBackoff is the first sleep after a complete failover round
	// fails. Default 1s.
	InitialBackoff time.Duration

	// MaxBackoff caps the exponential growth. Default 30s (decision 9).
	MaxBackoff time.Duration

	// BackoffJitter is multiplied by rand[0,1) and added to each backoff
	// to avoid thundering-herd. Default 0.5.
	BackoffJitter float64

	// Logger is used for the rate-limited disconnect WARN. Pass nil for
	// slog.Default.
	Logger *slog.Logger
}

func (c Config) withDefaults() Config {
	if c.HealthCheckTimeout <= 0 {
		c.HealthCheckTimeout = 3 * time.Second
	}
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Second
	}
	if c.BackoffJitter < 0 {
		c.BackoffJitter = 0
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Dialer attempts to establish a connection to one of the configured
// endpoints. It is safe to construct once and call Dial repeatedly.
type Dialer struct {
	cfg Config

	// connectClient is the HTTP/2-capable client Connect-RPC needs for the
	// bidi stream. Holds an http2.Transport configured for h2c.
	connectClient *http.Client

	// healthClient is a vanilla HTTP client for the /healthz probe. Using
	// the default transport keeps the probe interoperable with HTTP/1.1
	// servers (e.g. httptest.NewServer in tests) AND with the real
	// HTTP/2 admin server.
	healthClient *http.Client

	mu             sync.Mutex
	currentBackoff time.Duration
	lastWarnAt     time.Time
}

// NewDialer constructs a Dialer.
func NewDialer(cfg Config) *Dialer {
	cfg = cfg.withDefaults()
	return &Dialer{
		cfg:           cfg,
		connectClient: newConnectHTTPClient(cfg.TLSConfig),
		healthClient: &http.Client{
			Timeout: cfg.HealthCheckTimeout,
		},
	}
}

// Result describes a successful Dial.
type Result struct {
	Client   adminv1connect.AgentServiceClient
	Endpoint string
}

// Dial tries each endpoint in order, advancing past the ones that fail
// the health probe. Returns the first one that completes successfully.
// When every endpoint fails, Dial returns the last error and the caller
// should sleep on Backoff() before retrying.
//
// Dial respects ctx cancellation and returns ctx.Err() promptly.
func (d *Dialer) Dial(ctx context.Context) (*Result, error) {
	if d == nil {
		return nil, errors.New("admin agent: nil dialer")
	}
	endpoints := d.cfg.Endpoints
	if len(endpoints) == 0 {
		return nil, errors.New("admin agent: no admin endpoints configured")
	}

	var lastErr error
	for _, ep := range endpoints {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		ep = strings.TrimSpace(ep)
		if ep == "" {
			continue
		}
		if err := healthCheck(ctx, d.healthClient, ep, d.cfg.Token, d.cfg.HealthCheckTimeout); err != nil {
			lastErr = fmt.Errorf("endpoint %s: %w", ep, err)
			d.cfg.Logger.Debug("admin agent endpoint probe failed",
				"endpoint", ep, "error", err)
			continue
		}
		client := d.newClient(ep)
		d.resetBackoff()
		return &Result{Client: client, Endpoint: ep}, nil
	}

	if lastErr == nil {
		lastErr = errors.New("no endpoints available")
	}
	d.warnRateLimited(lastErr)
	return nil, lastErr
}

// Backoff returns the duration the caller should wait before the next
// Dial. Grows exponentially up to MaxBackoff with jitter; resets to
// InitialBackoff on the next successful Dial.
//
// Backoff advances internal state, so call it exactly once per failed
// Dial round.
func (d *Dialer) Backoff() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.currentBackoff <= 0 {
		d.currentBackoff = d.cfg.InitialBackoff
	}
	sleep := d.currentBackoff
	jitter := d.cfg.BackoffJitter
	if jitter <= 0 {
		jitter = 0.5
	}
	sleep += time.Duration(rand.Float64() * float64(sleep) * jitter)

	d.currentBackoff *= 2
	if d.currentBackoff > d.cfg.MaxBackoff {
		d.currentBackoff = d.cfg.MaxBackoff
	}
	return sleep
}

func (d *Dialer) resetBackoff() {
	d.mu.Lock()
	d.currentBackoff = 0
	d.lastWarnAt = time.Time{}
	d.mu.Unlock()
}

// warnRateLimited emits at most one WARN per minute regardless of how
// many failed Dial rounds occur (decision 9 / Phase 3 point 6).
func (d *Dialer) warnRateLimited(err error) {
	d.mu.Lock()
	now := time.Now()
	if !d.lastWarnAt.IsZero() && now.Sub(d.lastWarnAt) < time.Minute {
		d.mu.Unlock()
		return
	}
	d.lastWarnAt = now
	d.mu.Unlock()

	d.cfg.Logger.Warn("admin agent cannot reach admin server", "error", err.Error())
}

func (d *Dialer) newClient(endpoint string) adminv1connect.AgentServiceClient {
	opts := []connect.ClientOption{}
	if t := strings.TrimSpace(d.cfg.Token); t != "" {
		opts = append(opts, connect.WithInterceptors(bearerInterceptor(t)))
	}
	return adminv1connect.NewAgentServiceClient(d.connectClient, endpoint, opts...)
}

func bearerInterceptor(token string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer "+token)
			return next(ctx, req)
		}
	}
}

// newConnectHTTPClient builds an HTTP/2-only client.
//
// Background:
//   * Connect-RPC bidi streams require HTTP/2.
//   * We want to support both h2c (plaintext, dev) and HTTPS (prod, Phase 6).
//   * golang.org/x/net/http2.Transport with AllowHTTP=true upgrades plain
//     http:// URLs to h2c. The exact dial path depends on the URL scheme:
//     for "http://" the transport uses its DialTLSContext too (it does not
//     have a separate Dial path when AllowHTTP is true), passing the
//     TLSClientConfig as cfg. That means our DialTLSContext receives a
//     non-nil cfg even on h2c URLs when TLSClientConfig is set; we have
//     to detect "this is h2c" by some other signal.
//
// We use the simplest signal that works: when TLSClientConfig is nil on
// the transport, DialTLSContext returns plain TCP. When it is non-nil, we
// honour the TLS handshake. The agent passes a non-nil TLSClientConfig
// only when admin.tls.* is configured in nucleus.yml (Phase 6); for
// today's h2c-only paths it stays nil.
func newConnectHTTPClient(tlsConfig *tls.Config) *http.Client {
	useTLS := tlsConfig != nil
	t := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			if !useTLS {
				var nd net.Dialer
				return nd.DialContext(ctx, network, addr)
			}
			d := &tls.Dialer{Config: tlsConfig}
			return d.DialContext(ctx, network, addr)
		},
	}
	if useTLS {
		t.TLSClientConfig = tlsConfig
	}
	return &http.Client{Transport: t}
}

// healthCheck pings GET /healthz on the endpoint origin to verify the
// admin server is reachable before opening a stream.
func healthCheck(ctx context.Context, cli *http.Client, endpoint, token string, timeout time.Duration) error {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	healthURL := strings.TrimRight(endpoint, "/") + "/healthz"
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("build health request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return fmt.Errorf("health get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}
