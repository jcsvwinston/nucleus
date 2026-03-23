// Package auth provides authentication utilities for the GoFrame framework,
// including password hashing, JWT management, and session handling.
package auth

import "golang.org/x/crypto/bcrypt"

const bcryptCost = 12

// HashPassword generates a bcrypt hash of the given password with cost 12.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
// Returns true if they match, false otherwise (including on malformed hashes).
func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
