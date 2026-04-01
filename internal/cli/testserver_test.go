package cli

import (
	"strings"
	"testing"
)

func TestResolveTestServerFixturePath(t *testing.T) {
	path, err := resolveTestServerFixturePath("", []string{"fixtures.json"})
	if err != nil {
		t.Fatalf("resolve positional fixture failed: %v", err)
	}
	if path != "fixtures.json" {
		t.Fatalf("unexpected fixture path: %q", path)
	}

	path, err = resolveTestServerFixturePath("data.json", nil)
	if err != nil {
		t.Fatalf("resolve --fixture path failed: %v", err)
	}
	if path != "data.json" {
		t.Fatalf("unexpected fixture path: %q", path)
	}
}

func TestResolveTestServerFixturePathErrors(t *testing.T) {
	if _, err := resolveTestServerFixturePath("", nil); err == nil {
		t.Fatal("expected error when fixture path is missing")
	}
	if _, err := resolveTestServerFixturePath("a.json", []string{"b.json"}); err == nil {
		t.Fatal("expected error when both --fixture and positional path are provided")
	}
}

func TestBuildTestServerLoadArgs(t *testing.T) {
	args := buildTestServerLoadArgs(testServerLoadOptions{
		configPath: "goframe.yaml",
		fixture:    "fixtures.json",
		tablesRaw:  "users,posts",
		truncate:   true,
		force:      true,
		yes:        false,
		dryRun:     true,
	})
	got := strings.Join(args, " ")
	want := "--config goframe.yaml --tables users,posts --truncate --force --dry-run fixtures.json"
	if got != want {
		t.Fatalf("unexpected load args: got %q want %q", got, want)
	}
}

func TestBuildTestServerServeArgs(t *testing.T) {
	args := buildTestServerServeArgs("goframe.yaml", "127.0.0.1", 9090)
	got := strings.Join(args, " ")
	want := "--config goframe.yaml --host 127.0.0.1 --port 9090"
	if got != want {
		t.Fatalf("unexpected serve args: got %q want %q", got, want)
	}
}
