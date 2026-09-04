package auth

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// SessionMetaFirstSeenAtKey stores the first server-side observation timestamp (RFC3339).
	SessionMetaFirstSeenAtKey = "__nucleus_first_seen_at"
	// SessionMetaLastSeenAtKey stores the latest server-side observation timestamp (RFC3339).
	SessionMetaLastSeenAtKey = "__nucleus_last_seen_at"
	// SessionMetaPodKey stores the pod identifier handling the session request.
	SessionMetaPodKey = "__nucleus_runtime_pod"
	// SessionMetaHostKey stores the host/node identifier handling the session request.
	SessionMetaHostKey = "__nucleus_runtime_host"
	// SessionMetaInstanceKey stores a composed runtime instance identifier.
	SessionMetaInstanceKey = "__nucleus_runtime_instance"
	// SessionMetaRemoteIPKey stores the latest observed client IP for the session.
	SessionMetaRemoteIPKey = "__nucleus_remote_ip"
)

const defaultRuntimeMetadataInterval = 30 * time.Second

// SessionRuntimeIdentity describes the runtime node that handled a session request.
type SessionRuntimeIdentity struct {
	Pod      string
	Host     string
	Instance string
}

// DetectSessionRuntimeIdentity infers pod and host identifiers from common
// Kubernetes/runtime environment variables.
func DetectSessionRuntimeIdentity() SessionRuntimeIdentity {
	kubernetes := isLikelyKubernetesRuntime()

	pod := firstNonEmpty(
		os.Getenv("POD_NAME"),
		os.Getenv("K8S_POD_NAME"),
		os.Getenv("POD"),
	)
	if pod == "" && kubernetes {
		pod = strings.TrimSpace(os.Getenv("HOSTNAME"))
	}

	host := firstNonEmpty(
		os.Getenv("NODE_NAME"),
		os.Getenv("K8S_NODE_NAME"),
		os.Getenv("POD_NODE_NAME"),
		os.Getenv("HOST_NODE_NAME"),
	)
	if host == "" && !kubernetes {
		host = strings.TrimSpace(os.Getenv("HOSTNAME"))
	}
	if host == "" {
		resolved, err := os.Hostname()
		if err == nil {
			host = strings.TrimSpace(resolved)
		}
	}
	if kubernetes && pod != "" && host == pod {
		// Avoid misleading duplicate pod/host values when node identity is unavailable.
		host = ""
	}

	instance := strings.TrimSpace(os.Getenv("NUCLEUS_INSTANCE_ID"))
	if instance == "" {
		switch {
		case pod != "" && host != "":
			instance = pod + "@" + host
		case pod != "":
			instance = pod
		case host != "":
			instance = host
		}
	}

	return SessionRuntimeIdentity{
		Pod:      pod,
		Host:     host,
		Instance: instance,
	}
}

func isLikelyKubernetesRuntime() bool {
	return firstNonEmpty(
		os.Getenv("KUBERNETES_SERVICE_HOST"),
		os.Getenv("KUBERNETES_PORT"),
		os.Getenv("POD_NAME"),
		os.Getenv("POD_NAMESPACE"),
		os.Getenv("NODE_NAME"),
		os.Getenv("K8S_NODE_NAME"),
		os.Getenv("K8S_POD_NAME"),
	) != ""
}

// RuntimeMetadataMiddleware updates runtime metadata fields in existing sessions
// so shared stores can expose where each session is actively served.
func RuntimeMetadataMiddleware(sm *SessionManager, identity SessionRuntimeIdentity, minInterval time.Duration) func(http.Handler) http.Handler {
	if sm == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	if minInterval <= 0 {
		minInterval = defaultRuntimeMetadataInterval
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Skip if there is no committed session token to avoid creating session
			// rows for anonymous requests by default.
			if strings.TrimSpace(sm.SCS().Token(ctx)) != "" {
				now := time.Now().UTC()
				if shouldWriteRuntimeMetadata(sm, ctx, now, minInterval) {
					firstSeen := strings.TrimSpace(sm.GetString(ctx, SessionMetaFirstSeenAtKey))
					if firstSeen == "" {
						sm.Put(ctx, SessionMetaFirstSeenAtKey, now.Format(time.RFC3339))
					}
					sm.Put(ctx, SessionMetaLastSeenAtKey, now.Format(time.RFC3339))
					if identity.Pod != "" {
						sm.Put(ctx, SessionMetaPodKey, identity.Pod)
					}
					if identity.Host != "" {
						sm.Put(ctx, SessionMetaHostKey, identity.Host)
					}
					if identity.Instance != "" {
						sm.Put(ctx, SessionMetaInstanceKey, identity.Instance)
					}
					if ip := ClientIPFromRequest(r); ip != "" {
						sm.Put(ctx, SessionMetaRemoteIPKey, ip)
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ClientIPFromRequest returns the client IP of a request: the host part of
// r.RemoteAddr.
//
// It reads no forwarding header on purpose. X-Forwarded-For and X-Real-IP
// are set by whoever sent the request unless a proxy the deployment trusts
// overwrote them, and the router's RealIP middleware already rewrites
// RemoteAddr from those headers — for the proxies listed in
// trusted_proxies, and for nobody else. Reading the headers again here
// would let any client choose the IP that lands in its session metadata
// and in the audit trail, which is exactly what trusted_proxies exists to
// prevent.
func ClientIPFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}

	remote := strings.TrimSpace(r.RemoteAddr)
	if remote == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remote)
	if err == nil {
		return strings.TrimSpace(host)
	}
	return remote
}

func shouldWriteRuntimeMetadata(sm *SessionManager, ctx context.Context, now time.Time, minInterval time.Duration) bool {
	lastSeenRaw := strings.TrimSpace(sm.GetString(ctx, SessionMetaLastSeenAtKey))
	if lastSeenRaw == "" {
		return true
	}
	lastSeen, err := time.Parse(time.RFC3339, lastSeenRaw)
	if err != nil {
		return true
	}
	return now.Sub(lastSeen) >= minInterval
}

func firstNonEmpty(values ...string) string {
	for _, raw := range values {
		if v := strings.TrimSpace(raw); v != "" {
			return v
		}
	}
	return ""
}
