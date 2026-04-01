package cli

import "testing"

func TestResolveChangePasswordUsername(t *testing.T) {
	username, err := resolveChangePasswordUsername("", []string{"admin"})
	if err != nil {
		t.Fatalf("resolve positional username failed: %v", err)
	}
	if username != "admin" {
		t.Fatalf("unexpected username: %q", username)
	}

	username, err = resolveChangePasswordUsername("root", nil)
	if err != nil {
		t.Fatalf("resolve flag username failed: %v", err)
	}
	if username != "root" {
		t.Fatalf("unexpected username: %q", username)
	}
}

func TestResolveChangePasswordUsernameErrors(t *testing.T) {
	if _, err := resolveChangePasswordUsername("", nil); err == nil {
		t.Fatal("expected error when username is missing")
	}
	if _, err := resolveChangePasswordUsername("admin", []string{"other"}); err == nil {
		t.Fatal("expected error when both positional and --username are provided")
	}
}
