package nucleus

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

type doc struct {
	ID      string
	OwnerID string
}

func contextFor(r *http.Request) *Context {
	return &Context{Context: &routerpkg.Context{Request: r, Writer: httptest.NewRecorder()}}
}

func TestContext_ClaimsAndRoles(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := contextFor(req)

	if _, ok := c.Claims(); ok {
		t.Error("an anonymous request reported claims")
	}
	if c.UserID() != "" || c.HasRole("editor") {
		t.Error("an anonymous request answered as an identity")
	}

	withClaims := req.WithContext(auth.ContextWithClaims(req.Context(), &auth.Claims{
		UserID: "u1", Username: "ana", Role: "editor", Roles: []string{"billing"},
	}))
	c = contextFor(withClaims)
	if c.UserID() != "u1" {
		t.Errorf("UserID is %q", c.UserID())
	}
	if !c.HasRole("editor") || !c.HasRole("BILLING") {
		t.Error("HasRole does not see every role")
	}
	if c.HasRole("admin") {
		t.Error("HasRole invented a role")
	}
}

// Every helper is closed when the middleware is not mounted. One that
// answered "allowed" because nothing was configured would be worse than no
// helper at all.
func TestContext_AuthorizationIsClosedWithoutMiddleware(t *testing.T) {
	c := contextFor(httptest.NewRequest(http.MethodGet, "/", nil))
	if c.Can("/posts", "read") {
		t.Error("Can allowed with no enforcer on the request")
	}
	if c.CanObject(doc{OwnerID: "u1"}, "edit") {
		t.Error("CanObject allowed with no enforcer on the request")
	}
}

func TestContext_CanUsesTheRoutePolicy(t *testing.T) {
	enforcer, err := authz.New(nil)
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}
	if err := enforcer.AddRole("u1", "editor"); err != nil {
		t.Fatalf("add role: %v", err)
	}
	if err := enforcer.AllowResource("editor", "/posts", "GET"); err != nil {
		t.Fatalf("allow: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := auth.ContextWithClaims(req.Context(), &auth.Claims{UserID: "u1"})
	ctx = authz.ContextWithEnforcer(ctx, enforcer)
	c := contextFor(req.WithContext(ctx))

	if !c.Can("/posts", "GET") {
		t.Error("a granted route was denied")
	}
	if c.Can("/admin", "GET") {
		t.Error("an ungranted route was allowed")
	}
}

func TestContext_CanObjectSeparatesRows(t *testing.T) {
	objects, err := authz.NewObjectEnforcer(nil)
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}
	if err := objects.AddRole("u1", "editor"); err != nil {
		t.Fatalf("add role: %v", err)
	}
	if err := objects.Allow("editor", "r.obj.OwnerID == r.sub", "edit"); err != nil {
		t.Fatalf("allow: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := auth.ContextWithClaims(req.Context(), &auth.Claims{UserID: "u1"})
	ctx = authz.ContextWithObjectEnforcer(ctx, objects)
	c := contextFor(req.WithContext(ctx))

	if !c.CanObject(doc{ID: "1", OwnerID: "u1"}, "edit") {
		t.Error("the owner cannot edit their own row")
	}
	if c.CanObject(doc{ID: "2", OwnerID: "u2"}, "edit") {
		t.Error("a row owned by somebody else was editable")
	}
}

// An anonymous caller has no subject to match a rule against, so object
// permission is denied rather than matched against the empty string.
func TestContext_CanObjectDeniesAnonymous(t *testing.T) {
	objects, _ := authz.NewObjectEnforcer(nil)
	_ = objects.AddRole("", "editor")
	_ = objects.Allow("editor", "r.obj.OwnerID == r.sub", "edit")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := contextFor(req.WithContext(authz.ContextWithObjectEnforcer(req.Context(), objects)))
	if c.CanObject(doc{OwnerID: ""}, "edit") {
		t.Fatal("an anonymous caller matched an ownership rule")
	}
}
