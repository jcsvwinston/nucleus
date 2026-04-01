package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCanonicalizeCommand_RunserverAddress(t *testing.T) {
	cmd, args, err := canonicalizeCommand("runserver", []string{"127.0.0.1:9001"})
	if err != nil {
		t.Fatalf("canonicalize runserver failed: %v", err)
	}
	if cmd != "serve" {
		t.Fatalf("expected command serve, got %q", cmd)
	}
	if got, want := strings.Join(args, " "), "--host 127.0.0.1 --port 9001"; got != want {
		t.Fatalf("unexpected args: got %q want %q", got, want)
	}
}

func TestCanonicalizeCommand_RunserverPortOnly(t *testing.T) {
	cmd, args, err := canonicalizeCommand("runserver", []string{"8088"})
	if err != nil {
		t.Fatalf("canonicalize runserver failed: %v", err)
	}
	if cmd != "serve" {
		t.Fatalf("expected command serve, got %q", cmd)
	}
	if got, want := strings.Join(args, " "), "--port 8088"; got != want {
		t.Fatalf("unexpected args: got %q want %q", got, want)
	}
}

func TestCanonicalizeCommand_RunserverFlagOverride(t *testing.T) {
	cmd, args, err := canonicalizeCommand("runserver", []string{"--host", "0.0.0.0", "--port", "8010", "127.0.0.1:9001"})
	if err != nil {
		t.Fatalf("canonicalize runserver failed: %v", err)
	}
	if cmd != "serve" {
		t.Fatalf("expected command serve, got %q", cmd)
	}
	if got, want := strings.Join(args, " "), "--host 0.0.0.0 --port 8010"; got != want {
		t.Fatalf("unexpected args: got %q want %q", got, want)
	}
}

func TestCanonicalizeCommand_RunserverInvalidAddress(t *testing.T) {
	_, _, err := canonicalizeCommand("runserver", []string{"localhost:notaport"})
	if err == nil {
		t.Fatal("expected error for invalid runserver address")
	}
}

func TestCanonicalizeCommand_MakeMigrations(t *testing.T) {
	cmd, args, err := canonicalizeCommand("makemigrations", []string{"add_users"})
	if err != nil {
		t.Fatalf("canonicalize makemigrations failed: %v", err)
	}
	if cmd != "migrate" {
		t.Fatalf("expected command migrate, got %q", cmd)
	}
	if got, want := strings.Join(args, " "), "create add_users"; got != want {
		t.Fatalf("unexpected args: got %q want %q", got, want)
	}
}

func TestCanonicalizeCommand_MakeMigrationsNoName(t *testing.T) {
	_, _, err := canonicalizeCommand("makemigrations", nil)
	if err == nil {
		t.Fatal("expected error when migration name is missing")
	}
}

func TestCanonicalizeCommand_ShowMigrations(t *testing.T) {
	cmd, args, err := canonicalizeCommand("showmigrations", []string{"--migrations", "sql/migrations"})
	if err != nil {
		t.Fatalf("canonicalize showmigrations failed: %v", err)
	}
	if cmd != "migrate" {
		t.Fatalf("expected command migrate, got %q", cmd)
	}
	if got, want := strings.Join(args, " "), "--migrations sql/migrations status"; got != want {
		t.Fatalf("unexpected args: got %q want %q", got, want)
	}
}

func TestCanonicalizeCommand_DirectAlias(t *testing.T) {
	cmd, args, err := canonicalizeCommand("dbshell", []string{"--config", "goframe.yaml"})
	if err != nil {
		t.Fatalf("canonicalize dbshell failed: %v", err)
	}
	if cmd != "shell" {
		t.Fatalf("expected command shell, got %q", cmd)
	}
	if got, want := strings.Join(args, " "), "--config goframe.yaml"; got != want {
		t.Fatalf("unexpected args: got %q want %q", got, want)
	}
}

func TestRun_HelpAliasResolves(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := Run([]string{"help", "createsuperuser"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%s)", code, errOut.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "Usage of createuser:") {
		t.Fatalf("expected createuser help output, got: %s", combined)
	}
}
