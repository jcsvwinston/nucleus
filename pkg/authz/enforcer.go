// Package authz provides authorization for the GoFrame framework using Casbin.
// It wraps Casbin's enforcer with sensible RBAC defaults and provides Chi
// middleware for route-level access control.
package authz

import (
	"fmt"
	"log/slog"

	"github.com/casbin/casbin/v2"
	casbinmodel "github.com/casbin/casbin/v2/model"
	fileadapter "github.com/casbin/casbin/v2/persist/file-adapter"
)

// Default RBAC model configuration.
const defaultRBACModel = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && keyMatch(r.obj, p.obj) && (r.act == p.act || p.act == "*")
`

// Enforcer wraps a Casbin enforcer with logging and convenience methods.
type Enforcer struct {
	*casbin.Enforcer
	logger *slog.Logger
}

// New creates an Enforcer with the default RBAC model.
// If policyPath is provided, policies are loaded from that CSV file.
// If policyPath is empty, no policies are loaded (add them programmatically).
func New(logger *slog.Logger, policyPath ...string) (*Enforcer, error) {
	m, err := casbinmodel.NewModelFromString(defaultRBACModel)
	if err != nil {
		return nil, fmt.Errorf("authz.New model: %w", err)
	}

	var e *casbin.Enforcer
	if len(policyPath) > 0 && policyPath[0] != "" {
		adapter := fileadapter.NewAdapter(policyPath[0])
		e, err = casbin.NewEnforcer(m, adapter)
	} else {
		e, err = casbin.NewEnforcer(m)
	}
	if err != nil {
		return nil, fmt.Errorf("authz.New enforcer: %w", err)
	}

	return &Enforcer{Enforcer: e, logger: logger}, nil
}

// NewFromModel creates an Enforcer with a custom Casbin model string.
func NewFromModel(modelStr string, logger *slog.Logger, policyPath ...string) (*Enforcer, error) {
	m, err := casbinmodel.NewModelFromString(modelStr)
	if err != nil {
		return nil, fmt.Errorf("authz.NewFromModel: %w", err)
	}

	var e *casbin.Enforcer
	if len(policyPath) > 0 && policyPath[0] != "" {
		adapter := fileadapter.NewAdapter(policyPath[0])
		e, err = casbin.NewEnforcer(m, adapter)
	} else {
		e, err = casbin.NewEnforcer(m)
	}
	if err != nil {
		return nil, fmt.Errorf("authz.NewFromModel enforcer: %w", err)
	}

	return &Enforcer{Enforcer: e, logger: logger}, nil
}

// Can checks if the subject is allowed to perform the action on the object.
func (e *Enforcer) Can(sub, obj, act string) bool {
	ok, err := e.Enforce(sub, obj, act)
	if err != nil {
		e.logger.Error("authz.Can enforce error", "sub", sub, "obj", obj, "act", act, "error", err.Error())
		return false
	}
	return ok
}

// AddPolicy adds a permission policy (subject, object, action).
func (e *Enforcer) AddPolicy(sub, obj, act string) error {
	_, err := e.Enforcer.AddPolicy(sub, obj, act)
	if err != nil {
		return fmt.Errorf("authz.AddPolicy: %w", err)
	}
	return nil
}

// RemovePolicy removes a permission policy.
func (e *Enforcer) RemovePolicy(sub, obj, act string) error {
	_, err := e.Enforcer.RemovePolicy(sub, obj, act)
	if err != nil {
		return fmt.Errorf("authz.RemovePolicy: %w", err)
	}
	return nil
}

// AddRole assigns a role to a user (e.g. AddRole("alice", "admin")).
func (e *Enforcer) AddRole(user, role string) error {
	_, err := e.AddGroupingPolicy(user, role)
	if err != nil {
		return fmt.Errorf("authz.AddRole: %w", err)
	}
	return nil
}

// RemoveRole removes a role assignment from a user.
func (e *Enforcer) RemoveRole(user, role string) error {
	_, err := e.RemoveGroupingPolicy(user, role)
	if err != nil {
		return fmt.Errorf("authz.RemoveRole: %w", err)
	}
	return nil
}

// GetRoles returns all roles assigned to a user.
func (e *Enforcer) GetRoles(user string) []string {
	roles, err := e.GetRolesForUser(user)
	if err != nil {
		e.logger.Error("authz.GetRoles error", "user", user, "error", err.Error())
		return nil
	}
	return roles
}
