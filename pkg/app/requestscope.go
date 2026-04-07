package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
)

type requestScopeCtxKey struct{}

// RequestScope captures site/tenant/database routing decisions for one request.
type RequestScope struct {
	Host          string
	Site          string
	Tenant        string
	DatabaseAlias string
}

// RequestScopeFromContext returns the resolved request scope when available.
func RequestScopeFromContext(ctx context.Context) (RequestScope, bool) {
	if ctx == nil {
		return RequestScope{}, false
	}
	scope, ok := ctx.Value(requestScopeCtxKey{}).(RequestScope)
	return scope, ok
}

// SiteFromContext returns the resolved site name or empty when unavailable.
func SiteFromContext(ctx context.Context) string {
	scope, ok := RequestScopeFromContext(ctx)
	if !ok {
		return ""
	}
	return scope.Site
}

// TenantFromContext returns the resolved tenant id or empty when unavailable.
func TenantFromContext(ctx context.Context) string {
	scope, ok := RequestScopeFromContext(ctx)
	if !ok {
		return ""
	}
	return scope.Tenant
}

// DatabaseAliasFromContext returns the DB alias selected for the request.
func DatabaseAliasFromContext(ctx context.Context) string {
	scope, ok := RequestScopeFromContext(ctx)
	if !ok {
		return ""
	}
	return scope.DatabaseAlias
}

type requestScopeResolver struct {
	defaultSite          string
	defaultDatabaseAlias string

	multiSiteEnabled bool
	sites            map[string]SiteConfig
	siteOrder        []string

	multiTenantEnabled      bool
	multiTenantResolver     string
	multiTenantHeader       string
	multiTenantDefault      string
	multiTenantDBAliasTempl string
	requireTenantIsolatedDB bool
	tenants                 map[string]TenantConfig
}

const tenantIsolationViolationAlias = "__tenant_isolation_violation__"

func newRequestScopeResolver(cfg *Config) *requestScopeResolver {
	if cfg == nil {
		return &requestScopeResolver{
			defaultSite:          "default",
			defaultDatabaseAlias: "default",
			sites:                map[string]SiteConfig{"default": {Database: "default"}},
			siteOrder:            []string{"default"},
			tenants:              map[string]TenantConfig{},
		}
	}

	sites := cloneSiteConfigMap(cfg.MultiSite.Sites)
	if len(sites) == 0 {
		sites = map[string]SiteConfig{
			"default": {Database: cfg.DefaultDatabaseAlias()},
		}
	}
	defaultSite := normalizeAlias(cfg.MultiSite.DefaultSite)
	if defaultSite == "" {
		defaultSite = "default"
	}
	if _, ok := sites[defaultSite]; !ok {
		sites[defaultSite] = SiteConfig{Database: cfg.DefaultDatabaseAlias()}
	}

	order := make([]string, 0, len(sites))
	for site := range sites {
		order = append(order, site)
	}
	sort.Strings(order)

	tenants := cloneTenantConfigMap(cfg.MultiTenant.Tenants)
	if tenants == nil {
		tenants = map[string]TenantConfig{}
	}

	return &requestScopeResolver{
		defaultSite:             defaultSite,
		defaultDatabaseAlias:    cfg.DefaultDatabaseAlias(),
		multiSiteEnabled:        cfg.MultiSite.Enabled,
		sites:                   sites,
		siteOrder:               order,
		multiTenantEnabled:      cfg.MultiTenant.Enabled,
		multiTenantResolver:     cfg.MultiTenant.Resolver,
		multiTenantHeader:       cfg.MultiTenant.Header,
		multiTenantDefault:      cfg.MultiTenant.DefaultTenant,
		multiTenantDBAliasTempl: cfg.MultiTenant.DatabaseAliasTemplate,
		requireTenantIsolatedDB: cfg.MultiTenant.RequireIsolatedDB,
		tenants:                 tenants,
	}
}

func (r *requestScopeResolver) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			scope := r.Resolve(req)
			ctx := context.WithValue(req.Context(), requestScopeCtxKey{}, scope)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

func (r *requestScopeResolver) Resolve(req *http.Request) RequestScope {
	if req == nil {
		return RequestScope{
			Site:          "default",
			DatabaseAlias: "default",
		}
	}
	if r == nil {
		return RequestScope{
			Host:          normalizeRequestHost(req),
			Site:          "default",
			DatabaseAlias: "default",
		}
	}

	host := normalizeRequestHost(req)
	siteName, siteCfg, matchedPattern := r.resolveSite(host)
	tenantID := r.resolveTenant(req, host, matchedPattern)
	dbAlias := r.resolveDatabaseAlias(siteName, siteCfg, tenantID)

	return RequestScope{
		Host:          host,
		Site:          siteName,
		Tenant:        tenantID,
		DatabaseAlias: dbAlias,
	}
}

func (r *requestScopeResolver) resolveSite(host string) (string, SiteConfig, string) {
	defaultSite := normalizeAlias(r.defaultSite)
	if defaultSite == "" {
		defaultSite = "default"
	}
	defaultCfg := r.sites[defaultSite]
	if defaultCfg.Database == "" {
		defaultCfg.Database = r.defaultDatabaseAlias
	}
	if !r.multiSiteEnabled || host == "" {
		return defaultSite, defaultCfg, ""
	}

	for _, siteName := range r.siteOrder {
		site := r.sites[siteName]
		for _, pattern := range site.Hosts {
			if hostMatchesPattern(host, pattern) {
				if site.Database == "" {
					site.Database = r.defaultDatabaseAlias
				}
				return siteName, site, pattern
			}
		}
	}
	return defaultSite, defaultCfg, ""
}

func (r *requestScopeResolver) resolveTenant(req *http.Request, host, matchedPattern string) string {
	if !r.multiTenantEnabled {
		return ""
	}

	var tenant string
	switch r.multiTenantResolver {
	case "header":
		tenant = normalizeAlias(req.Header.Get(r.multiTenantHeader))
	default:
		tenant = extractTenantFromHost(host, matchedPattern)
	}
	if tenant == "" {
		tenant = normalizeAlias(r.multiTenantDefault)
	}
	return tenant
}

func (r *requestScopeResolver) resolveDatabaseAlias(siteName string, siteCfg SiteConfig, tenantID string) string {
	alias := normalizeAlias(siteCfg.Database)
	if alias == "" {
		alias = normalizeAlias(r.defaultDatabaseAlias)
	}

	if tenantID == "" {
		return alias
	}

	if tenantCfg, ok := r.tenants[tenantID]; ok {
		tenantSite := normalizeAlias(tenantCfg.Site)
		if tenantSite == "" || tenantSite == normalizeAlias(siteName) {
			if explicit := normalizeAlias(tenantCfg.Database); explicit != "" {
				if r.requireTenantIsolatedDB && explicit == alias {
					return tenantIsolationViolationAlias
				}
				return explicit
			}
		}
	}

	if candidate := formatAliasTemplate(siteCfg.TenantDatabaseAliasTemplate, tenantID); candidate != "" {
		if r.requireTenantIsolatedDB && candidate == alias {
			return tenantIsolationViolationAlias
		}
		return candidate
	}
	if candidate := formatAliasTemplate(r.multiTenantDBAliasTempl, tenantID); candidate != "" {
		if r.requireTenantIsolatedDB && candidate == alias {
			return tenantIsolationViolationAlias
		}
		return candidate
	}
	if r.requireTenantIsolatedDB {
		return tenantIsolationViolationAlias
	}
	return alias
}

func normalizeRequestHost(req *http.Request) string {
	if req == nil {
		return ""
	}
	raw := strings.TrimSpace(req.Host)
	if raw == "" && req.URL != nil {
		raw = strings.TrimSpace(req.URL.Host)
	}
	if raw == "" {
		return ""
	}

	if host, _, err := net.SplitHostPort(raw); err == nil && host != "" {
		raw = host
	} else if strings.HasPrefix(raw, "[") && strings.Contains(raw, "]") {
		raw = strings.TrimPrefix(strings.Split(raw, "]")[0], "[")
	}

	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.TrimSuffix(raw, ".")
	return raw
}

func hostMatchesPattern(host, pattern string) bool {
	host = normalizeAlias(host)
	pattern = normalizeAlias(pattern)
	if host == "" || pattern == "" {
		return false
	}
	if !strings.HasPrefix(pattern, "*.") {
		return host == pattern
	}
	suffix := strings.TrimPrefix(pattern, "*")
	if !strings.HasSuffix(host, suffix) {
		return false
	}
	prefix := strings.TrimSuffix(host, suffix)
	return strings.TrimSpace(prefix) != ""
}

func extractTenantFromHost(host, matchedPattern string) string {
	host = normalizeAlias(host)
	matchedPattern = normalizeAlias(matchedPattern)
	if host == "" {
		return ""
	}

	if strings.HasPrefix(matchedPattern, "*.") {
		suffix := strings.TrimPrefix(matchedPattern, "*")
		if strings.HasSuffix(host, suffix) {
			prefix := strings.TrimSuffix(host, suffix)
			prefix = strings.TrimSuffix(prefix, ".")
			return normalizeAlias(prefix)
		}
	}

	parts := strings.Split(host, ".")
	if len(parts) < 3 {
		return ""
	}
	return normalizeAlias(parts[0])
}

func formatAliasTemplate(template, tenantID string) string {
	tpl := strings.TrimSpace(template)
	tenant := normalizeAlias(tenantID)
	if tpl == "" || tenant == "" {
		return ""
	}
	if strings.Contains(tpl, "%s") {
		return normalizeAlias(fmt.Sprintf(tpl, tenant))
	}
	if strings.Contains(tpl, "{tenant}") {
		return normalizeAlias(strings.ReplaceAll(tpl, "{tenant}", tenant))
	}
	return normalizeAlias(tpl)
}
