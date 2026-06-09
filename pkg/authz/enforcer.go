// Package authz provides authorization for the Nucleus framework using Casbin.
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
//
// Default-deny with deny-override semantics:
//
//   - No policy matches  → request is denied (the absence of an `allow`
//     match is treated as deny by `some(where (p.eft == allow))`).
//   - Allow + deny match  → request is denied. An explicit deny rule
//     overrides every matching allow rule, so operators can write
//     "this user is blocked even though their role normally has access"
//     policies without having to revoke the role's broader allow.
//
// Policies carry an `eft` field. AddPolicy auto-stamps `allow`; Deny
// auto-stamps `deny`. The programmatic API has not changed shape, but
// CSV policy files loaded via fileadapter MUST now have four columns
// per row (sub, obj, act, eft) — the loader treats a missing eft as
// empty which neither matches `allow` nor `deny`, making the policy
// inert. See MigrateCSVPolicyFile for a one-shot migration helper.
const defaultRBACModel = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act, eft

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))

[matchers]
m = g(r.sub, p.sub) && keyMatch(r.obj, p.obj) && (r.act == p.act || p.act == "*")
`

// Policy effect values stamped into the model's eft column.
const (
	effectAllow = "allow"
	effectDeny  = "deny"
)

// Enforcer wraps a Casbin enforcer with logging and convenience methods.
//
// The underlying *casbin.Enforcer is held in an unexported field rather
// than embedded so that Casbin's concrete type and its full method set do
// not leak onto this stable public surface (ADR-015, F-4). Every Casbin
// capability Nucleus exposes is forwarded by an explicit method below.
type Enforcer struct {
	enforcer *casbin.Enforcer
	logger   *slog.Logger
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

	return &Enforcer{enforcer: e, logger: logger}, nil
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

	return &Enforcer{enforcer: e, logger: logger}, nil
}

// Can checks if the subject is allowed to perform the action on the object.
func (e *Enforcer) Can(sub, obj, act string) bool {
	ok, err := e.enforcer.Enforce(sub, obj, act)
	if err != nil {
		e.logger.Error("authz.Can enforce error", "sub", sub, "obj", obj, "act", act, "error", err.Error())
		return false
	}
	return ok
}

// AddPolicy adds an allow policy (subject, object, action). The eft
// column is auto-stamped to `allow`. To add a deny rule, use Deny.
func (e *Enforcer) AddPolicy(sub, obj, act string) error {
	_, err := e.enforcer.AddPolicy(sub, obj, act, effectAllow)
	if err != nil {
		return fmt.Errorf("authz.AddPolicy: %w", err)
	}
	return nil
}

// Deny adds an explicit deny policy (subject, object, action). Deny
// rules override every matching allow rule under the model's
// deny-override effect formula, so this is the primitive for "block
// this user even though their role normally has access".
func (e *Enforcer) Deny(sub, obj, act string) error {
	_, err := e.enforcer.AddPolicy(sub, obj, act, effectDeny)
	if err != nil {
		return fmt.Errorf("authz.Deny: %w", err)
	}
	return nil
}

// RemovePolicy removes a permission policy regardless of its eft. Both
// allow and deny variants matching (sub, obj, act) are dropped, which
// matches operator intent — "stop applying this rule" should not
// require knowing how the rule was originally written.
func (e *Enforcer) RemovePolicy(sub, obj, act string) error {
	removedAllow, errAllow := e.enforcer.RemovePolicy(sub, obj, act, effectAllow)
	removedDeny, errDeny := e.enforcer.RemovePolicy(sub, obj, act, effectDeny)
	if errAllow != nil {
		return fmt.Errorf("authz.RemovePolicy (allow): %w", errAllow)
	}
	if errDeny != nil {
		return fmt.Errorf("authz.RemovePolicy (deny): %w", errDeny)
	}
	if !removedAllow && !removedDeny {
		// Casbin doesn't treat "nothing to remove" as an error. Mirror
		// that semantics rather than surfacing a fresh one.
		return nil
	}
	return nil
}

// AddRole assigns a role to a user (e.g. AddRole("alice", "admin")).
func (e *Enforcer) AddRole(user, role string) error {
	_, err := e.enforcer.AddGroupingPolicy(user, role)
	if err != nil {
		return fmt.Errorf("authz.AddRole: %w", err)
	}
	return nil
}

// RemoveRole removes a role assignment from a user.
func (e *Enforcer) RemoveRole(user, role string) error {
	_, err := e.enforcer.RemoveGroupingPolicy(user, role)
	if err != nil {
		return fmt.Errorf("authz.RemoveRole: %w", err)
	}
	return nil
}

// GetRoles returns all roles assigned to a user.
func (e *Enforcer) GetRoles(user string) []string {
	roles, err := e.enforcer.GetRolesForUser(user)
	if err != nil {
		e.logger.Error("authz.GetRoles error", "user", user, "error", err.Error())
		return nil
	}
	return roles
}

// GetPolicy returns all permission policy rules as (subject, object,
// action, eft) string tuples. It forwards Casbin's policy store without
// exposing any Casbin type, so callers (e.g. the admin RBAC inspector)
// can read the live ruleset.
func (e *Enforcer) GetPolicy() ([][]string, error) {
	return e.enforcer.GetPolicy()
}

// GetGroupingPolicy returns all role-assignment (grouping) rules as
// (user, role) string tuples.
func (e *Enforcer) GetGroupingPolicy() ([][]string, error) {
	return e.enforcer.GetGroupingPolicy()
}

// GetAllRoles returns every role referenced by a grouping policy.
func (e *Enforcer) GetAllRoles() ([]string, error) {
	return e.enforcer.GetAllRoles()
}
