package nucleus

import (
	"errors"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func TestValidateReferential(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     *app.Config
		wantErr bool
	}{
		{"nil cfg", nil, false},
		{"empty cfg", &app.Config{}, false},
		{"smtp driver, host+port set", &app.Config{MailDriver: "smtp", SMTPHost: "mail.example.com", SMTPPort: 587}, false},
		{"smtp driver, missing host", &app.Config{MailDriver: "smtp", SMTPPort: 587}, true},
		{"smtp driver, zero port", &app.Config{MailDriver: "smtp", SMTPHost: "mail.example.com", SMTPPort: 0}, true},
		{"smtp driver case-insensitive, missing host", &app.Config{MailDriver: "SMTP", SMTPPort: 587}, true},
		{"non-smtp driver, zero port ok", &app.Config{MailDriver: "noop", SMTPPort: 0}, false},
		{"unset driver, zero port ok", &app.Config{SMTPPort: 0}, false},
		{"samesite none requires secure", &app.Config{SessionCookieSameSite: "none", SessionCookieSecure: false}, true},
		{"samesite none + secure ok", &app.Config{SessionCookieSameSite: "none", SessionCookieSecure: true}, false},
		{"samesite None case + insecure", &app.Config{SessionCookieSameSite: "None", SessionCookieSecure: false}, true},
		{"samesite lax + insecure ok", &app.Config{SessionCookieSameSite: "lax", SessionCookieSecure: false}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReferential(tc.cfg)
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected nil, got %v", err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidConfigReference) {
				t.Errorf("error should wrap ErrInvalidConfigReference, got %v", err)
			}
		})
	}
}

func TestValidateModuleRequires(t *testing.T) {
	t.Parallel()

	cfg := &app.Config{Databases: map[string]app.DatabaseConfig{"default": {}, "reporting": {}}}

	mod := func(name string, aliases ...string) ModuleSpec {
		return Module[struct{}]{Name: name, Requires: aliases}.Build()
	}

	t.Run("satisfied requires", func(t *testing.T) {
		err := validateModuleRequires(cfg, map[string]ModuleSpec{
			"billing": mod("billing", "reporting"),
		})
		if err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("missing alias fails with documented message", func(t *testing.T) {
		err := validateModuleRequires(cfg, map[string]ModuleSpec{
			"billing": mod("billing", "warehouse"),
		})
		if err == nil {
			t.Fatal("expected error for unconfigured database alias")
		}
		if !errors.Is(err, ErrInvalidConfigReference) {
			t.Errorf("error should wrap ErrInvalidConfigReference, got %v", err)
		}
		if !strings.Contains(err.Error(), `module "billing"`) || !strings.Contains(err.Error(), `"warehouse"`) {
			t.Errorf("message should name the module and the missing alias; got %q", err.Error())
		}
	})

	t.Run("no requires is ok", func(t *testing.T) {
		if err := validateModuleRequires(cfg, map[string]ModuleSpec{"plain": mod("plain")}); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("no modules is ok", func(t *testing.T) {
		if err := validateModuleRequires(cfg, nil); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("deterministic first error across modules", func(t *testing.T) {
		// Both fail; "alpha" sorts before "zeta", so its error is reported.
		err := validateModuleRequires(cfg, map[string]ModuleSpec{
			"zeta":  mod("zeta", "missing_z"),
			"alpha": mod("alpha", "missing_a"),
		})
		if err == nil || !strings.Contains(err.Error(), `module "alpha"`) {
			t.Errorf("expected deterministic error naming module \"alpha\"; got %v", err)
		}
	})

	t.Run("within-module first missing alias reported", func(t *testing.T) {
		// Requires() is declaration-ordered; the first missing alias wins.
		err := validateModuleRequires(cfg, map[string]ModuleSpec{
			"m": mod("m", "missing_first", "missing_second"),
		})
		if err == nil || !strings.Contains(err.Error(), `"missing_first"`) {
			t.Errorf("expected error naming the first missing alias; got %v", err)
		}
	})

	t.Run("empty Databases fails a module that requires one", func(t *testing.T) {
		err := validateModuleRequires(&app.Config{}, map[string]ModuleSpec{
			"m": mod("m", "default"),
		})
		if err == nil {
			t.Error("expected error when Databases is empty and a module requires an alias")
		}
	})

	t.Run("default alias resolves after NormalizeRuntimeConfig", func(t *testing.T) {
		// Direct-struct Run normalises before validating (NormalizeRuntimeConfig
		// synthesises "default" when Databases is empty), so a module that
		// requires "default" must pass once normalised — guards the Run wiring.
		normalised := &app.Config{}
		app.NormalizeRuntimeConfig(normalised)
		err := validateModuleRequires(normalised, map[string]ModuleSpec{
			"m": mod("m", "default"),
		})
		if err != nil {
			t.Errorf("expected nil after normalisation synthesises the default alias; got %v", err)
		}
	})
}
