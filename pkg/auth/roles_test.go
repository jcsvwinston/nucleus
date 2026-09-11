package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
)

// An identity provider answers with a list. Before this, two of three group
// memberships were dropped and nothing said so.
func TestClaims_CarryEveryRole(t *testing.T) {
	m := NewJWTManager(strings.Repeat("roles-secret-xyz", 2), time.Hour, "authtest")
	token, err := m.GenerateWithRoles("1", "ana", "editor", []string{"billing", "support"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	claims, err := m.Validate(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if claims.Role != "editor" {
		t.Errorf("the primary role changed: %q", claims.Role)
	}
	if !claims.HasRole("billing") || !claims.HasRole("SUPPORT") || !claims.HasRole("editor") {
		t.Errorf("roles did not survive the token: %v", claims.AllRoles())
	}
	if claims.HasRole("admin") {
		t.Error("a role the identity does not hold was reported")
	}
	if got := claims.AllRoles(); len(got) != 3 || got[0] != "editor" {
		t.Errorf("AllRoles is %v, want the primary role first", got)
	}
}

// A token minted the old way is unchanged: one role, nothing added.
func TestClaims_SingleRoleTokenIsUnchanged(t *testing.T) {
	m := NewJWTManager(strings.Repeat("roles-secret-xyz", 2), time.Hour, "authtest")
	token, _ := m.Generate("1", "ana", "editor")
	claims, err := m.Validate(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "editor" {
		t.Fatalf("roles is %v, want just the primary one", claims.Roles)
	}
	if got := claims.AllRoles(); len(got) != 1 {
		t.Fatalf("AllRoles is %v", got)
	}
}

func TestNormalizeRoles_DropsBlanksAndDuplicates(t *testing.T) {
	got := normalizeRoles("Editor", []string{" editor ", "", "billing", "BILLING"})
	if len(got) != 2 || got[0] != "Editor" || got[1] != "billing" {
		t.Fatalf("normalizeRoles returned %v", got)
	}
}

func TestUser_AllRolesAndHasRole(t *testing.T) {
	u := &backend.User{Role: "editor", Roles: []string{"billing", "editor"}}
	if got := u.AllRoles(); len(got) != 2 {
		t.Fatalf("AllRoles is %v", got)
	}
	if !u.HasRole("BILLING") || !u.HasRole("editor") || u.HasRole("admin") {
		t.Fatalf("HasRole is wrong for %v", u.AllRoles())
	}

	var nilUser *backend.User
	if nilUser.HasRole("editor") || nilUser.AllRoles() != nil {
		t.Fatal("a nil user must answer no roles rather than panic")
	}
}

// Every token carries an id, which is what makes revocation addressable.
func TestGenerate_StampsAUniqueTokenID(t *testing.T) {
	m := NewJWTManager(strings.Repeat("roles-secret-xyz", 2), time.Hour, "authtest")
	first, _ := m.Generate("1", "ana", "editor")
	second, _ := m.Generate("1", "ana", "editor")

	a, err := m.Validate(first)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	b, _ := m.Validate(second)
	if a.ID == "" || b.ID == "" {
		t.Fatal("a token was minted without an id")
	}
	if a.ID == b.ID {
		t.Fatal("two tokens share an id: revoking one would revoke the other")
	}
}
