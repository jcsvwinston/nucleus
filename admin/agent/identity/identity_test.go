package identity

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func discardLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestResolve_CreatesAndPersists verifies the first call writes a fresh
// UUID and subsequent calls (or new Resolvers on the same dir) load it.
func TestResolve_CreatesAndPersists(t *testing.T) {
	dir := t.TempDir()

	r := New(dir, discardLogger(t))
	first := r.Resolve()
	if !first.Persistent {
		t.Fatalf("first resolve should be persistent, got %+v", first)
	}
	if first.Source != "created" {
		t.Fatalf("first.Source = %q, want created", first.Source)
	}
	if first.NodeID == "" {
		t.Fatal("empty NodeID")
	}
	// Looks like a UUID v4? (8-4-4-4-12)
	if strings.Count(first.NodeID, "-") != 4 {
		t.Errorf("NodeID does not look like UUIDv4: %q", first.NodeID)
	}

	// Second resolver, same directory, must load the same value.
	r2 := New(dir, discardLogger(t))
	second := r2.Resolve()
	if !second.Persistent || second.Source != "loaded" {
		t.Fatalf("second resolve = %+v, want persistent loaded", second)
	}
	if second.NodeID != first.NodeID {
		t.Errorf("NodeID changed across resolvers: %q vs %q", first.NodeID, second.NodeID)
	}
}

// TestResolve_LoadsCorruptedFile_FallbacksOnInvalidContent verifies the
// loose-validity gate; if the file was corrupted to non-NodeID-shaped
// content, we re-create.
func TestResolve_LoadsCorruptedFile_FallbacksOnInvalidContent(t *testing.T) {
	dir := t.TempDir()
	// Write garbage with a space inside (not allowed by looksLikeNodeID).
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("invalid id with spaces\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := New(dir, discardLogger(t))
	got := r.Resolve()
	if got.Source != "created" {
		t.Fatalf("expected re-create on garbage content, got %+v", got)
	}

	// Verify the file now contains the new value.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != got.NodeID {
		t.Errorf("file content mismatch: %q vs resolved %q", string(data), got.NodeID)
	}
}

// TestResolve_NoStateDir_Ephemeral verifies the fallback path when state
// dir is unconfigured.
func TestResolve_NoStateDir_Ephemeral(t *testing.T) {
	r := New("", discardLogger(t))
	got := r.Resolve()

	if got.Persistent {
		t.Fatalf("expected ephemeral, got %+v", got)
	}
	if got.Source != "ephemeral-hostname" && got.Source != "ephemeral-random" {
		t.Errorf("Source = %q, want one of ephemeral-*", got.Source)
	}
	if got.NodeID == "" {
		t.Fatal("empty NodeID")
	}
	if !strings.Contains(got.NodeID, "-") {
		t.Errorf("NodeID lacks suffix: %q", got.NodeID)
	}
}

// TestResolve_UnwritableStateDir_Ephemeral verifies the fallback when
// MkdirAll fails because the path points at a regular file.
func TestResolve_UnwritableStateDir_Ephemeral(t *testing.T) {
	dir := t.TempDir()
	// Place a regular file where the state dir would go.
	blockingFile := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Use a path that goes "through" that file: /<blockingFile>/state-subdir
	stateDir := filepath.Join(blockingFile, "state-subdir")

	r := New(stateDir, discardLogger(t))
	got := r.Resolve()

	if got.Persistent {
		t.Fatalf("expected ephemeral fallback, got %+v", got)
	}
}

// TestLooksLikeNodeID covers the validity gate.
func TestLooksLikeNodeID(t *testing.T) {
	cases := map[string]bool{
		"":                                       false,
		"abc":                                    true,
		"550e8400-e29b-41d4-a716-446655440000":   true,
		"node-foo-bar":                           true,
		"pod@host":                               true,
		"region/us-east/host":                    true,
		"hello world":                            false, // space
		"oops\nnewline":                          false,
		strings.Repeat("a", 257):                 false, // too long
	}
	for in, want := range cases {
		if got := looksLikeNodeID(in); got != want {
			t.Errorf("looksLikeNodeID(%q) = %v, want %v", in, got, want)
		}
	}
}
