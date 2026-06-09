package authz

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

func newTestEnforcer(t *testing.T) *Enforcer {
	t.Helper()
	e, err := New(slog.Default())
	if err != nil {
		t.Fatalf("failed to create enforcer: %v", err)
	}
	return e
}

func TestEnforcer_Can(t *testing.T) {
	e := newTestEnforcer(t)

	e.AddPolicy("alice", "/api/users/*", "read")
	e.AddPolicy("alice", "/api/users/*", "create")

	if !e.Can("alice", "/api/users/1", "read") {
		t.Error("alice should be able to read /api/users/1")
	}
	if !e.Can("alice", "/api/users/1", "create") {
		t.Error("alice should be able to create")
	}
	if e.Can("alice", "/api/users/1", "delete") {
		t.Error("alice should NOT be able to delete")
	}
	if e.Can("bob", "/api/users/1", "read") {
		t.Error("bob should NOT have any permissions")
	}
}

func TestEnforcer_Roles(t *testing.T) {
	e := newTestEnforcer(t)

	e.AddPolicy("admin", "/api/*", "*")
	e.AddRole("alice", "admin")

	if !e.Can("alice", "/api/users/1", "delete") {
		t.Error("alice (admin role) should be able to delete")
	}

	roles := e.GetRoles("alice")
	if len(roles) != 1 || roles[0] != "admin" {
		t.Errorf("expected [admin], got %v", roles)
	}
}

// TestEnforcer_PolicyForwarders exercises the Casbin-free read forwarders
// (GetPolicy / GetGroupingPolicy / GetAllRoles) added in ADR-015 §2 when
// authz.Enforcer stopped embedding *casbin.Enforcer. They are stable
// surface consumed by the admin RBAC inspector, so pin their behaviour.
func TestEnforcer_PolicyForwarders(t *testing.T) {
	e := newTestEnforcer(t)

	if err := e.AddPolicy("admin", "/api/*", "read"); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	if err := e.AddRole("alice", "admin"); err != nil {
		t.Fatalf("AddRole: %v", err)
	}

	// GetPolicy returns the allow rule with its eft column stamped.
	policies, err := e.GetPolicy()
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	found := false
	for _, p := range policies {
		if len(p) >= 4 && p[0] == "admin" && p[1] == "/api/*" && p[2] == "read" && p[3] == effectAllow {
			found = true
		}
	}
	if !found {
		t.Errorf("GetPolicy missing (admin,/api/*,read,allow); got %v", policies)
	}

	// GetGroupingPolicy returns the role assignment.
	groups, err := e.GetGroupingPolicy()
	if err != nil {
		t.Fatalf("GetGroupingPolicy: %v", err)
	}
	if len(groups) != 1 || groups[0][0] != "alice" || groups[0][1] != "admin" {
		t.Errorf("expected [[alice admin]], got %v", groups)
	}

	// GetAllRoles returns every role referenced by a grouping policy.
	allRoles, err := e.GetAllRoles()
	if err != nil {
		t.Fatalf("GetAllRoles: %v", err)
	}
	if len(allRoles) != 1 || allRoles[0] != "admin" {
		t.Errorf("expected [admin], got %v", allRoles)
	}
}

func TestEnforcer_RemovePolicy(t *testing.T) {
	e := newTestEnforcer(t)
	e.AddPolicy("alice", "/api/*", "read")
	e.RemovePolicy("alice", "/api/*", "read")

	if e.Can("alice", "/api/users", "read") {
		t.Error("alice should not have access after policy removal")
	}
}

func TestEnforcer_RemoveRole(t *testing.T) {
	e := newTestEnforcer(t)
	e.AddPolicy("admin", "/api/*", "*")
	e.AddRole("alice", "admin")
	e.RemoveRole("alice", "admin")

	if e.Can("alice", "/api/users", "read") {
		t.Error("alice should not have access after role removal")
	}
}

func TestMiddleware_Authorized(t *testing.T) {
	e := newTestEnforcer(t)
	e.AddPolicy("user-1", "/protected", "read")

	jwtMgr := auth.NewJWTManager("test-secret-key-for-authz-test!!", time.Hour)
	token, _ := jwtMgr.Generate("user-1", "alice", "admin")

	// Chain: JWT middleware -> Authz middleware -> handler
	handler := jwtMgr.Middleware()(
		e.Middleware()(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
			}),
		),
	)

	r := httptest.NewRequest(http.MethodGet, "/protected", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestMiddleware_Forbidden(t *testing.T) {
	e := newTestEnforcer(t)
	// No policy for user-1 on /protected

	jwtMgr := auth.NewJWTManager("test-secret-key-for-authz-test!!", time.Hour)
	token, _ := jwtMgr.Generate("user-1", "alice", "admin")

	handler := jwtMgr.Middleware()(
		e.Middleware()(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler should not be called")
			}),
		),
	)

	r := httptest.NewRequest(http.MethodGet, "/protected", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 403 {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestMiddleware_Unauthorized(t *testing.T) {
	e := newTestEnforcer(t)

	handler := e.Middleware()(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called")
		}),
	)

	r := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireRole_Allowed(t *testing.T) {
	e := newTestEnforcer(t)
	jwtMgr := auth.NewJWTManager("test-secret-key-for-authz-test!!", time.Hour)
	token, _ := jwtMgr.Generate("user-1", "alice", "admin")

	handler := jwtMgr.Middleware()(
		e.RequireRole("admin")(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
			}),
		),
	)

	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Errorf("expected 200 for admin role, got %d", w.Code)
	}
}

func TestRequireRole_Forbidden(t *testing.T) {
	e := newTestEnforcer(t)
	jwtMgr := auth.NewJWTManager("test-secret-key-for-authz-test!!", time.Hour)
	token, _ := jwtMgr.Generate("user-1", "alice", "user")

	handler := jwtMgr.Middleware()(
		e.RequireRole("admin")(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler should not be called")
			}),
		),
	)

	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 403 {
		t.Errorf("expected 403 for wrong role, got %d", w.Code)
	}
}

func TestRequireRole_CasbinRole(t *testing.T) {
	e := newTestEnforcer(t)
	e.AddRole("user-1", "superadmin")

	jwtMgr := auth.NewJWTManager("test-secret-key-for-authz-test!!", time.Hour)
	token, _ := jwtMgr.Generate("user-1", "alice", "user") // JWT role is "user"

	handler := jwtMgr.Middleware()(
		e.RequireRole("superadmin")( // But Casbin role is "superadmin"
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
			}),
		),
	)

	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Errorf("expected 200 via Casbin role, got %d", w.Code)
	}
}

func TestSetupAdminPolicies(t *testing.T) {
	e := newTestEnforcer(t)
	err := e.SetupAdminPolicies("/admin", "users", "products")
	if err != nil {
		t.Fatal(err)
	}

	e.AddRole("alice", "admin")
	if !e.Can("alice", "/admin/api/models/users/1", "delete") {
		t.Error("admin should have full access to users")
	}

	e.AddRole("bob", "viewer")
	if !e.Can("bob", "/admin/api/models/products/1", "read") {
		t.Error("viewer should have read access")
	}
	if e.Can("bob", "/admin/api/models/products/1", "delete") {
		t.Error("viewer should NOT have delete access")
	}
}

func TestAllowAll(t *testing.T) {
	e := newTestEnforcer(t)
	err := e.AllowAll("admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !e.Can("admin", "/any/resource", "any-action") {
		t.Error("admin should have full access after AllowAll")
	}
}

func TestAllowResource(t *testing.T) {
	e := newTestEnforcer(t)
	err := e.AllowResource("editor", "/api/posts/*", "read", "create", "update")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !e.Can("editor", "/api/posts/1", "read") {
		t.Error("editor should have read access")
	}
	if !e.Can("editor", "/api/posts/1", "create") {
		t.Error("editor should have create access")
	}
	if !e.Can("editor", "/api/posts/1", "update") {
		t.Error("editor should have update access")
	}
	if e.Can("editor", "/api/posts/1", "delete") {
		t.Error("editor should NOT have delete access")
	}
}

func TestHttpMethodToAction(t *testing.T) {
	tests := map[string]string{
		"GET": "read", "HEAD": "read", "POST": "create",
		"PUT": "update", "PATCH": "update", "DELETE": "delete",
	}
	for method, expected := range tests {
		if got := httpMethodToAction(method); got != expected {
			t.Errorf("%s: expected %s, got %s", method, expected, got)
		}
	}
}

// Ensure we don't use the context import
var _ = context.Background
