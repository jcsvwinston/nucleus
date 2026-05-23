package nucleus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// configEndpointTestConfig returns an in-memory SQLite app config with admin
// bootstrap credentials, mirroring pkg/app's testAppConfig. The bootstrap
// password is a redaction-set key (admin_bootstrap_password), so the snapshot
// it produces exercises real redaction.
func configEndpointTestConfig() *app.Config {
	return &app.Config{
		Host:            "127.0.0.1",
		Port:            0,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    2 * time.Second,
		IdleTimeout:     5 * time.Second,
		DatabaseDefault: "default",
		Databases: map[string]app.DatabaseConfig{
			"default": {
				URL:         "sqlite://:memory:",
				MaxOpen:     1,
				MaxIdle:     1,
				MaxLifetime: time.Minute,
			},
		},
		LogLevel:               "error",
		LogFormat:              "text",
		AdminPrefix:            "/admin",
		AdminTitle:             "Test Admin",
		AdminBootstrapUsername: "admin",
		AdminBootstrapEmail:    "admin@example.com",
		AdminBootstrapPassword: "supersecret123",
	}
}

// adminLoginCookies logs in with the bootstrap credentials and returns the
// resulting session cookies, reusing the flow validated in
// pkg/app/app_test.go:TestAppNew_AdminLoginWithBootstrapCredentials.
func adminLoginCookies(t *testing.T, core *app.App) []*http.Cookie {
	t.Helper()
	form := url.Values{
		"username": {"admin"},
		"password": {"supersecret123"},
		"next":     {"/admin/"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	core.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("admin login: expected 303, got %d body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("admin login: expected a session cookie to be set")
	}
	return cookies
}

func TestConfigEndpoint_Anonymous_Forbidden(t *testing.T) {
	core, err := app.New(configEndpointTestConfig())
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	defer core.Shutdown(context.Background())

	mountConfigEndpoint(core, effectiveFromConfig(*core.Config, core.Config.LogRedactExtraKeys))

	req := httptest.NewRequest(http.MethodGet, configEndpointPath, nil)
	rec := httptest.NewRecorder()
	core.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous GET %s: expected 403, got %d body=%s", configEndpointPath, rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "supersecret123") {
		t.Fatal("403 response must not leak any configuration value")
	}
}

func TestConfigEndpoint_AdminSession_OK(t *testing.T) {
	core, err := app.New(configEndpointTestConfig())
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	defer core.Shutdown(context.Background())

	mountConfigEndpoint(core, effectiveFromConfig(*core.Config, core.Config.LogRedactExtraKeys))

	cookies := adminLoginCookies(t, core)

	req := httptest.NewRequest(http.MethodGet, configEndpointPath, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	core.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin-session GET %s: expected 200, got %d body=%s", configEndpointPath, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected JSON content type, got %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected Cache-Control: no-store, got %q", cc)
	}

	var got EffectiveConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
	if len(got.Values) == 0 {
		t.Fatal("expected a non-empty effective config")
	}

	// The bootstrap password must be redacted, never returned in cleartext.
	if strings.Contains(rec.Body.String(), "supersecret123") {
		t.Fatal("response leaked the admin bootstrap password in cleartext")
	}
	var sawSecret bool
	for _, v := range got.Values {
		if v.Key == "admin_bootstrap_password" {
			sawSecret = true
			if !v.Redacted {
				t.Errorf("admin_bootstrap_password should be marked redacted")
			}
			if v.Value != observe.RedactionPlaceholder {
				t.Errorf("admin_bootstrap_password value = %v; want redaction placeholder", v.Value)
			}
		}
	}
	if !sawSecret {
		t.Error("expected admin_bootstrap_password key in the effective config")
	}
}

func TestConfigEndpoint_AbsentWithoutAdmin(t *testing.T) {
	core, err := app.New(configEndpointTestConfig(), app.WithoutDefaults())
	if err != nil {
		t.Fatalf("app.New(WithoutDefaults): %v", err)
	}
	defer core.Shutdown(context.Background())

	if core.Admin != nil {
		t.Fatal("precondition: admin subsystem should be absent under WithoutDefaults")
	}

	// Mount is a no-op when admin is absent.
	mountConfigEndpoint(core, effectiveFromConfig(*core.Config, core.Config.LogRedactExtraKeys))

	req := httptest.NewRequest(http.MethodGet, configEndpointPath, nil)
	rec := httptest.NewRecorder()
	core.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("WithoutDefaults GET %s: expected 404 (endpoint not mounted), got %d", configEndpointPath, rec.Code)
	}
}

func TestConfigEndpoint_NonGetMethodNotAllowed(t *testing.T) {
	core, err := app.New(configEndpointTestConfig())
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	defer core.Shutdown(context.Background())

	mountConfigEndpoint(core, effectiveFromConfig(*core.Config, core.Config.LogRedactExtraKeys))

	// Only GET is registered. A POST matches the path but not the method,
	// so net/http's method-aware mux returns 405 — without ever invoking
	// the GET route's auth middleware, and without leaking config.
	req := httptest.NewRequest(http.MethodPost, configEndpointPath, nil)
	rec := httptest.NewRecorder()
	core.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST %s: expected 405, got %d body=%s", configEndpointPath, rec.Code, rec.Body.String())
	}
}

// TestApp_EffectiveSnapshot_FromConfigFile exercises the builder→Run
// threading: FromConfigFile must capture a file-provenance snapshot that
// effectiveSnapshot then returns verbatim (the non-nil fast path), rather
// than falling back to the runtime flatten.
func TestApp_EffectiveSnapshot_FromConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/nucleus.yaml"
	const body = "port: 9999\nadmin_bootstrap_password: topsecret\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	a, err := New().FromConfigFile(path).Build()
	if err != nil {
		t.Fatalf("FromConfigFile.Build: %v", err)
	}
	if a.effective == nil {
		t.Fatal("FromConfigFile must capture an effective snapshot")
	}

	// nil core forces the method to rely on the stored snapshot, proving the
	// fast path (not the runtime fallback) is what serves the endpoint.
	snap := a.effectiveSnapshot(nil)

	byKey := make(map[string]EffectiveValue, len(snap.Values))
	for _, v := range snap.Values {
		byKey[v.Key] = v
	}
	port, ok := byKey["port"]
	if !ok {
		t.Fatal("expected 'port' in the snapshot")
	}
	if port.Source.Kind != "yaml" || port.Source.Path != path {
		t.Errorf("port source = %+v; want yaml:%s", port.Source, path)
	}
	if port.Source.Kind == sourceKindRuntime {
		t.Error("file-loaded snapshot must not use the runtime source kind")
	}
	secret, ok := byKey["admin_bootstrap_password"]
	if !ok || !secret.Redacted || secret.Value != observe.RedactionPlaceholder {
		t.Errorf("admin_bootstrap_password not redacted in file snapshot: %+v", secret)
	}
}

// TestEffectiveFromConfig_CloudCredentials pins the redaction posture for
// cloud storage config: the S3 access key ID is a credential and must be
// redacted, while the Azure account name is a public identifier and is
// deliberately left in cleartext (see pkg/observe/redact.go).
func TestEffectiveFromConfig_CloudCredentials(t *testing.T) {
	cfg := configEndpointTestConfig()
	cfg.Storage.S3.AccessKeyID = "AKIAEXAMPLEDONOTLEAK"
	cfg.Storage.S3.SecretAccessKey = "s3secretdonotleak"
	cfg.Storage.Azure.AccountName = "publicaccountname"
	cfg.Storage.Azure.AccountKey = "azuresecretdonotleak"

	ec := effectiveFromConfig(*cfg, cfg.LogRedactExtraKeys)
	byKey := make(map[string]EffectiveValue, len(ec.Values))
	for _, v := range ec.Values {
		byKey[v.Key] = v
	}

	for _, secretKey := range []string{"storage.s3.access_key_id", "storage.s3.secret_access_key", "storage.azure.account_key"} {
		v, ok := byKey[secretKey]
		if !ok {
			t.Errorf("expected %q in flattened config", secretKey)
			continue
		}
		if !v.Redacted || v.Value != observe.RedactionPlaceholder {
			t.Errorf("%s must be redacted, got %+v", secretKey, v)
		}
	}

	// Azure account name is a public DNS label — deliberately not redacted.
	if v, ok := byKey["storage.azure.account_name"]; ok {
		if v.Redacted {
			t.Errorf("storage.azure.account_name should not be redacted (public identifier)")
		}
		if v.Value != "publicaccountname" {
			t.Errorf("storage.azure.account_name = %v; want publicaccountname", v.Value)
		}
	}
}

func TestEffectiveFromConfig_RedactsSecrets(t *testing.T) {
	cfg := configEndpointTestConfig()
	ec := effectiveFromConfig(*cfg, cfg.LogRedactExtraKeys)

	if len(ec.Values) == 0 {
		t.Fatal("expected flattened values")
	}
	var sawSecret, sawPlain bool
	for _, v := range ec.Values {
		if v.Source.Kind != sourceKindRuntime {
			t.Errorf("key %q: source kind = %q; want %q", v.Key, v.Source.Kind, sourceKindRuntime)
		}
		switch v.Key {
		case "admin_bootstrap_password":
			sawSecret = true
			if !v.Redacted || v.Value != observe.RedactionPlaceholder {
				t.Errorf("admin_bootstrap_password not redacted: redacted=%v value=%v", v.Redacted, v.Value)
			}
		case "admin_prefix":
			sawPlain = true
			if v.Redacted {
				t.Errorf("admin_prefix should not be redacted")
			}
			if v.Value != "/admin" {
				t.Errorf("admin_prefix = %v; want /admin", v.Value)
			}
		}
	}
	if !sawSecret {
		t.Error("expected admin_bootstrap_password in flattened config")
	}
	if !sawPlain {
		t.Error("expected admin_prefix in flattened config")
	}
}
