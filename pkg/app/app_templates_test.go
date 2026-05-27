package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestNew_TemplatesDir_EmptyAndMalformed covers the template-init path in New:
// a present-but-empty TemplatesDir must NOT panic (the bug fixed 2026-05-27 —
// template.Must(ParseGlob) panicked on a zero-match glob), and a genuinely
// malformed template must return an error rather than panic.
func TestNew_TemplatesDir_EmptyAndMalformed(t *testing.T) {
	t.Run("empty templates dir starts cleanly with no templates", func(t *testing.T) {
		cfg := testAppConfig()
		cfg.TemplatesDir = t.TempDir() // exists, contains no .html

		a, err := New(cfg)
		if err != nil {
			t.Fatalf("New with an empty templates dir should succeed, got: %v", err)
		}
		defer a.Shutdown(context.Background())
		if a.Templates != nil {
			t.Errorf("expected no templates loaded from an empty dir, got %v", a.Templates)
		}
	})

	t.Run("malformed template returns an error, not a panic", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "broken.html"), []byte("{{ .Unclosed "), 0o600); err != nil {
			t.Fatalf("write template: %v", err)
		}
		cfg := testAppConfig()
		cfg.TemplatesDir = dir

		a, err := New(cfg)
		if err == nil {
			a.Shutdown(context.Background())
			t.Fatal("expected New to return an error for a malformed template")
		}
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Op != "New templates" {
			t.Errorf("expected an *OpError with Op %q, got: %v", "New templates", err)
		}
	})
}
