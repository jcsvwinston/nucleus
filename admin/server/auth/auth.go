// Package auth holds the two thin auth surfaces of the admin server:
//
//   - Agent: shared bearer token (today) or mTLS (Phase 6). Validated on
//     every Connect-RPC call from an agent.
//   - UI: trusted-proxy header pass-through (X-Auth-User / X-Auth-Email)
//     with optional bearer fallback. Per decision 14, the canonical
//     deployment runs oauth2-proxy or equivalent in front of the UI
//     listener; the server does NOT implement OIDC itself.
package auth

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strings"
)

// Identity is the minimal set of facts the rest of the server may rely
// on after a request has been authenticated. Currently only used for
// observability + audit.
type Identity struct {
	Subject string // "agent:<NodeID>" for agents, the UI user for UI
	Email   string // empty for agents, optional for UI
	Role    string // "agent" | "ui-operator"
}

// AgentMiddleware returns an http middleware that enforces shared-token
// auth on the agent listener. When token is empty the middleware is a
// pass-through (the listener is presumed bound to a private network or
// using mTLS at the listener layer).
func AgentMiddleware(token string) func(http.Handler) http.Handler {
	expected := strings.TrimSpace(token)
	return func(next http.Handler) http.Handler {
		if expected == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := bearerFromHeader(r)
			if subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UIConfig groups the UI-listener auth knobs.
type UIConfig struct {
	// BearerToken, when non-empty, lets a request authenticate by sending
	// "Authorization: Bearer <BearerToken>". The token is checked AFTER
	// trusted-proxy header pass-through.
	BearerToken string

	// AuthHeader and EmailHeader name the headers the trusted reverse
	// proxy uses to forward identity (X-Auth-User / X-Auth-Email by
	// default, configurable per Config.UIAuthHeader / UIEmailHeader).
	AuthHeader  string
	EmailHeader string

	// TrustedCIDRs is the allowlist of remote addresses whose
	// trusted-proxy headers are honoured. Empty = "127.0.0.1/32, ::1/128".
	TrustedCIDRs []string
}

// UIMiddleware authenticates UI requests. It returns a generic 401 on
// failure rather than leaking which credential mode was attempted.
func UIMiddleware(cfg UIConfig) func(http.Handler) http.Handler {
	trusted := parseCIDRs(cfg.TrustedCIDRs)
	authHeader := strings.TrimSpace(cfg.AuthHeader)
	if authHeader == "" {
		authHeader = "X-Auth-User"
	}
	emailHeader := strings.TrimSpace(cfg.EmailHeader)
	if emailHeader == "" {
		emailHeader = "X-Auth-Email"
	}
	bearer := strings.TrimSpace(cfg.BearerToken)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1) trusted-proxy header path
			if from := remoteIP(r); from != nil && cidrsContain(trusted, from) {
				if user := strings.TrimSpace(r.Header.Get(authHeader)); user != "" {
					next.ServeHTTP(w, r)
					return
				}
			}
			// 2) bearer fallback
			if bearer != "" {
				got := bearerFromHeader(r)
				if subtle.ConstantTimeCompare([]byte(got), []byte(bearer)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
}

// IdentityFromRequest extracts the configured identity. Returns
// ("", "") for the agent path; UIs may have non-empty values.
func IdentityFromRequest(r *http.Request, cfg UIConfig) Identity {
	if r == nil {
		return Identity{Role: "unknown"}
	}
	user := strings.TrimSpace(r.Header.Get(cfg.AuthHeader))
	if user != "" {
		return Identity{
			Subject: user,
			Email:   strings.TrimSpace(r.Header.Get(cfg.EmailHeader)),
			Role:    "ui-operator",
		}
	}
	if strings.TrimSpace(bearerFromHeader(r)) != "" {
		return Identity{Subject: "ui-bearer", Role: "ui-operator"}
	}
	return Identity{Role: "ui-anonymous"}
}

// ErrTrustedProxyMisconfigured is returned by parseCIDRs when an entry
// is malformed and the caller asked to fail-fast.
var ErrTrustedProxyMisconfigured = errors.New("admin server: malformed trusted_cidrs entry")

func parseCIDRs(cidrs []string) []*net.IPNet {
	if len(cidrs) == 0 {
		// Defaults: localhost only.
		_, v4loop, _ := net.ParseCIDR("127.0.0.1/32")
		_, v6loop, _ := net.ParseCIDR("::1/128")
		return []*net.IPNet{v4loop, v6loop}
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			continue
		}
		out = append(out, network)
	}
	return out
}

func cidrsContain(networks []*net.IPNet, ip net.IP) bool {
	for _, n := range networks {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func remoteIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

func bearerFromHeader(r *http.Request) string {
	if r == nil {
		return ""
	}
	v := strings.TrimSpace(r.Header.Get("Authorization"))
	if v == "" {
		return ""
	}
	parts := strings.SplitN(v, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
