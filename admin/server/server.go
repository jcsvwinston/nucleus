// Package server is the top-level admin observability server. Construct
// with New, drive with Run. Implementation details live in the sub-
// packages: nodes, routing, services, auth, ui.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/jcsvwinston/nucleus/admin/server/auth"
	"github.com/jcsvwinston/nucleus/admin/server/nodes"
	"github.com/jcsvwinston/nucleus/admin/server/routing"
	"github.com/jcsvwinston/nucleus/admin/server/services"
	"github.com/jcsvwinston/nucleus/admin/server/ui"

	adminv1connect "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// Server is the assembled admin observability server. It owns two HTTP
// listeners: one for agents and one for UIs/operators. Both serve over
// HTTP/2 (h2c when no TLS config is provided).
type Server struct {
	cfg Config

	state *services.State

	agentSrv *http.Server
	uiSrv    *http.Server

	// listenersMu guards the listener fields. Run writes them once
	// during boot; AgentAddr/UIAddr read them concurrently from test
	// goroutines, so a small mutex is the simplest race-free interface.
	listenersMu   sync.RWMutex
	agentListener net.Listener
	uiListener    net.Listener

	logger *slog.Logger
}

// New constructs a Server. It performs no IO; call Run to start serving.
func New(cfg Config) *Server {
	cfg = cfg.withDefaults()

	state := &services.State{
		Nodes:    nodes.New(),
		EventBus: routing.NewEventBus(),
		Replay: routing.NewReplay(routing.ReplayCapacities{
			HTTP:    cfg.HTTPReplayBufferSize,
			SQL:     cfg.SQLReplayBufferSize,
			Session: cfg.SessionReplayBufferSize,
			Custom:  cfg.CustomReplayBufferSize,
		}),
		Snapshots:      routing.NewSnapshotRouter(0),
		DataStudio:     routing.NewDataStudioRouter(0),
		Logger:         cfg.Logger,
		SendChanBuffer: 64,
		HeartbeatGrace: cfg.AgentInactivityTimeout,
	}

	s := &Server{
		cfg:    cfg,
		state:  state,
		logger: cfg.Logger,
	}

	// AgentService listener: protected by AgentMiddleware. /healthz is
	// carved out BEFORE auth so load balancers and the agent's dialer can
	// probe without owning the token.
	protectedAgent := http.NewServeMux()
	protectedAgent.Handle(adminv1connect.NewAgentServiceHandler(services.NewAgentService(state)))
	agentRoot := http.NewServeMux()
	agentRoot.HandleFunc("/healthz", healthOK)
	agentRoot.Handle("/", auth.AgentMiddleware(cfg.AgentToken)(protectedAgent))

	// UI listener: protected by UIMiddleware. /healthz is carved out the
	// same way; everything else (ControlService, DataStudioService, and
	// the UI assets) goes through the auth chain.
	controlSvc := services.NewControlService(state, cfg.EventChannelSize, cfg.SnapshotTimeout)
	dataStudioSvc := services.NewDataStudioService(state, cfg.SnapshotTimeout)
	protectedUI := http.NewServeMux()
	protectedUI.Handle(adminv1connect.NewControlServiceHandler(controlSvc))
	protectedUI.Handle(adminv1connect.NewDataStudioServiceHandler(dataStudioSvc))
	protectedUI.Handle("/", staticUIHandler())
	uiRoot := http.NewServeMux()
	uiRoot.HandleFunc("/healthz", healthOK)
	uiRoot.Handle("/", auth.UIMiddleware(auth.UIConfig{
		BearerToken:  cfg.UIBearerToken,
		AuthHeader:   cfg.UIAuthHeader,
		EmailHeader:  cfg.UIEmailHeader,
		TrustedCIDRs: cfg.UITrustedProxyCIDRs,
	})(protectedUI))

	s.agentSrv = newH2CServer(agentRoot, cfg.AgentTLS)
	s.uiSrv = newH2CServer(uiRoot, cfg.UITLS)

	return s
}

// State returns the wired-up services.State. Useful for tests.
func (s *Server) State() *services.State { return s.state }

// AgentAddr returns the resolved agent address (after Run has bound).
func (s *Server) AgentAddr() string {
	if s == nil {
		return ""
	}
	s.listenersMu.RLock()
	defer s.listenersMu.RUnlock()
	if s.agentListener == nil {
		return ""
	}
	return s.agentListener.Addr().String()
}

// UIAddr returns the resolved UI address.
func (s *Server) UIAddr() string {
	if s == nil {
		return ""
	}
	s.listenersMu.RLock()
	defer s.listenersMu.RUnlock()
	if s.uiListener == nil {
		return ""
	}
	return s.uiListener.Addr().String()
}

// Run starts both listeners and blocks until ctx is cancelled. Returns
// nil on graceful shutdown, non-nil on listen errors. Not idempotent.
func (s *Server) Run(ctx context.Context) error {
	agentLn, err := net.Listen("tcp", s.cfg.AgentAddr)
	if err != nil {
		return fmt.Errorf("admin server: listen agent %s: %w", s.cfg.AgentAddr, err)
	}
	uiLn, err := net.Listen("tcp", s.cfg.UIAddr)
	if err != nil {
		_ = agentLn.Close()
		return fmt.Errorf("admin server: listen ui %s: %w", s.cfg.UIAddr, err)
	}

	s.listenersMu.Lock()
	s.agentListener = agentLn
	s.uiListener = uiLn
	s.listenersMu.Unlock()

	s.logger.Info("admin server starting",
		"agent_addr", agentLn.Addr(),
		"ui_addr", uiLn.Addr())

	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		err := s.agentSrv.Serve(agentLn)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("agent listener: %w", err)
		}
	}()
	go func() {
		defer wg.Done()
		err := s.uiSrv.Serve(uiLn)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("ui listener: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		shutdownErr := s.shutdown(2 * time.Second)
		wg.Wait()
		return shutdownErr
	case err := <-errCh:
		_ = s.shutdown(time.Second)
		wg.Wait()
		return err
	}
}

// shutdown gracefully closes both listeners. Best-effort; bounded by
// timeout per server.
func (s *Server) shutdown(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var first error
	if err := s.agentSrv.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		first = err
	}
	if err := s.uiSrv.Shutdown(ctx); err != nil && first == nil && !errors.Is(err, context.DeadlineExceeded) {
		first = err
	}
	return first
}

// staticUIHandler serves the embedded admin/ui/dist filesystem. When the
// build pipeline has not produced dist (fresh checkout), it serves the
// PlaceholderHTML page.
func staticUIHandler() http.Handler {
	if uiFS := ui.FS(); uiFS != nil {
		return spaHandler(uiFS)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(ui.PlaceholderHTML))
	})
}

// spaHandler serves static assets from fsys with SPA-style fallback to
// index.html. Hash-based asset filenames cache fine; index.html should
// not, so we set Cache-Control: no-store on it.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(fsys, path); err == nil {
			if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, "/index.html") {
				w.Header().Set("Cache-Control", "no-store")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, "admin UI missing index.html", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(index)
	})
}

func healthOK(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// newH2CServer constructs an http.Server that serves both HTTP/1.1 and
// HTTP/2 cleartext. Connect-RPC bidi streams require HTTP/2; h2c lets
// the agent dial without TLS in dev. Production deployments should set
// tlsConfig and front the listener with a real cert.
func newH2CServer(handler http.Handler, tlsConfig *tls.Config) *http.Server {
	h2s := &http2.Server{}
	wrapped := h2c.NewHandler(handler, h2s)
	srv := &http.Server{
		Handler:           wrapped,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if tlsConfig != nil {
		srv.TLSConfig = tlsConfig
	}
	return srv
}
