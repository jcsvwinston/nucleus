package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// TestHandleListRBACPolicies_IncludesEft pins that the admin RBAC inspector
// surfaces the eft (allow|deny) column. Without it, a deny rule and an allow
// rule for the same (sub, obj, act) are indistinguishable in the UI even
// though the Casbin model enforces deny-override correctly. Regression guard
// for the F-4 security-audit follow-up (ADR-015).
func TestHandleListRBACPolicies_IncludesEft(t *testing.T) {
	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	if err := enf.AddPolicy("alice", "/api/users/*", "read"); err != nil { // allow
		t.Fatalf("AddPolicy: %v", err)
	}
	if err := enf.Deny("mallory", "/api/users/*", "read"); err != nil { // deny
		t.Fatalf("Deny: %v", err)
	}

	// Zero-value PanelConfig => config.Auth == nil => authorizeAction allows.
	p := &Panel{rbac: enf}

	req := httptest.NewRequest(http.MethodGet, "/api/rbac/policies", nil)
	rec := httptest.NewRecorder()
	ctx := router.NewContext(rec, req, nil)

	if err := p.handleListRBACPolicies(ctx); err != nil {
		t.Fatalf("handleListRBACPolicies: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload struct {
		Enabled  bool                `json:"enabled"`
		Policies []map[string]string `json:"policies"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.Enabled {
		t.Fatal("expected enabled=true")
	}

	eftBySubject := make(map[string]string, len(payload.Policies))
	for _, pol := range payload.Policies {
		if pol["eft"] == "" {
			t.Errorf("policy %v is missing the eft column", pol)
		}
		eftBySubject[pol["sub"]] = pol["eft"]
	}

	if got := eftBySubject["alice"]; got != "allow" {
		t.Errorf("alice rule: expected eft=allow, got %q", got)
	}
	if got := eftBySubject["mallory"]; got != "deny" {
		t.Errorf("mallory rule: expected eft=deny, got %q", got)
	}
}
