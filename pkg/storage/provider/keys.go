// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"net/url"
	"path"
	"strings"
)

// Key hygiene shared by every backend. It lived in the S3 implementation
// until the cloud providers moved to their own modules, at which point a
// helper that local, GCS, Azure and the cleanup sweeper all call could no
// longer sit inside one of them: it belongs to the contract they share.

func NormalizeKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.ReplaceAll(key, "\\", "/")
	key = strings.TrimLeft(key, "/")
	// Collapse multiple slashes
	for strings.Contains(key, "//") {
		key = strings.ReplaceAll(key, "//", "/")
	}
	if key == "." {
		return ""
	}
	return key
}
func ValidateKey(key string) error {
	if key == "" {
		return ErrInvalidKey("empty key")
	}
	if strings.ContainsRune(key, '\x00') {
		return ErrInvalidKey("contains NUL byte")
	}
	if strings.Contains(key, "//") {
		return ErrInvalidKey("contains double slash")
	}
	if path.IsAbs(key) {
		return ErrInvalidKey("absolute paths are not allowed")
	}
	if path.Clean(key) != key {
		return ErrInvalidKey("contains non-canonical path segments")
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "." || segment == ".." {
			return ErrInvalidKey("path traversal is not allowed")
		}
	}
	return nil
}
func ValidateKeyPrefix(prefix string) error {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return nil
	}
	return ValidateKey(prefix)
}

func EscapeURLPath(key string) string {
	parts := strings.Split(key, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
