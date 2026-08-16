package app

import (
	"fmt"
	"log/slog"
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
//     (`authz.BootstrapSubject`), and with claims present it tries the
//     token's user id, then its role, then anonymous — first subject
//     the policy allows wins (QCD-FW-1). It is the right behaviour for a
//     framework-wide default-deny mount because the bootstrap allow-
//     list grants anonymous access to the framework-owned routes
//     (`/healthz`, `/metrics` — unless `metrics_public: false` —,
//     `/login`, `/.well-known/jwks.json`, `/static/*`; see
//     authz.BootstrapAllowList). Routes outside the
//     allow-list return 403 Forbidden for unauthenticated callers,
//     not 401, because the surface is "this user (anonymous) is not
//     permitted" rather than "no credentials supplied".
//
// Operators who want the stricter 401 behaviour on specific routes
// can mount `Enforcer.Middleware()` over that subtree explicitly.
func buildDefaultAuthzMiddleware(enf *authz.Enforcer, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Subject resolution (QCD-FW-1): a request is allowed when ANY
			// of its subjects passes — the token's user id, the token's
			// role, then `anonymous`. The anonymous fallback means an
			// authenticated caller never loses what the bootstrap
			// allow-list grants everyone; the role subject is what makes
			// the CSV role policies AUTH_GUIDE documents work at the
			// global layer. Claims reach the context via the
			// OptionalJWTMiddleware that App.New mounts ahead of this
			// middleware whenever JWT signing material is configured.
			subjects := make([]string, 0, 3)
			if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims != nil {
				if claims.UserID != "" {
					subjects = append(subjects, claims.UserID)
				}
				if claims.Role != "" && claims.Role != claims.UserID {
					subjects = append(subjects, claims.Role)
				}
			}
			subjects = append(subjects, authz.BootstrapSubject)

			action := httpMethodToAction(r.Method)
			allowed := false
			for _, subject := range subjects {
				if enf.Can(subject, r.URL.Path, action) {
					allowed = true
					break
				}
			}
			if !allowed {
				// DX-2: the denial used to be mute — one `status=403` HTTP
				// line even at debug level, while the good log lived on the
				// Enforcer.Middleware path the framework does not mount.
				// Say who was denied what, and hand the operator the exact
				// CSV row that would allow it.
				if logger != nil {
					primary := subjects[0]
					logger.Info("authz denied",
						"subject", primary,
						"subjects_tried", strings.Join(subjects, ","),
						"resource", r.URL.Path,
						"action", action,
						"hint", fmt.Sprintf("add to rbac_policy.csv: p, %s, %s, %s, allow", primary, r.URL.Path, action),
					)
				}
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
