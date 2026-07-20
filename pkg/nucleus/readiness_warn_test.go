package nucleus

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// newCaptureCore returns a minimal *app.App whose Logger writes to the returned
// buffer, so the boot-time readiness diagnostics emitted via moduleLogger(core)
// are observable without standing up a full application. warnModuleReadiness
// touches only core.Logger, so the bare container is sufficient.
func newCaptureCore() (*app.App, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	core := &app.App{Logger: slog.New(slog.NewTextHandler(buf, nil))}
	return core, buf
}

// TestWarnModuleReadiness_MigrationsWarns covers R1 (ADR-013): a module that
// embeds a non-empty migrations FS gets one boot WARN, because Nucleus is
// SQL-first and never auto-applies them.
func TestWarnModuleReadiness_MigrationsWarns(t *testing.T) {
	core, buf := newCaptureCore()
	spec := Module[struct{}]{
		Name: "billing",
		Migrations: fstest.MapFS{
			"001_init.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		},
	}.Build()

	warnModuleReadiness(core, spec)

	out := buf.String()
	if !strings.Contains(out, "does not auto-apply") {
		t.Fatalf("expected the embedded-migrations WARN, got %q", out)
	}
	if !strings.Contains(out, "billing") {
		t.Fatalf("expected the WARN to name the module, got %q", out)
	}
}

// TestWarnModuleReadiness_EmptyMigrationsSilent guards the no-op case: a non-nil
// but empty migrations FS must not warn, so the common "no migrations" path stays
// quiet.
func TestWarnModuleReadiness_EmptyMigrationsSilent(t *testing.T) {
	core, buf := newCaptureCore()
	spec := Module[struct{}]{Name: "empty-fs", Migrations: fstest.MapFS{}}.Build()

	warnModuleReadiness(core, spec)

	if buf.Len() != 0 {
		t.Fatalf("an empty migrations FS must not warn, got %q", buf.String())
	}
}

// TestWarnModuleReadiness_BareModuleSilent confirms a module that advertises no
// inert surface (no migrations, jobs, or webhooks) produces no diagnostics.
func TestWarnModuleReadiness_BareModuleSilent(t *testing.T) {
	core, buf := newCaptureCore()
	spec := Module[struct{}]{Name: "plain"}.Build()

	warnModuleReadiness(core, spec)

	if buf.Len() != 0 {
		t.Fatalf("a module with no inert surface must not warn, got %q", buf.String())
	}
}

// TestWarnModuleReadiness_JobsWebhooksNoLongerWarn locks in the ADR-010
// Phase 2 change: declared Jobs/Webhooks closures execute for real now, so
// the former "background execution is not yet wired" readiness WARN must be
// gone — a module declaring both stays silent here.
func TestWarnModuleReadiness_JobsWebhooksNoLongerWarn(t *testing.T) {
	core, buf := newCaptureCore()
	spec := Module[struct{}]{
		Name:     "worker",
		Jobs:     func(JobRegistry, struct{}) {},
		Webhooks: func(WebhookRegistry, struct{}) {},
	}.Build()

	warnModuleReadiness(core, spec)

	if buf.Len() != 0 {
		t.Fatalf("Jobs/Webhooks are executed since Phase 2 and must not produce a readiness WARN, got %q", buf.String())
	}
}
