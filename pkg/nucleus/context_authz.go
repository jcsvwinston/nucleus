package nucleus

import (
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
)

// The authorization a handler can reach for.
//
// Before these, a handler that needed the caller's identity dug it out of
// the request context by hand, and one that needed a decision about a
// specific row wrote the comparison inline — which is where an
// authorization rule stops being reviewable. The helpers are deliberately
// thin: they read what the middleware already put on the request, and every
// one of them is CLOSED when the middleware is not mounted. A helper that
// answered "allowed" because nothing was configured would be worse than no
// helper at all.

// Claims returns the identity of the caller, and whether there is one.
func (c *Context) Claims() (*auth.Claims, bool) {
	if c == nil || c.Context == nil || c.Context.Request == nil {
		return nil, false
	}
	return auth.ClaimsFromContext(c.Context.Request.Context())
}

// UserID returns the caller's id, or empty when the request carries no
// identity.
func (c *Context) UserID() string {
	claims, ok := c.Claims()
	if !ok || claims == nil {
		return ""
	}
	return claims.UserID
}

// HasRole reports whether the caller holds a role, primary or otherwise.
func (c *Context) HasRole(role string) bool {
	claims, ok := c.Claims()
	if !ok {
		return false
	}
	return claims.HasRole(role)
}

// Can reports whether the caller may perform an action on a PATH, using the
// route policy the authorization middleware carries.
//
// It answers false when no enforcer is on the request — a handler mounted
// outside the authorization middleware gets a denial, not an accidental
// grant.
func (c *Context) Can(path, action string) bool {
	if c == nil || c.Context == nil || c.Context.Request == nil {
		return false
	}
	enforcer, ok := authz.EnforcerFromContext(c.Context.Request.Context())
	if !ok {
		return false
	}
	subject := c.UserID()
	if subject == "" {
		subject = authz.BootstrapSubject
	}
	return enforcer.Can(subject, path, action)
}

// CanObject reports whether the caller may perform an action on a specific
// RESOURCE — "may she edit THIS post", which a path cannot express.
//
//	post, err := repo.Find(id)
//	if !c.CanObject(post, "edit") {
//	    return c.JSON(http.StatusForbidden, ...)
//	}
//
// It answers false when no object enforcer is on the request.
func (c *Context) CanObject(resource any, action string) bool {
	if c == nil || c.Context == nil || c.Context.Request == nil {
		return false
	}
	enforcer, ok := authz.ObjectEnforcerFromContext(c.Context.Request.Context())
	if !ok {
		return false
	}
	subject := c.UserID()
	if subject == "" {
		return false
	}
	return enforcer.Can(subject, resource, action)
}
