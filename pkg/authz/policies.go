package authz

// BootstrapSubject is the subject used by SeedBootstrapAllowList and by
// the default-deny middleware in pkg/app when no JWT claims are present
// on a request. Operators can grant or deny access for unauthenticated
// callers by writing policies against this subject; framework-owned
// routes registered by SeedBootstrapAllowList are the canonical
// example.
const BootstrapSubject = "anonymous"

// BootstrapAllowList returns the routes the framework registers under
// BootstrapSubject before any user policy file loads. These are paths
// the framework itself owns and that must respond without
// authorization — Kubernetes probes, Prometheus scrapes, the login
// flow, and the static assets that the runtime mounts. Operators
// cannot override this list via config; removing an entry requires a
// code change.
//
// Returned as ((object, action) tuples); the subject is implicit
// (BootstrapSubject) and the action is "*" because these routes serve
// every HTTP method that the underlying handler accepts.
func BootstrapAllowList() []struct{ Object, Action string } {
	return []struct{ Object, Action string }{
		{Object: "/healthz", Action: "*"},
		{Object: "/metrics", Action: "*"},
		{Object: "/login", Action: "*"},
		{Object: "/.well-known/jwks.json", Action: "*"},
		{Object: "/static/*", Action: "*"},
	}
}

// SeedBootstrapAllowList programmatically adds the BootstrapAllowList
// entries to the enforcer under BootstrapSubject. pkg/app calls this
// during App.New (before mounting the default authz middleware) so
// the framework's own probe / login routes respond without
// authorization regardless of whether the operator has loaded a user
// policy file.
func (e *Enforcer) SeedBootstrapAllowList() error {
	for _, rule := range BootstrapAllowList() {
		if err := e.AddPolicy(BootstrapSubject, rule.Object, rule.Action); err != nil {
			return err
		}
	}
	return nil
}

// SetupAdminPolicies configures common admin policies for a set of model names.
// It creates policies for the "admin" role to have full CRUD access to all models
// and the "viewer" role to have read-only access.
func (e *Enforcer) SetupAdminPolicies(prefix string, modelNames ...string) error {
	for _, name := range modelNames {
		resource := prefix + "/api/models/" + name + "/*"

		// Admin gets full access
		if err := e.AddPolicy("admin", resource, "*"); err != nil {
			return err
		}

		// Viewer gets read-only
		if err := e.AddPolicy("viewer", resource, "read"); err != nil {
			return err
		}
	}

	// Admin can access the dashboard and model list
	if err := e.AddPolicy("admin", prefix+"/*", "read"); err != nil {
		return err
	}
	if err := e.AddPolicy("viewer", prefix+"/*", "read"); err != nil {
		return err
	}

	return nil
}

// AllowAll grants the given role full access to all resources.
func (e *Enforcer) AllowAll(role string) error {
	return e.AddPolicy(role, "/*", "*")
}

// AllowResource grants a role access to a specific resource pattern with actions.
func (e *Enforcer) AllowResource(role, resource string, actions ...string) error {
	for _, act := range actions {
		if err := e.AddPolicy(role, resource, act); err != nil {
			return err
		}
	}
	return nil
}
