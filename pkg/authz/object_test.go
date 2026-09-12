package authz

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type post struct {
	ID       string
	AuthorID string
	TenantID string
}

// The question route permissions cannot answer.
func TestObjectEnforcer_OwnershipSeparatesTwoRowsOfTheSameResource(t *testing.T) {
	e, err := NewObjectEnforcer(nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := e.AddRole("ana", "editor"); err != nil {
		t.Fatalf("add role: %v", err)
	}
	if err := e.Allow("editor", "r.obj.AuthorID == r.sub", "edit"); err != nil {
		t.Fatalf("allow: %v", err)
	}

	hers := post{ID: "1", AuthorID: "ana"}
	somebodyElses := post{ID: "2", AuthorID: "beto"}

	if !e.Can("ana", hers, "edit") {
		t.Error("ana cannot edit her own post")
	}
	if e.Can("ana", somebodyElses, "edit") {
		t.Error("ana can edit somebody else's post")
	}
	if e.Can("ana", hers, "delete") {
		t.Error("a grant for edit also allowed delete")
	}
}

// "deny wins" means the same thing here as on routes.
func TestObjectEnforcer_DenyBeatsAGrant(t *testing.T) {
	e, _ := NewObjectEnforcer(nil)
	if err := e.AddRole("ana", "editor"); err != nil {
		t.Fatalf("add role: %v", err)
	}
	if err := e.Allow("editor", "true", "edit"); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if err := e.Deny("editor", `r.obj.TenantID == "locked"`, "edit"); err != nil {
		t.Fatalf("deny: %v", err)
	}

	if !e.Can("ana", post{TenantID: "open"}, "edit") {
		t.Error("the grant did not apply")
	}
	if e.Can("ana", post{TenantID: "locked"}, "edit") {
		t.Error("the deny did not beat the grant")
	}
}

// A wildcard action grants every verb, matching the route enforcer.
func TestObjectEnforcer_WildcardAction(t *testing.T) {
	e, _ := NewObjectEnforcer(nil)
	_ = e.AddRole("auditor", "auditors")
	if err := e.Allow("auditors", `r.obj.TenantID == "acme"`, "*"); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if !e.Can("auditor", post{TenantID: "acme"}, "read") {
		t.Error("the wildcard did not cover read")
	}
	if e.Can("auditor", post{TenantID: "other"}, "read") {
		t.Error("the wildcard ignored the rule")
	}
}

// A rule the engine cannot evaluate must DENY. An authorization layer that
// answers "allow" when it could not decide is the failure this package
// exists to prevent.
func TestObjectEnforcer_AnUnevaluableRuleDenies(t *testing.T) {
	e, _ := NewObjectEnforcer(nil)
	_ = e.AddRole("ana", "editor")
	if err := e.Allow("editor", "r.obj.NoSuchField == r.sub", "edit"); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if e.Can("ana", post{AuthorID: "ana"}, "edit") {
		t.Fatal("a rule referring to a field that does not exist allowed the action")
	}
}

func TestObjectEnforcer_EmptyRuleIsRefused(t *testing.T) {
	e, _ := NewObjectEnforcer(nil)
	if err := e.Allow("editor", "   ", "edit"); err == nil {
		t.Fatal("an empty rule was accepted")
	}
}

func TestObjectEnforcer_NilIsClosed(t *testing.T) {
	var e *ObjectEnforcer
	if e.Can("ana", post{}, "edit") {
		t.Fatal("a nil enforcer allowed an action")
	}
}

func TestObjectMiddleware_CarriesTheEnforcer(t *testing.T) {
	e, _ := NewObjectEnforcer(nil)
	var found bool
	h := ObjectMiddleware(e)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, found = ObjectEnforcerFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !found {
		t.Fatal("the handler could not reach the object enforcer")
	}
}

func TestEnforcerFromContext_AbsentIsNotPresent(t *testing.T) {
	if _, ok := EnforcerFromContext(t.Context()); ok {
		t.Fatal("a bare context reported an enforcer")
	}
	if _, ok := ObjectEnforcerFromContext(t.Context()); ok {
		t.Fatal("a bare context reported an object enforcer")
	}
}
