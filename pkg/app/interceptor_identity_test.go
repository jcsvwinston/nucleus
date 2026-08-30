package app

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/router/interceptor"
)

// claimsProbe records what a third-party interceptor could see about the
// caller's identity.
type claimsProbe struct {
	mu     sync.Mutex
	ran    bool
	sawOK  bool
	uid    string
	role   string
	remote string
}

func (p *claimsProbe) snapshot() claimsProbe {
	p.mu.Lock()
	defer p.mu.Unlock()
	return claimsProbe{ran: p.ran, sawOK: p.sawOK, uid: p.uid, role: p.role, remote: p.remote}
}

func registerClaimsProbe(t *testing.T, name string, p *claimsProbe) {
	t.Helper()
	if err := interceptor.Register(name, func(interceptor.Config) (interceptor.Interceptor, error) {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p.mu.Lock()
				p.ran = true
				p.remote = r.RemoteAddr
				if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims != nil {
					p.sawOK = true
					p.uid = claims.UserID
					p.role = claims.Role
				}
				p.mu.Unlock()
				next.ServeHTTP(w, r)
			})
		}, nil
	}); err != nil {
		t.Fatalf("Register(%q): %v", name, err)
	}
	t.Cleanup(func() { interceptor.Unregister(name) })
}

// TestInterceptorsSeeTheDecodedIdentity pins that a third-party interceptor
// is mounted where the identity already exists (QCD-FW-25).
//
// The interceptor chain ran OUTSIDE the JWT decode, so with a valid bearer
// on the wire the interceptor got ok=false from ClaimsFromContext while the
// handler behind it saw the very same request authenticated:
//
//	[A] token válido presente. Authorization visto por el interceptor: "Bearer eyJhbGci…"
//	[A] DENTRO del interceptor : ClaimsFromContext ok=false uid="" role=""
//	[A] EN el handler (control): status=200 role=admin uid=u-42
//
// That is most of the declared usefulness of the seam gone: an interceptor
// that cannot tell who is calling cannot audit, cannot scope a tenant, and
// cannot rate-limit per principal.
func TestInterceptorsSeeTheDecodedIdentity(t *testing.T) {
	const name = "zzclaimsprobe"
	probe := &claimsProbe{}
	registerClaimsProbe(t, name, probe)

	cfg := testAppConfig()
	cfg.JWTSecret = "qcd-fw-25-test-secret-0123456789abcdef"
	cfg.HTTPInterceptors = []string{name}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	if a.JWT == nil {
		t.Fatal("App.JWT is nil despite jwt_secret being configured")
	}

	var handlerSawRole string
	a.Router.Get("/api/mine", func(c *router.Context) error {
		if claims, ok := auth.ClaimsFromContext(c.Request.Context()); ok && claims != nil {
			handlerSawRole = claims.Role
		}
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	})
	if err := a.Authorizer.AddPolicy("u-42", "/api/mine", "read"); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	token, err := a.JWT.Generate("u-42", "alice", "admin")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if rec := bearerGet(t, a, "/api/mine", token); rec.Code != http.StatusOK {
		t.Fatalf("precondition: the request must reach the handler, got %d body=%s", rec.Code, rec.Body.String())
	}

	got := probe.snapshot()
	if !got.ran {
		t.Fatal("precondition: the interceptor never ran")
	}
	// Control: the handler DID see the identity, so the token is fine and
	// the only question is where the interceptor sits.
	if handlerSawRole != "admin" {
		t.Fatalf("precondition: the handler must see the claims, got role=%q", handlerSawRole)
	}
	if !got.sawOK {
		t.Errorf("the interceptor saw no identity (ok=false) on a request the handler saw as role=%q", handlerSawRole)
	}
	if got.uid != "u-42" || got.role != "admin" {
		t.Errorf("interceptor claims: uid=%q role=%q, want u-42/admin", got.uid, got.role)
	}
}

// TestInterceptorsStillRunWithoutAToken is the control: moving the mount
// point must not make the seam depend on there being an identity. An
// unauthenticated request must still pass through the interceptor.
func TestInterceptorsStillRunWithoutAToken(t *testing.T) {
	const name = "zzclaimsprobe2"
	probe := &claimsProbe{}
	registerClaimsProbe(t, name, probe)

	cfg := testAppConfig()
	cfg.JWTSecret = "qcd-fw-25-test-secret-0123456789abcdef"
	cfg.HTTPInterceptors = []string{name}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })

	a.Router.Get("/api/open", func(c *router.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "true"})
	})
	if err := a.Authorizer.AddPolicy("anonymous", "/api/open", "read"); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	bearerGet(t, a, "/api/open", "")

	got := probe.snapshot()
	if !got.ran {
		t.Error("an unauthenticated request must still pass through the interceptor")
	}
	if got.sawOK {
		t.Error("there is no identity on this request; the interceptor must not invent one")
	}
}
