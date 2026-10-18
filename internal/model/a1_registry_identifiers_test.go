// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"errors"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/dupfixture"
)

// Product collides by name with dupfixture.Product: the registry keys by
// the type's name, which is what two modules' models share.
type Product struct {
	ID   int    `db:"id" json:"id"`
	Name string `db:"name" json:"name"`
}

// NU-20: a second, different type under a name the registry holds is an
// error; the same type again is an allowed override.
func TestRegistry_DuplicateNameIsAnError(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&Product{}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(&Product{}, ModelConfig{PageSize: 50}); err != nil {
		t.Fatalf("re-registering the same type must be allowed: %v", err)
	}
	err := r.Register(&dupfixture.Product{})
	if !errors.Is(err, ErrDuplicateModelName) {
		t.Fatalf("a different type under the same name: err=%v, want ErrDuplicateModelName", err)
	}
}

// NU-39: a column or index name is an identifier; only a table may be
// schema-qualified; Oracle reserved words are quoted at the choke point.
func TestIdentifiers_SplitAndOracleReserved(t *testing.T) {
	for _, ok := range []string{"orders", "order_items", "Ünïcode1"} {
		if !isValidIdentifier(ok) {
			t.Errorf("identifier %q rejected", ok)
		}
	}
	for _, bad := range []string{"", "billing.orders", "a-b", "a b", "x;"} {
		if isValidIdentifier(bad) {
			t.Errorf("identifier %q accepted", bad)
		}
	}
	if !isValidTableRef("billing.orders") || isValidTableRef("a.b.c") || isValidTableRef(".orders") {
		t.Errorf("table ref: one dot, both halves identifiers")
	}
	if got := oracleIdentifier("comment"); got != `"COMMENT"` {
		t.Errorf("oracleIdentifier(comment) = %s, want the quoted upper-case spelling", got)
	}
	if got := oracleIdentifier("customer_id"); got != "customer_id" {
		t.Errorf("oracleIdentifier(customer_id) = %s, want verbatim", got)
	}
}
