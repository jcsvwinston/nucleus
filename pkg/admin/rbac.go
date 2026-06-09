package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// rbacEnforcer wraps the Casbin enforcer for admin authorization.
type rbacEnforcer struct {
	enforcer *authz.Enforcer
}

// newRBACAuth creates an AdminAuth provider with Casbin-based RBAC.
func newRBACAuth(enforcer *authz.Enforcer, fallback AdminAuth) AdminAuth {
	return &rbacEnforcer{enforcer: enforcer}
}

// Authenticate delegates to the fallback auth provider.
func (r *rbacEnforcer) Authenticate(req *http.Request) (*auth.User, error) {
	// RBAC enforcer doesn't handle authentication, delegate to fallback
	return nil, fmt.Errorf("rbac auth requires fallback provider")
}

// Authorize checks Casbin policies for the given user, model, and action.
func (r *rbacEnforcer) Authorize(user *auth.User, model string, action string) bool {
	if user == nil {
		return false
	}

	// Superusers bypass all policy checks
	if user.IsSuperuser {
		return true
	}

	// Build resource path: admin:<model>
	resource := "admin:" + model
	if resource == "admin:*" {
		resource = "admin:*"
	}

	// Check Casbin policy
	return r.enforcer.Can(user.ID, resource, action) ||
		r.enforcer.Can(user.Role, resource, action) ||
		r.enforcer.Can(user.Username, resource, action)
}

// LoginHandler is not supported on rbacEnforcer directly.
// Use the combined auth provider.
func (r *rbacEnforcer) LoginHandler() http.Handler {
	return http.NotFoundHandler()
}

// combinedAdminAuth combines database auth with RBAC authorization.
type combinedAdminAuth struct {
	auth     AdminAuth // For authentication (login)
	enforcer *authz.Enforcer
	session  *auth.SessionManager
	prefix   string
}

// newCombinedAdminAuth creates auth with Casbin RBAC authorization.
func newCombinedAdminAuth(authProvider AdminAuth, enforcer *authz.Enforcer) AdminAuth {
	if ca, ok := authProvider.(*combinedAdminAuth); ok {
		return &combinedAdminAuth{
			auth:     ca.auth,
			enforcer: enforcer,
			session:  ca.session,
			prefix:   ca.prefix,
		}
	}
	if dba, ok := authProvider.(*DatabaseAdminAuth); ok {
		return &combinedAdminAuth{
			auth:     authProvider,
			enforcer: enforcer,
			session:  dba.session,
			prefix:   dba.prefix,
		}
	}
	return &combinedAdminAuth{
		auth:     authProvider,
		enforcer: enforcer,
	}
}

func (c *combinedAdminAuth) Authenticate(r *http.Request) (*auth.User, error) {
	return c.auth.Authenticate(r)
}

func (c *combinedAdminAuth) Authorize(user *auth.User, model string, action string) bool {
	if user == nil {
		return false
	}

	// Superusers bypass all policy checks
	if user.IsSuperuser {
		return true
	}

	// Build resource path: admin:<model>
	resource := "admin:" + model

	// Check Casbin policy for user ID, role, and username
	if c.enforcer != nil {
		if c.enforcer.Can(user.ID, resource, action) {
			return true
		}
		if c.enforcer.Can(user.Role, resource, action) {
			return true
		}
		if c.enforcer.Can(user.Username, resource, action) {
			return true
		}
	}

	return false
}

func (c *combinedAdminAuth) LoginHandler() http.Handler {
	return c.auth.LoginHandler()
}

// RBAC API Handlers

func (p *Panel) handleListRBACPolicies(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "rbac_list"); err != nil {
		return err
	}

	if p.rbac == nil {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"enabled":  false,
			"reason":   "RBAC enforcer not configured",
			"policies": []interface{}{},
			"roles":    []interface{}{},
		})
	}

	policies, _ := p.rbac.GetPolicy()
	rolePolicies, _ := p.rbac.GetGroupingPolicy()

	// Format policies for response. The eft column (allow|deny) is
	// included so an operator can distinguish an allow rule from a deny
	// rule for the same (sub, obj, act) in the RBAC inspector — without
	// it, a deny is indistinguishable from an allow in the UI even though
	// the model enforces deny-override correctly. See pkg/authz
	// effectAllow / effectDeny.
	formattedPolicies := make([]map[string]string, 0, len(policies))
	for _, pol := range policies {
		if len(pol) >= 3 {
			formatted := map[string]string{
				"sub": pol[0],
				"obj": pol[1],
				"act": pol[2],
			}
			if len(pol) >= 4 {
				formatted["eft"] = pol[3]
			}
			formattedPolicies = append(formattedPolicies, formatted)
		}
	}

	// Format roles
	formattedRoles := make([]map[string]string, 0, len(rolePolicies))
	for _, pol := range rolePolicies {
		if len(pol) >= 2 {
			formattedRoles = append(formattedRoles, map[string]string{
				"user": pol[0],
				"role": pol[1],
			})
		}
	}

	// Get all unique subjects and roles
	subjects := map[string]bool{}
	roles := map[string]bool{}
	for _, pol := range policies {
		if len(pol) >= 3 {
			subjects[pol[0]] = true
			roles[pol[1]] = true
		}
	}

	subjectList := make([]string, 0, len(subjects))
	for s := range subjects {
		subjectList = append(subjectList, s)
	}
	sort.Strings(subjectList)

	roleList := make([]string, 0, len(roles))
	for r := range roles {
		roleList = append(roleList, r)
	}
	sort.Strings(roleList)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"enabled":   true,
		"policies":  formattedPolicies,
		"roles":     formattedRoles,
		"subjects":  subjectList,
		"role_list": roleList,
	})
}

func (p *Panel) handleAddRBACPolicy(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "rbac_manage"); err != nil {
		return err
	}

	if p.rbac == nil {
		return gferrors.BadRequest("RBAC enforcer not configured")
	}

	var req struct {
		Sub string `json:"sub"`
		Obj string `json:"obj"`
		Act string `json:"act"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	if req.Sub == "" || req.Obj == "" || req.Act == "" {
		return gferrors.BadRequest("sub, obj, and act are required")
	}

	if err := p.rbac.AddPolicy(req.Sub, req.Obj, req.Act); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"added": true,
		"sub":   req.Sub,
		"obj":   req.Obj,
		"act":   req.Act,
	})
}

func (p *Panel) handleRemoveRBACPolicy(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "rbac_manage"); err != nil {
		return err
	}

	if p.rbac == nil {
		return gferrors.BadRequest("RBAC enforcer not configured")
	}

	var req struct {
		Sub string `json:"sub"`
		Obj string `json:"obj"`
		Act string `json:"act"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	if req.Sub == "" || req.Obj == "" || req.Act == "" {
		return gferrors.BadRequest("sub, obj, and act are required")
	}

	if err := p.rbac.RemovePolicy(req.Sub, req.Obj, req.Act); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"removed": true,
		"sub":     req.Sub,
		"obj":     req.Obj,
		"act":     req.Act,
	})
}

func (p *Panel) handleAssignRBACRole(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "rbac_manage"); err != nil {
		return err
	}

	if p.rbac == nil {
		return gferrors.BadRequest("RBAC enforcer not configured")
	}

	var req struct {
		User string `json:"user"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	if req.User == "" || req.Role == "" {
		return gferrors.BadRequest("user and role are required")
	}

	if err := p.rbac.AddRole(req.User, req.Role); err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"assigned": true,
		"user":     req.User,
		"role":     req.Role,
	})
}

func (p *Panel) handleRemoveRBACRole(c *router.Context) error {
	r := c.Request
	if err := p.authorizeAction(c, "*", "rbac_manage"); err != nil {
		return err
	}

	if p.rbac == nil {
		return gferrors.BadRequest("RBAC enforcer not configured")
	}

	var req struct {
		User string `json:"user"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return gferrors.BadRequest("invalid JSON")
	}

	if req.User == "" || req.Role == "" {
		return gferrors.BadRequest("user and role are required")
	}

	if err := p.rbac.RemoveRole(req.User, req.Role); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"removed": true,
		"user":    req.User,
		"role":    req.Role,
	})
}

func (p *Panel) handleGetRBACRoles(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "rbac_list"); err != nil {
		return err
	}

	if p.rbac == nil {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"enabled": false,
			"roles":   []interface{}{},
		})
	}

	user := c.Query("user")
	if user == "" {
		// Return all roles
		roles, _ := p.rbac.GetAllRoles()
		return c.JSON(http.StatusOK, map[string]interface{}{
			"enabled": true,
			"roles":   roles,
		})
	}

	// Return roles for specific user
	userRoles := p.rbac.GetRoles(user)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"enabled": true,
		"user":    user,
		"roles":   userRoles,
	})
}

func (p *Panel) handleCheckRBACPermission(c *router.Context) error {
	if err := p.authorizeAction(c, "*", "rbac_list"); err != nil {
		return err
	}

	if p.rbac == nil {
		return gferrors.BadRequest("RBAC enforcer not configured")
	}

	sub := c.Query("sub")
	obj := c.Query("obj")
	act := c.Query("act")

	if sub == "" || obj == "" || act == "" {
		return gferrors.BadRequest("sub, obj, and act query parameters are required")
	}

	allowed := p.rbac.Can(sub, obj, act)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"sub":     sub,
		"obj":     obj,
		"act":     act,
		"allowed": allowed,
	})
}
