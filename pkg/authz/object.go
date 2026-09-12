package authz

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
)

// Object permissions answer the question route permissions cannot: not
// "may this subject PUT /posts/*", but "may this subject edit the posts
// SHE owns".
//
// Every application ends up needing it, and without it every application
// writes the same `if post.AuthorID != claims.UserID { 403 }` inside its
// handlers — where nothing audits it, nothing lists it, and one forgotten
// branch is an authorization bug nobody can see by reading the policy.
//
// The model here is Casbin's ABAC form: a policy row carries an EXPRESSION
// over the resource's attributes instead of a path, and the request carries
// the resource itself.
const objectModelText = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, rule, act, eft

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))

[matchers]
m = g(r.sub, p.sub) && eval(p.rule) && (r.act == p.act || p.act == "*")
`

// ObjectEnforcer decides permissions on resource INSTANCES.
//
// It is a separate enforcer from the route one rather than a wider model,
// because the two answer different questions with different inputs and a
// single model would have to accept a string where an object belongs. They
// share role assignments: AddRole here and on the route enforcer are the
// same grant on different surfaces, which is why both exist rather than one
// replacing the other.
type ObjectEnforcer struct {
	enforcer *casbin.Enforcer
	logger   *slog.Logger
}

// NewObjectEnforcer builds an enforcer for object rules.
func NewObjectEnforcer(logger *slog.Logger) (*ObjectEnforcer, error) {
	if logger == nil {
		logger = slog.Default()
	}
	m, err := model.NewModelFromString(objectModelText)
	if err != nil {
		return nil, fmt.Errorf("authz: object model: %w", err)
	}
	enforcer, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, fmt.Errorf("authz: object enforcer: %w", err)
	}
	return &ObjectEnforcer{enforcer: enforcer, logger: logger}, nil
}

// Allow grants a role an action on any resource matching rule — a Casbin
// expression over the request, where r.obj is the resource and r.sub the
// subject:
//
//	e.Allow("editor", "r.obj.AuthorID == r.sub", "edit")
//	e.Allow("auditor", "r.obj.TenantID == r.sub", "*")
//
// The expression is evaluated against the object as given, so its fields
// must be exported.
func (e *ObjectEnforcer) Allow(role, rule, action string) error {
	if strings.TrimSpace(rule) == "" {
		return fmt.Errorf("authz: an object rule cannot be empty (use \"true\" to match every resource)")
	}
	_, err := e.enforcer.AddPolicy(role, rule, action, "allow")
	return err
}

// Deny refuses a role an action on matching resources, and beats any
// grant — the same effect rule the route enforcer uses, so "deny wins"
// means one thing in this framework.
func (e *ObjectEnforcer) Deny(role, rule, action string) error {
	_, err := e.enforcer.AddPolicy(role, rule, action, "deny")
	return err
}

// AddRole assigns a subject to a role.
func (e *ObjectEnforcer) AddRole(subject, role string) error {
	_, err := e.enforcer.AddGroupingPolicy(subject, role)
	return err
}

// Can reports whether the subject may perform the action on this specific
// resource.
//
// An error from the policy engine is a DENIAL, logged. An authorization
// layer that answers "allow" when it could not decide is the failure mode
// this whole package exists to prevent.
func (e *ObjectEnforcer) Can(subject string, resource any, action string) bool {
	if e == nil || e.enforcer == nil {
		return false
	}
	allowed, err := e.enforcer.Enforce(subject, resource, action)
	if err != nil {
		e.logger.Error("authz: object policy could not be evaluated; denying",
			"subject", subject, "action", action, "error", err)
		return false
	}
	return allowed
}

// Policies returns the object rules, for an operator surface that lists
// them — the point of moving ownership out of handlers is that it becomes
// something you can read.
func (e *ObjectEnforcer) Policies() ([][]string, error) {
	return e.enforcer.GetPolicy()
}

// --- carrying the enforcers on a request ------------------------------------

type enforcerContextKey struct{}
type objectEnforcerContextKey struct{}

// ContextWithEnforcer puts the route enforcer on a context. Enforcer's
// middleware does it for every request it handles, so a handler can reach
// the policy without the application threading it through by hand.
func ContextWithEnforcer(ctx context.Context, e *Enforcer) context.Context {
	return context.WithValue(ctx, enforcerContextKey{}, e)
}

// EnforcerFromContext returns the route enforcer, if one is present.
func EnforcerFromContext(ctx context.Context) (*Enforcer, bool) {
	e, ok := ctx.Value(enforcerContextKey{}).(*Enforcer)
	return e, ok && e != nil
}

// ContextWithObjectEnforcer puts the object enforcer on a context.
func ContextWithObjectEnforcer(ctx context.Context, e *ObjectEnforcer) context.Context {
	return context.WithValue(ctx, objectEnforcerContextKey{}, e)
}

// ObjectEnforcerFromContext returns the object enforcer, if one is present.
func ObjectEnforcerFromContext(ctx context.Context) (*ObjectEnforcer, bool) {
	e, ok := ctx.Value(objectEnforcerContextKey{}).(*ObjectEnforcer)
	return e, ok && e != nil
}

// ObjectMiddleware carries an object enforcer on every request, so handlers
// can ask about resource instances.
func ObjectMiddleware(e *ObjectEnforcer) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(ContextWithObjectEnforcer(r.Context(), e)))
		})
	}
}
