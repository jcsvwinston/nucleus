package auth

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	password := "supersecret123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash == "" {
		t.Fatal("hash should not be empty")
	}
	if hash == password {
		t.Fatal("hash should not equal plaintext")
	}

	if !CheckPassword(password, hash) {
		t.Error("CheckPassword should return true for correct password")
	}
	if CheckPassword("wrongpassword", hash) {
		t.Error("CheckPassword should return false for wrong password")
	}
}

func TestCheckPassword_InvalidHash(t *testing.T) {
	if CheckPassword("anything", "not-a-valid-hash") {
		t.Error("should return false for invalid hash")
	}
}
