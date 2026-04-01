package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestBuildGoTestArgs_Defaults(t *testing.T) {
	args, err := buildGoTestArgs([]string{"./..."}, goTestOptions{count: 1})
	if err != nil {
		t.Fatalf("buildGoTestArgs failed: %v", err)
	}

	got := strings.Join(args, " ")
	want := "test ./..."
	if got != want {
		t.Fatalf("unexpected args: got %q want %q", got, want)
	}
}

func TestBuildGoTestArgs_WithFlags(t *testing.T) {
	args, err := buildGoTestArgs([]string{"./pkg/router"}, goTestOptions{
		run:      "TestRouter",
		count:    2,
		race:     true,
		verbose:  true,
		failfast: true,
		cover:    true,
		timeout:  90 * time.Second,
	})
	if err != nil {
		t.Fatalf("buildGoTestArgs failed: %v", err)
	}

	got := strings.Join(args, " ")
	want := "test -run TestRouter -count 2 -race -v -failfast -cover -timeout 1m30s ./pkg/router"
	if got != want {
		t.Fatalf("unexpected args: got %q want %q", got, want)
	}
}

func TestBuildGoTestArgs_Validations(t *testing.T) {
	if _, err := buildGoTestArgs(nil, goTestOptions{count: 1}); err == nil {
		t.Fatal("expected error when no package is provided")
	}
	if _, err := buildGoTestArgs([]string{"./..."}, goTestOptions{count: 0}); err == nil {
		t.Fatal("expected error when count <= 0")
	}
	if _, err := buildGoTestArgs([]string{"./..."}, goTestOptions{count: 1, timeout: -1 * time.Second}); err == nil {
		t.Fatal("expected error when timeout is negative")
	}
}

func TestRunTest_DryRun(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	err := runTest(
		[]string{"--dry-run", "--run", "TestRun", "--count", "3", "./cmd/goframe"},
		strings.NewReader(""),
		&out,
		&errOut,
	)
	if err != nil {
		t.Fatalf("runTest dry-run failed: %v (stderr=%s)", err, errOut.String())
	}
	if !strings.Contains(out.String(), "go test -run TestRun -count 3 ./cmd/goframe") {
		t.Fatalf("unexpected dry-run output: %s", out.String())
	}
}
