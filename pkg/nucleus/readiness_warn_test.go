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
// are observable without standing up a full application. warnModuleReadiness and
// invokePhase2Stubs touch only core.Logger, so the bare container is sufficient.
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

// TestWarnModuleReadiness_JobsWarns covers R2 (ADR-013): a declared Jobs closure
// is inert until the Phase 2+ background-execution subsystem lands, so it warns
// once at boot rather than silently never running.
func TestWarnModuleReadiness_JobsWarns(t *testing.T) {
	core, buf := newCaptureCore()
	spec := Module[struct{}]{
		Name: "worker",
		Jobs: func(JobRegistry, struct{}) {},
	}.Build()

	warnModuleReadiness(core, spec)

	out := buf.String()
	if !strings.Contains(out, "background execution is not yet wired") {
		t.Fatalf("expected the Jobs/Webhooks readiness WARN, got %q", out)
	}
	if !strings.Contains(out, "jobs=true") {
		t.Fatalf("expected the jobs=true attribute, got %q", out)
	}
}

// TestWarnModuleReadiness_WebhooksWarns mirrors the Jobs case for a declared
// Webhooks closure via the unexported moduleIntrospector view.
func TestWarnModuleReadiness_WebhooksWarns(t *testing.T) {
	core, buf := newCaptureCore()
	spec := Module[struct{}]{
		Name:     "hooks",
		Webhooks: func(WebhookRegistry, struct{}) {},
	}.Build()

	warnModuleReadiness(core, spec)

	if out := buf.String(); !strings.Contains(out, "webhooks=true") {
		t.Fatalf("expected the webhooks=true attribute, got %q", out)
	}
}

// TestInvokePhase2Stubs_PanicDowngradedToWarn proves the panic-guard around the
// Phase-2 shape-only stub calls: a developer Jobs/Webhooks closure that
// dereferences the not-yet-wired nil registry is downgraded to a WARN per stub
// instead of crashing application boot.
func TestInvokePhase2Stubs_PanicDowngradedToWarn(t *testing.T) {
	core, buf := newCaptureCore()
	spec := Module[struct{}]{
		Name:     "panicky",
		Jobs:     func(JobRegistry, struct{}) { panic("boom-jobs") },
		Webhooks: func(WebhookRegistry, struct{}) { panic("boom-webhooks") },
	}.Build()

	// Must not panic.
	invokePhase2Stubs(core, spec)

	out := buf.String()
	if !strings.Contains(out, "boom-jobs") {
		t.Fatalf("expected the Jobs panic downgraded to a WARN, got %q", out)
	}
	if !strings.Contains(out, "boom-webhooks") {
		t.Fatalf("expected the Webhooks panic downgraded to a WARN, got %q", out)
	}
	// The WARN must carry a stack so the developer can locate the offending
	// closure line — the panic value alone does not point at the source.
	if !strings.Contains(out, "stack=") {
		t.Fatalf("expected a stack attribute in the panic WARN, got %q", out)
	}
}
