package router

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Request ID
// ---------------------------------------------------------------------------

type reqIDKeyType struct{}

var reqIDKey = reqIDKeyType{}

// RequestID generates a unique request identifier and stores it in the request
// context. The ID is also written as the X-Request-Id response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = generateRequestID()
		}
		ctx := context.WithValue(r.Context(), reqIDKey, id)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetReqID returns the request ID from context, or empty string.
func GetReqID(ctx context.Context) string {
	if id, ok := ctx.Value(reqIDKey).(string); ok {
		return id
	}
	return ""
}

func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// ---------------------------------------------------------------------------
// Real IP
// ---------------------------------------------------------------------------

// RealIP rewrites r.RemoteAddr with the client IP taken from X-Forwarded-For /
// X-Real-IP — but ONLY when the immediate peer is a trusted proxy. On its own
// (the exported middleware) no proxies are trusted, so forwarding headers are
// ignored and r.RemoteAddr is left untouched. Use the router's
// WithTrustedProxies option (wired from the `trusted_proxies` config key) to
// honor forwarding headers behind a known load balancer. Trusting these
// headers unconditionally lets any client spoof its IP — evading per-IP rate
// limits and poisoning audit logs (H-N3).
func RealIP(next http.Handler) http.Handler {
	return realIPMiddleware(nil)(next)
}

// realIPMiddleware builds the RealIP middleware bound to a trusted-proxy
// matcher. A nil/empty matcher trusts no proxy and never rewrites RemoteAddr.
func realIPMiddleware(trusted *trustedProxyMatcher) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rip := realIPFromRequest(r, trusted); rip != "" {
				r.RemoteAddr = rip
			}
			next.ServeHTTP(w, r)
		})
	}
}

// realIPFromRequest returns the forwarded client IP for r, or "" if the
// forwarding headers must not be trusted (peer is not a trusted proxy, or none
// are configured). When the peer is trusted it walks X-Forwarded-For from the
// right and returns the rightmost address that is not itself a trusted proxy —
// the real client as seen by the outermost trusted hop — falling back to
// X-Real-IP. Returning "" signals the caller to leave r.RemoteAddr unchanged.
func realIPFromRequest(r *http.Request, trusted *trustedProxyMatcher) string {
	if !trusted.trusts(r.RemoteAddr) {
		return ""
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(parts[i])
			if ip == "" || trusted.trusts(ip) {
				continue
			}
			return ip
		}
	}
	// X-Real-IP is filtered the same way the X-Forwarded-For walk above is:
	// an address that is ITSELF a trusted proxy is not a client, so the
	// header carries nothing this hop can vouch for.
	//
	// Without the filter the fallback was honoured verbatim, and under a
	// catch-all `trusted_proxies` that made it a spoofing vector (QCD-FW-18):
	// every peer is trusted, the walk above skips every hop because they are
	// all trusted, and whatever X-Real-IP said became the client — a forged
	// address, rate-limit evasion one header at a time, and an audit trail
	// recording the attacker's choice. `doctor --check security` reports that
	// configuration; this stops the runtime from honouring a header it cannot
	// verify even when nobody ran doctor.
	//
	// A correctly configured deployment sees no change: a load balancer sets
	// X-Real-IP to a real client, and a real client is not in trusted_proxies.
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" && !trusted.trusts(xrip) {
		return xrip
	}
	return ""
}

// trustedProxyMatcher tests whether a network address belongs to the
// configured set of trusted upstream proxies. The zero value (and nil) trust
// nothing.
type trustedProxyMatcher struct {
	nets []*net.IPNet
}

// newTrustedProxyMatcher parses IP and CIDR entries into a matcher. Blank and
// unparseable entries are skipped. A bare IP matches only itself.
func newTrustedProxyMatcher(entries []string) *trustedProxyMatcher {
	m := &trustedProxyMatcher{}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, ipnet, err := net.ParseCIDR(e); err == nil {
			m.nets = append(m.nets, ipnet)
			continue
		}
		if ip := net.ParseIP(e); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			m.nets = append(m.nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return m
}

// trusts reports whether addr (an "ip", "ip:port", or "[ipv6]:port") falls
// within any configured trusted-proxy range.
func (m *trustedProxyMatcher) trusts(addr string) bool {
	if m == nil || len(m.nets) == 0 {
		return false
	}
	host := strings.TrimSpace(addr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range m.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Recoverer
// ---------------------------------------------------------------------------

// Recoverer catches panics in downstream handlers, logs the stack trace, and
// returns a 500 Internal Server Error response. It logs through the process
// default logger; DefaultStack uses RecovererWithLogger so the panic goes
// through the application's handler (redaction, attributes, sink) like every
// other line.
func Recoverer(next http.Handler) http.Handler {
	return RecovererWithLogger(nil)(next)
}

// RecovererWithLogger is Recoverer logging through logger; nil falls back to
// slog.Default().
func RecovererWithLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rv := recover(); rv != nil {
					l := logger
					if l == nil {
						l = slog.Default()
					}
					l.Error("panic recovered",
						"error", rv,
						"stack", string(debug.Stack()),
						"method", r.Method,
						"path", r.URL.Path,
					)
					if !headerWritten(w) {
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------------
// Timeout
// ---------------------------------------------------------------------------

// TimeoutMiddleware wraps the stdlib http.TimeoutHandler to cancel requests
// that exceed the given duration. It automatically skips WebSocket upgrades
// and requests that accept text/event-stream.
func TimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return TimeoutMiddlewareWithExemptions(timeout, nil)
}

// TimeoutMiddlewareWithExemptions is TimeoutMiddleware with URL path
// prefixes that bypass it. http.TimeoutHandler buffers the response and
// hides http.Flusher, so a streaming handler behind it cannot flush a byte
// until it returns; an exempt subtree gets the raw writer and no deadline.
// A timeout <= 0 disables the middleware for every route.
func TimeoutMiddlewareWithExemptions(timeout time.Duration, exemptPrefixes []string) func(http.Handler) http.Handler {
	timeoutBody := `{"error":{"code":"TIMEOUT","message":"request timeout"}}`
	return func(next http.Handler) http.Handler {
		if timeout <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsWebSocketUpgrade(r) || acceptsEventStream(r) || pathHasPrefix(r.URL.Path, exemptPrefixes) {
				next.ServeHTTP(w, r)
				return
			}
			http.TimeoutHandler(next, timeout, timeoutBody).ServeHTTP(w, r)
		})
	}
}

// acceptsEventStream reports an SSE client: EventSource sends
// `Accept: text/event-stream`, and a buffered, deadlined writer can never
// serve one.
func acceptsEventStream(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

func pathHasPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if path == p || strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Compress
// ---------------------------------------------------------------------------

// Compress returns middleware that gzip-compresses response bodies for clients
// that accept gzip encoding. level follows compress/flate constants.
func Compress(level int) func(http.Handler) http.Handler {
	if level < flate.HuffmanOnly {
		level = flate.DefaultCompression
	}
	if level > flate.BestCompression {
		level = flate.BestCompression
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsWebSocketUpgrade(r) {
				next.ServeHTTP(w, r)
				return
			}
			// The representation varies on Accept-Encoding whether or not THIS
			// response is compressed — a shared cache that misses the header
			// could serve a gzip body to a client that never asked for it
			// (cache poisoning), or the uncompressed variant to everyone.
			w.Header().Add("Vary", "Accept-Encoding")
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				next.ServeHTTP(w, r)
				return
			}

			// The decision to compress is taken on the first write, once the
			// status and the Content-Type are known (NU-36): a 204, a 304, an
			// image or an already-encoded body go through untouched instead
			// of being wrapped in a gzip stream that carries nothing.
			gw := &gzipResponseWriter{ResponseWriter: w, level: level}
			defer gw.close()
			next.ServeHTTP(gw, r)
		})
	}
}

type gzipResponseWriter struct {
	http.ResponseWriter
	level   int
	gz      *gzip.Writer
	decided bool
}

// decide fixes, once, whether this response is compressed. It runs on the
// first WriteHeader or Write, when status and Content-Type are settled.
func (g *gzipResponseWriter) decide(status int) {
	if g.decided {
		return
	}
	g.decided = true
	h := g.Header()
	switch {
	case status < http.StatusOK, status == http.StatusNoContent, status == http.StatusNotModified:
		return
	case h.Get("Content-Encoding") != "", h.Get("Content-Range") != "":
		return
	case !compressibleContentType(h.Get("Content-Type")):
		return
	}
	gz, err := gzip.NewWriterLevel(g.ResponseWriter, g.level)
	if err != nil {
		return
	}
	g.gz = gz
	h.Set("Content-Encoding", "gzip")
	h.Del("Content-Length")
}

// compressibleContentType is the allow-list of representations worth a gzip
// stream: text, JSON, XML, JavaScript and SVG. Everything else — images,
// video, archives, octet streams, an unknown type — is already compressed
// or opaque, and a gzip wrapper only costs CPU and bytes.
func compressibleContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch {
	case ct == "":
		return false
	case strings.HasPrefix(ct, "text/"):
		return true
	case strings.HasSuffix(ct, "+json"), strings.HasSuffix(ct, "+xml"):
		return true
	}
	switch ct {
	case "application/json", "application/javascript", "application/ecmascript",
		"application/xml", "application/x-www-form-urlencoded", "image/svg+xml",
		"application/wasm", "application/x-ndjson":
		return true
	}
	return false
}

func (g *gzipResponseWriter) WriteHeader(status int) {
	g.decide(status)
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.decided {
		// Mirror net/http: with no Content-Type set, the first bytes decide
		// it. Sniff here, before they are gzip bytes.
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.decide(http.StatusOK)
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	if !g.decided {
		g.decide(http.StatusOK)
	}
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipResponseWriter) close() {
	if g.gz != nil {
		_ = g.gz.Close()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (g *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return g.ResponseWriter
}

// headerWritten reports whether a response writer in the chain already
// wrote its header, unwrapping through Unwrap() and the WroteHeader()
// accessor the wrappers in this package expose.
func headerWritten(w http.ResponseWriter) bool {
	for w != nil {
		if hw, ok := w.(interface{ WroteHeader() bool }); ok {
			return hw.WroteHeader()
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return false
		}
		w = u.Unwrap()
	}
	return false
}

// Hijack implements http.Hijacker if the underlying writer supports it.
func (g *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := g.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("hijacker not supported by the underlying writer")
}

// ---------------------------------------------------------------------------
// WrapResponseWriter
// ---------------------------------------------------------------------------

// WrapResponseWriter is a response writer wrapper that captures the HTTP status
// code and the number of bytes written. It replaces chi/middleware's equivalent.
type WrapResponseWriter struct {
	http.ResponseWriter
	status       int
	bytesWritten int
	wroteHeader  bool
	mu           sync.Mutex
}

// NewWrapResponseWriter creates a new WrapResponseWriter. The protoMajor
// argument is accepted for API compatibility but currently unused.
func NewWrapResponseWriter(w http.ResponseWriter, _ int) *WrapResponseWriter {
	return &WrapResponseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (w *WrapResponseWriter) WriteHeader(code int) {
	w.mu.Lock()
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(code)
	}
	w.mu.Unlock()
}

func (w *WrapResponseWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	if !w.wroteHeader {
		w.status = http.StatusOK
		w.wroteHeader = true
	}
	w.mu.Unlock()
	n, err := w.ResponseWriter.Write(b)
	w.mu.Lock()
	w.bytesWritten += n
	w.mu.Unlock()
	return n, err
}

// Status returns the HTTP status code that was written.
func (w *WrapResponseWriter) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

// BytesWritten returns the total bytes written to the response body.
func (w *WrapResponseWriter) BytesWritten() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.bytesWritten
}

// WroteHeader reports whether the status line has been written (via an
// explicit WriteHeader or an implicit first Write). Recoverer consults it
// to avoid a second WriteHeader after a mid-response panic.
func (w *WrapResponseWriter) WroteHeader() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.wroteHeader
}

// Unwrap returns the underlying ResponseWriter for middleware compatibility.
func (w *WrapResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush implements http.Flusher if the underlying writer supports it.
func (w *WrapResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker if the underlying writer supports it.
func (w *WrapResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("hijacker not supported by the underlying writer")
}

func IsWebSocketUpgrade(r *http.Request) bool {
	if r == nil {
		return false
	}
	connection := strings.ToLower(strings.TrimSpace(r.Header.Get("Connection")))
	upgrade := strings.ToLower(strings.TrimSpace(r.Header.Get("Upgrade")))
	return strings.Contains(connection, "upgrade") && upgrade == "websocket"
}
