package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestScopeResolver_DefaultFallback(t *testing.T) {
	cfg := DefaultConfig()
	resolver := newRequestScopeResolver(&cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "localhost:8080"

	scope := resolver.Resolve(req)
	if scope.Site != "default" {
		t.Fatalf("expected default site, got %s", scope.Site)
	}
	if scope.Tenant != "" {
		t.Fatalf("expected empty tenant, got %s", scope.Tenant)
	}
	if scope.DatabaseAlias != "default" {
		t.Fatalf("expected default database alias, got %s", scope.DatabaseAlias)
	}
}

func TestRequestScopeResolver_WildcardSubdomainTenant(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DatabaseDefault = "main"
	cfg.Databases = map[string]DatabaseConfig{
		"main":        {URL: "sqlite://:memory:"},
		"tenant_acme": {URL: "sqlite://:memory:"},
	}
	cfg.MultiSite = MultiSiteConfig{
		Enabled:     true,
		DefaultSite: "public",
		Sites: map[string]SiteConfig{
			"public": {
				Hosts:                       []string{"*.example.com"},
				Database:                    "main",
				TenantDatabaseAliasTemplate: "tenant_%s",
			},
		},
	}
	cfg.MultiTenant = MultiTenantConfig{
		Enabled:  true,
		Resolver: "subdomain",
	}
	normalizeRuntimeConfig(&cfg)
	resolver := newRequestScopeResolver(&cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "acme.example.com"
	scope := resolver.Resolve(req)

	if scope.Site != "public" {
		t.Fatalf("expected site public, got %s", scope.Site)
	}
	if scope.Tenant != "acme" {
		t.Fatalf("expected tenant acme, got %s", scope.Tenant)
	}
	if scope.DatabaseAlias != "tenant_acme" {
		t.Fatalf("expected tenant_acme alias, got %s", scope.DatabaseAlias)
	}
}

func TestRequestScopeResolver_HeaderTenantWithExplicitMapping(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DatabaseDefault = "default"
	cfg.Databases = map[string]DatabaseConfig{
		"default":      {URL: "sqlite://:memory:"},
		"tenant_omega": {URL: "sqlite://:memory:"},
	}
	cfg.MultiSite = MultiSiteConfig{
		Enabled:     true,
		DefaultSite: "admin",
		Sites: map[string]SiteConfig{
			"admin": {Hosts: []string{"admin.example.com"}, Database: "default"},
		},
	}
	cfg.MultiTenant = MultiTenantConfig{
		Enabled:  true,
		Resolver: "header",
		Header:   "X-Tenant-ID",
		Tenants: map[string]TenantConfig{
			"omega": {Site: "admin", Database: "tenant_omega"},
		},
	}
	normalizeRuntimeConfig(&cfg)
	resolver := newRequestScopeResolver(&cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "admin.example.com"
	req.Header.Set("X-Tenant-ID", "omega")
	scope := resolver.Resolve(req)

	if scope.Site != "admin" {
		t.Fatalf("expected site admin, got %s", scope.Site)
	}
	if scope.Tenant != "omega" {
		t.Fatalf("expected tenant omega, got %s", scope.Tenant)
	}
	if scope.DatabaseAlias != "tenant_omega" {
		t.Fatalf("expected tenant_omega alias, got %s", scope.DatabaseAlias)
	}
}

func TestHostMatchesPattern(t *testing.T) {
	cases := []struct {
		host    string
		pattern string
		want    bool
	}{
		{host: "a.example.com", pattern: "*.example.com", want: true},
		{host: "example.com", pattern: "*.example.com", want: false},
		{host: "admin.example.com", pattern: "admin.example.com", want: true},
		{host: "admin.example.com", pattern: "api.example.com", want: false},
	}

	for _, tc := range cases {
		if got := hostMatchesPattern(tc.host, tc.pattern); got != tc.want {
			t.Fatalf("hostMatchesPattern(%q, %q)=%v want %v", tc.host, tc.pattern, got, tc.want)
		}
	}
}
