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
// as success; either way the protected handler is NOT called. The signature
// deliberately omits next — do not capture the wrapped handler in a closure and
// call it from OnDeny, which would grant access.
type DenialHandler func(w http.ResponseWriter, r *http.Request, d Denial)

// SubjectResolver derives the policy subject (the sub in Enforcer.Can) from a
// request and its claims. Supply one via AuthzOptions to check policies keyed by
// something other than the user id — e.g. an app whose policy table is keyed by
// role (rather than by user via Casbin grouping rules) returns claims.Role. When
// nil, Middleware uses claims.UserID.
type SubjectResolver func(r *http.Request, claims *auth.Claims) string

// ActionResolver derives the policy action (the act in Enforcer.Can) from a
// request. Supply one via AuthzOptions to express actions the HTTP method alone
// cannot — e.g. a pure-HTML SSR form POSTs to both update and delete routes, so
// a resolver maps a POST whose path ends in /delete to "delete" rather than the
// default "create". When nil, Middleware maps the HTTP method (GET→read,
// POST→create, PUT/PATCH→update, DELETE→delete).
type ActionResolver func(r *http.Request) string

// AuthzOptions configures how Middleware/RequireRole answer a denial and, for
// Middleware, how the policy subject and action are derived. The zero value
// preserves the default behaviour (a JSON error envelope, subject = claims.UserID,
// action = HTTP method), so existing callers are unaffected.
type AuthzOptions struct {
	// OnDeny, when non-nil, is invoked instead of writing the default JSON
	// error envelope whenever a request is rejected (whether unauthenticated
	// or forbidden). SSR applications set this; JSON APIs leave it nil.
	OnDeny DenialHandler

	// ResolveSubject, when non-nil, overrides the policy subject Middleware
	// checks (default: claims.UserID). RequireRole ignores it — it matches the
	// claim's role directly, not through the policy store.
	ResolveSubject SubjectResolver

	// ResolveAction, when non-nil, overrides the policy action Middleware checks
	// (default: the HTTP-method mapping). RequireRole ignores it.
	ResolveAction ActionResolver
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

// MiddlewareWithOptions is Middleware with a configurable denial response and,
// optionally, custom subject/action derivation. opts.OnDeny replaces the default
// JSON envelope on every rejection; opts.ResolveSubject and opts.ResolveAction
// override how the policy subject and action are derived from the request
// (defaults: claims.UserID and the HTTP-method mapping). The zero AuthzOptions
// value preserves Middleware's behaviour exactly.
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

			subject := claims.UserID
			if opts.ResolveSubject != nil {
				subject = opts.ResolveSubject(r, claims)
			}
			resource := r.URL.Path
			action := httpMethodToAction(r.Method)
			if opts.ResolveAction != nil {
				action = opts.ResolveAction(r)
			}

			// A resolver that returns "" yields a query that matches no policy,
			// so the request is safely denied under default-deny — but that is
			// indistinguishable from a genuine policy miss. Warn so a
			// misconfigured resolver is auditable rather than silently 403-ing.
			if subject == "" || action == "" {
				e.logger.Warn("authz: resolver returned empty subject or action; request will be denied",
					"user", claims.UserID, "subject", subject, "action", action, "resource", resource)
			}

			if !e.Can(subject, resource, action) {
				e.logger.Info("authz denied",
					// "user" is the raw claims.UserID; "subject" is the policy key
					// actually checked — they differ when ResolveSubject is set.
					"subject", subject,
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
