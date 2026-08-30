package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"
)

// demoProvider is a stand-in identity provider for this executable example.
// A real deployment registers "oidc"/"saml"; the framework ships none by
// design (ADR-028), so a runnable example that mounts the FW-29 routes has
// to bring its own. It does the two things a provider does: say where to
// send the browser (Begin), and turn the callback into a user (Complete).
type demoProvider struct{ instance string }

func (p *demoProvider) Name() string { return p.instance }

func (p *demoProvider) Begin(_ context.Context, r federated.BeginRequest) (federated.Redirect, error) {
	// A real provider builds an authorize URL against its issuer and binds
	// r.Nonce into it. The callback it will send the browser back to is
	// r.CallbackURL — the framework's, not one the provider invents.
	return federated.Redirect{
		URL:   "https://idp.demo/authorize?redirect_uri=" + r.CallbackURL,
		State: map[string]string{"nonce": r.Nonce},
	}, nil
}

func (p *demoProvider) Complete(_ context.Context, r federated.CompleteRequest) (*federated.User, error) {
	// A real provider validates the code/assertion in r.Query/r.Form against
	// its issuer. Here we trust the demo IdP and read the username it echoed.
	return &federated.User{Username: r.Query.Get("user")}, nil
}

// TestFederatedRoutesExample is the executable proof of the vía documented in
// website/docs/features/auth.md: the application mounts the two federated
// routes, the framework owns the flow. FW-29 stopped the startup log from
// promising routes nobody mounted; this pins that the documented way to
// mount them actually works — through a.Router (the mux the server serves),
// NOT http.DefaultServeMux, which is what the doc's http.Handle example
// silently used and which the nucleus server never consults.
//
// It also measures the default-deny interaction the doc did not mention:
// the sign-in routes must answer to an UNAUTHENTICATED browser (the whole
// point is the caller has no session yet), so an app on the default RBAC
// stack has to allow them explicitly.
func TestFederatedRoutesExample(t *testing.T) {
	// 1. Register the provider type the config will reference.
	if err := federated.Register("demoidp", func(cfg backend.Config) (federated.Provider, error) {
		return &demoProvider{instance: cfg.Name}, nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { federated.Unregister("demoidp") })

	// 2. Declare the instance and the browser-facing base URL (required
	//    whenever auth_federated is non-empty — the callback URL derives
	//    from it).
	cfg := &app.Config{
		Host:            "127.0.0.1",
		Port:            0,
		DatabaseDefault: "default",
		Databases: map[string]app.DatabaseConfig{
			"default": {URL: "sqlite://:memory:", MaxOpen: 1, MaxIdle: 1},
		},
		LogLevel:      "error",
		LogFormat:     "text",
		PublicBaseURL: "https://app.example.com",
		AuthFederated: []auth.FederatedInstance{
			{Name: "corp", Provider: "demoidp", DisplayName: "Corp SSO"},
		},
	}

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	if a.AuthFederated == nil {
		t.Fatal("App.AuthFederated is nil despite auth_federated being configured")
	}

	set := a.AuthFederated
	const instance = "corp"

	// 3. Mount the two routes on a.Router — the mux the server serves.
	//    http.Handle would register on http.DefaultServeMux, which the
	//    nucleus server never consults, so those routes would 404: exactly
	//    the promise FW-29 removed from the startup log.
	//
	//    For the demo the anti-forgery state rides in an HttpOnly cookie
	//    between the two handlers, which is what a real app does too.
	const stateCookie = "fed_state"
	a.Router.HandleFunc(auth.FederatedStartPath(instance), func(w http.ResponseWriter, r *http.Request) {
		redirectURL, state, err := set.Begin(r.Context(), instance)
		if err != nil {
			http.Error(w, "sign-in unavailable", http.StatusBadGateway)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     stateCookie,
			Value:    state,
			Path:     "/",
			HttpOnly: true,
			MaxAge:   300,
		})
		http.Redirect(w, r, redirectURL, http.StatusFound)
	})
	a.Router.HandleFunc(auth.FederatedCallbackPath(instance), func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(stateCookie)
		if err != nil {
			http.Error(w, "missing state", http.StatusBadRequest)
			return
		}
		user, err := set.Complete(r.Context(), instance, c.Value, r)
		if err != nil {
			http.Error(w, "sign-in failed", http.StatusUnauthorized)
			return
		}
		// A real app establishes its session here. The demo just echoes.
		_, _ = w.Write([]byte("signed in: " + user.Username))
	})

	// 4. The sign-in routes must answer an UNAUTHENTICATED browser. On the
	//    default-deny RBAC stack every subject with no claims resolves to
	//    "anonymous", so the app must grant anonymous access to exactly
	//    these two paths — the doc's YAML never showed this, and without it
	//    the start route returns 403 before Begin ever runs.
	for _, p := range []string{auth.FederatedStartPath(instance), auth.FederatedCallbackPath(instance)} {
		if err := a.Authorizer.AddPolicy("anonymous", p, "*"); err != nil {
			t.Fatalf("AddPolicy(%s): %v", p, err)
		}
	}

	srv := httptest.NewServer(a.Router)
	defer srv.Close()
	jar := &captureJar{}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // don't chase the IdP redirect
		},
	}

	// 5. Start: the browser hits /auth/corp/start and is redirected to the
	//    IdP, carrying the state cookie.
	startResp, err := client.Get(srv.URL + auth.FederatedStartPath(instance))
	if err != nil {
		t.Fatalf("GET start: %v", err)
	}
	startResp.Body.Close()
	if startResp.StatusCode != http.StatusFound {
		t.Fatalf("start must redirect (302) an anonymous browser to the IdP, got %d — is the anonymous policy mounted, and are the routes on a.Router?", startResp.StatusCode)
	}
	if loc := startResp.Header.Get("Location"); loc == "" || loc[:5] != "https" {
		t.Fatalf("start must redirect to the IdP authorize URL, got Location=%q", loc)
	}

	// 6. Callback: the IdP sends the browser back with the state cookie the
	//    start step set. Complete claims the pending flow and returns the
	//    user.
	cbReq, _ := http.NewRequest("GET", srv.URL+auth.FederatedCallbackPath(instance)+"?user=ana", nil)
	cbResp, err := client.Do(cbReq)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer cbResp.Body.Close()
	if cbResp.StatusCode != http.StatusOK {
		t.Fatalf("callback must complete the flow (200), got %d", cbResp.StatusCode)
	}
	buf := make([]byte, 64)
	n, _ := cbResp.Body.Read(buf)
	if got := string(buf[:n]); got != "signed in: ana" {
		t.Fatalf("callback body = %q, want %q", got, "signed in: ana")
	}
}

// captureJar is a minimal cookie jar: the demo only needs the state cookie
// to survive from the start response to the callback request against the
// same test server. net/http/cookiejar would pull in publicsuffix, which
// this single-origin demo does not need.
type captureJar struct{ cookies []*http.Cookie }

func (j *captureJar) SetCookies(_ *url.URL, cs []*http.Cookie) { j.cookies = append(j.cookies, cs...) }
func (j *captureJar) Cookies(*url.URL) []*http.Cookie          { return j.cookies }
