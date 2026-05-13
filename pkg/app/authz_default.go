package app

import (
	"net/http"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

// buildDefaultAuthzMiddleware returns the framework's default-deny
// authorization middleware mounted by App.New per ADR-004.
//
// Difference from `(*authz.Enforcer).Middleware()`:
//
//   - `Enforcer.Middleware` returns 401 Unauthorized when no JWT
//     claims are present in context. That is the right behaviour for
//     a route operator who explicitly wires both JWT + RBAC for an
//     authenticated API.
//   - This middleware treats absent claims as the anonymous subject
//     (`authz.BootstrapSubject`). It is the right behaviour for a
//     framework-wide default-deny mount because the bootstrap allow-
//     list grants anonymous access to the framework-owned routes
//     (`/healthz`, `/metrics`, `/admin/login`, `/login`,
//     `/.well-known/jwks.json`, `/static/*`). Routes outside the
//     allow-list return 403 Forbidden for unauthenticated callers,
//     not 401, because the surface is "this user (anonymous) is not
//     permitted" rather than "no credentials supplied".
//
// Operators who want the stricter 401 behaviour on specific routes
// can mount `Enforcer.Middleware()` over that subtree explicitly.
func buildDefaultAuthzMiddleware(enf *authz.Enforcer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject := authz.BootstrapSubject
			if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims != nil && claims.UserID != "" {
				subject = claims.UserID
			}
			action := httpMethodToAction(r.Method)
			if !enf.Can(subject, r.URL.Path, action) {
				gferrors.WriteError(w, r, gferrors.Forbidden("you do not have permission to perform this action"), nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// httpMethodToAction mirrors the CRUD mapping in pkg/authz/middleware.go
// without exporting that helper. Kept local so the package boundary on
// pkg/authz stays minimal.
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
