package authz

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
