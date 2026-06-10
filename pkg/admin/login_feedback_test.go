package admin

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A rejected login must be visible to the user. When the SPA build is
// available, renderLoginPage serves index.html — historically dropping the
// error message entirely, so a failed login was indistinguishable from
// "nothing happened" (fleetdesk finding #16). The message now travels as an
// injected meta tag the SPA login page renders.
func TestRenderLoginPage_SPASurfacesErrorMessage(t *testing.T) {
	distDir := t.TempDir()
	shell := `<!doctype html><html><head><title>x</title></head><body></body></html>`
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte(shell), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(adminUIDirEnv, distDir)

	a := &DatabaseAdminAuth{prefix: "/admin"}

	t.Run("error injected and escaped", func(t *testing.T) {
		w := httptest.NewRecorder()
		a.renderLoginPage(w, 401, "/admin/", `Invalid credentials. <script>alert(1)</script>`, "")
		body := w.Body.String()
		if w.Code != 401 {
			t.Fatalf("status = %d, want 401", w.Code)
		}
		if !strings.Contains(body, `<meta name="nucleus-admin-login-error" content="Invalid credentials. &lt;script&gt;alert(1)&lt;/script&gt;">`) {
			t.Errorf("login error meta missing or unescaped:\n%s", body)
		}
		if got := strings.Count(body, "nucleus-admin-prefix"); got != 1 {
			t.Errorf("prefix meta count = %d, want exactly 1:\n%s", got, body)
		}
		if got := strings.Count(body, "<head>"); got != 1 {
			t.Errorf("<head> count = %d, want exactly 1:\n%s", got, body)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", cc)
		}
	})

	t.Run("attribute breakout via double quote is escaped", func(t *testing.T) {
		w := httptest.NewRecorder()
		a.renderLoginPage(w, 401, "/admin/", `x" onload="alert(1)`, "")
		body := w.Body.String()
		if !strings.Contains(body, `content="x&#34; onload=&#34;alert(1)">`) {
			t.Errorf("double quote not escaped in attribute context:\n%s", body)
		}
	})

	t.Run("error wins over info when both set", func(t *testing.T) {
		w := httptest.NewRecorder()
		a.renderLoginPage(w, 401, "/admin/", "Bad password.", "Signed out.")
		body := w.Body.String()
		if !strings.Contains(body, "nucleus-admin-login-error") {
			t.Errorf("error meta missing:\n%s", body)
		}
		if strings.Contains(body, "nucleus-admin-login-info") {
			t.Errorf("info meta must be absent when an error is present:\n%s", body)
		}
	})

	t.Run("clean GET injects nothing", func(t *testing.T) {
		w := httptest.NewRecorder()
		a.renderLoginPage(w, 200, "/admin/", "", "")
		if strings.Contains(w.Body.String(), "nucleus-admin-login-") {
			t.Errorf("no message expected on clean render:\n%s", w.Body.String())
		}
	})

	t.Run("info message uses the info meta", func(t *testing.T) {
		w := httptest.NewRecorder()
		a.renderLoginPage(w, 200, "/admin/", "", "Signed out.")
		if !strings.Contains(w.Body.String(), `<meta name="nucleus-admin-login-info" content="Signed out.">`) {
			t.Errorf("info meta missing:\n%s", w.Body.String())
		}
	})
}
