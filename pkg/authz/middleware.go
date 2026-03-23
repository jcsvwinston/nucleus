package authz

import (
	"net/http"
	"strings"

	"github.com/goframe/goframe/pkg/auth"
	gferrors "github.com/goframe/goframe/pkg/errors"
)

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
// the HTTP method to a CRUD action.
func (e *Enforcer) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				gferrors.WriteError(w, gferrors.Unauthorized("authentication required"), nil)
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
				gferrors.WriteError(w, gferrors.Forbidden("you do not have permission to perform this action"), nil)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole returns middleware that checks if the authenticated user has
// one of the specified roles.
func (e *Enforcer) RequireRole(roles ...string) func(http.Handler) http.Handler {
	roleSet := make(map[string]bool, len(roles))
	for _, r := range roles {
		roleSet[r] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				gferrors.WriteError(w, gferrors.Unauthorized("authentication required"), nil)
				return
			}

			// Check direct role from JWT claims
			if roleSet[claims.Role] {
				next.ServeHTTP(w, r)
				return
			}

			// Check Casbin role assignments
			userRoles := e.GetRoles(claims.UserID)
			for _, ur := range userRoles {
				if roleSet[ur] {
					next.ServeHTTP(w, r)
					return
				}
			}

			gferrors.WriteError(w, gferrors.Forbidden("insufficient role"), nil)
		})
	}
}
