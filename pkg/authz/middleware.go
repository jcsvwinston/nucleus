package authz

import (
	"net/http"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

// Denial describes an authorization rejection handed to a DenialHandler. It
// lets an application choose how to answer — redirect an anonymous visitor to a
// login page, or render a styled 403 for a signed-in user who lacks the
// required role/permission — instead of the default JSON error envelope. This
// is what makes the authz middlewares usable from a server-rendered (SSR) UI,
// not only a JSON API.
type Denial struct {
	// Status is the HTTP status the default renderer would use:
	// http.StatusUnauthorized (401) when no identity was present, or
	// http.StatusForbidden (403) when the request was authenticated but not
	// permitted.
	Status int

	// Authenticated reports whether the request carried identity (JWT claims).
	// When false the visitor is anonymous — a login redirect is usually the
	// right response; when true they are signed in but lack the role or
	// permission — render a 403.
	Authenticated bool

	// Reason is the human-readable explanation (e.g. "insufficient role").
	Reason string
}

// DenialHandler answers an authorization denial. Supply one via AuthzOptions to
// render a page or issue a redirect; when nil, the middleware writes the default
// JSON error envelope. The handler owns the response from this point — it must
// write a status and body and must not call the next handler. If it returns
// without writing a status, net/http sends an empty 200 OK, which a client reads
// as success; either way the protected handler is NOT called.
type DenialHandler func(w http.ResponseWriter, r *http.Request, d Denial)

// AuthzOptions configures how Middleware/RequireRole answer a denial. The zero
// value preserves the default behaviour (a JSON error envelope), so existing
// callers are unaffected.
type AuthzOptions struct {
	// OnDeny, when non-nil, is invoked instead of writing the default JSON
	// error envelope whenever a request is rejected (whether unauthenticated
	// or forbidden). SSR applications set this; JSON APIs leave it nil.
	OnDeny DenialHandler
}

// httpMethodToAction maps HTTP methods to CRUD action names.
func httpMethodToAction(method string) string {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead:
		return "read"
	case http.MethodPost:
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return "read"
	}
}

// Middleware returns a Chi middleware that checks authorization using the enforcer.
// It extracts the user identity from the JWT claims in the context (set by
// auth.JWTManager.Middleware), derives the resource from the URL path, and maps
// the HTTP method to a CRUD action. Denials are answered with a JSON error
// envelope; for an SSR-friendly page or redirect, use MiddlewareWithOptions.
func (e *Enforcer) Middleware() func(http.Handler) http.Handler {
	return e.MiddlewareWithOptions(AuthzOptions{})
}

// MiddlewareWithOptions is Middleware with a configurable denial response. When
// opts.OnDeny is set it is invoked on every rejection (instead of the default
// JSON envelope); otherwise the behaviour is identical to Middleware.
func (e *Enforcer) MiddlewareWithOptions(opts AuthzOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				e.deny(w, r, opts, Denial{
					Status:        http.StatusUnauthorized,
					Authenticated: false,
					Reason:        "authentication required",
				})
				return
			}

			resource := r.URL.Path
			action := httpMethodToAction(r.Method)

			if !e.Can(claims.UserID, resource, action) {
				e.logger.Info("authz denied",
					"user", claims.UserID,
					"resource", resource,
					"action", action,
				)
				e.deny(w, r, opts, Denial{
					Status:        http.StatusForbidden,
					Authenticated: true,
					Reason:        "you do not have permission to perform this action",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole returns middleware that checks if the authenticated user has one
// of the specified roles. Denials are answered with a JSON error envelope; for
// an SSR-friendly page or redirect, use RequireRoleWithOptions.
func (e *Enforcer) RequireRole(roles ...string) func(http.Handler) http.Handler {
	return e.RequireRoleWithOptions(AuthzOptions{}, roles...)
}

// RequireRoleWithOptions is RequireRole with a configurable denial response.
// When opts.OnDeny is set it is invoked on every rejection (instead of the
// default JSON envelope); otherwise the behaviour is identical to RequireRole.
func (e *Enforcer) RequireRoleWithOptions(opts AuthzOptions, roles ...string) func(http.Handler) http.Handler {
	roleSet := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		roleSet[r] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				e.deny(w, r, opts, Denial{
					Status:        http.StatusUnauthorized,
					Authenticated: false,
					Reason:        "authentication required",
				})
				return
			}

			// Check direct role from JWT claims
			if _, ok := roleSet[claims.Role]; ok {
				next.ServeHTTP(w, r)
				return
			}

			// Check Casbin role assignments
			userRoles := e.GetRoles(claims.UserID)
			for _, ur := range userRoles {
				if _, ok := roleSet[ur]; ok {
					next.ServeHTTP(w, r)
					return
				}
			}

			e.deny(w, r, opts, Denial{
				Status:        http.StatusForbidden,
				Authenticated: true,
				Reason:        "insufficient role",
			})
			return
		})
	}
}

// deny answers a rejected request: the caller's OnDeny handler when set,
// otherwise the default JSON error envelope. The default path reproduces the
// prior behaviour exactly — 401 for an anonymous request, 403 for an
// authenticated one — so callers that don't opt in see no change.
func (e *Enforcer) deny(w http.ResponseWriter, r *http.Request, opts AuthzOptions, d Denial) {
	if opts.OnDeny != nil {
		opts.OnDeny(w, r, d)
		return
	}
	if d.Authenticated {
		gferrors.WriteError(w, r, gferrors.Forbidden(d.Reason), nil)
		return
	}
	gferrors.WriteError(w, r, gferrors.Unauthorized(d.Reason), nil)
}
