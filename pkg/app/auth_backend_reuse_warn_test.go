package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	authbackend "github.com/jcsvwinston/nucleus/pkg/auth/backend"
)

// reuseProvider is a UserProvider standing in for one application's user
// table. The table name is carried on the value so a test can tell which
// App's provider actually answered.
type reuseProvider struct{ table string }

func (p reuseProvider) FindByID(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (p reuseProvider) FindByUsername(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (p reuseProvider) FindByEmail(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (p reuseProvider) ValidateCredentials(_ context.Context, username, _ string) (*auth.User, error) {
	return &auth.User{ID: p.table + "-" + username, Username: username}, nil
}

// TestBuildAuthChain_WarnsWhenReusingARegisteredBackend pins that a second
// App in the same process says so out loud.
//
// The backend registry is per-process, so the second App's
// RegisterBackend is a duplicate and its own UserProvider is dropped. That
// is documented as intentional ("a second App in the same process reuses
// what is already there"), but the measured effect is stronger than
// "reuses":
//
//	app A (tabla zz_usuarios_a) autentica 'ana'   -> user=&{ns7-ana ...} err=<nil>
//	app B (tabla zz_usuarios_b) autentica 'ana'   -> user=&{ns7-ana ...} err=<nil>   <-- usuaria de A
//	app B (tabla zz_usuarios_b) autentica 'bruno' -> user=<nil> err="invalid credentials"  <-- su PROPIA usuaria
//
// App B authenticates against the wrong table, with no error and no log. In
// a multi-app-in-process deployment that is identity confusion between
// tenants. The registration error was discarded outright (`_ = ...`), so
// nothing anywhere recorded that a provider had been dropped.
func TestBuildAuthChain_WarnsWhenReusingARegisteredBackend(t *testing.T) {
	const name = "zzreuse"
	t.Cleanup(func() { authbackend.Unregister(name) })

	// App A registers first and wins the name.
	var bufA bytes.Buffer
	appA := &App{Logger: slog.New(slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelWarn}))}
	if err := appA.buildAuthChain(appOptions{
		userProvider:     reuseProvider{table: "a"},
		userProviderName: name,
	}, &Config{}); err != nil {
		t.Fatalf("app A buildAuthChain: %v", err)
	}
	if out := bufA.String(); strings.Contains(out, "WARN") {
		t.Errorf("the FIRST registration must be silent, got: %q", out)
	}

	// App B asks for the same name with a different table.
	var bufB bytes.Buffer
	appB := &App{Logger: slog.New(slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelWarn}))}
	if err := appB.buildAuthChain(appOptions{
		userProvider:     reuseProvider{table: "b"},
		userProviderName: name,
	}, &Config{}); err != nil {
		t.Fatalf("app B buildAuthChain: %v", err)
	}

	out := bufB.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("a DROPPED UserProvider must be logged; got: %q", out)
	}
	if !strings.Contains(out, name) {
		t.Errorf("the WARN must name the reused backend %q; got: %q", name, out)
	}
}

// TestUserProviderBackend_ErrUserNotFoundIsNotSilentlyRemapped is the
// control that keeps the test above meaningful: it asserts the provider
// stand-in really is wired the way an App's own table would be.
func TestUserProviderBackend_StandInAuthenticates(t *testing.T) {
	t.Cleanup(func() { authbackend.Unregister("zzcontrol") })
	b, err := auth.NewUserProviderBackend("zzcontrol", reuseProvider{table: "a"})
	if err != nil {
		t.Fatalf("NewUserProviderBackend: %v", err)
	}
	u, err := b.Authenticate(context.Background(), "ana", "pw")
	if err != nil || u == nil {
		t.Fatalf("the stand-in provider must authenticate: user=%v err=%v", u, err)
	}
	if !strings.HasPrefix(u.ID, "a-") {
		t.Errorf("the user must come from table a, got %q", u.ID)
	}
}
