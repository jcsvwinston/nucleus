// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package accounts_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/accounts"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// NU-126: an application built WithoutDefaults() discards the rows its
// modules declare and says so when that leaves a route open. The account
// flows grant the anonymous subject every action on every route they serve —
// someone registering has no identity yet — so in the api starter's shape
// nothing is lost, and the boot log must not claim otherwise.
func TestFromRuntime_WithoutDefaults_NoDiscardedPoliciesLine(t *testing.T) {
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() {
			os.Stdout = orig
			_ = w.Close()
		}()
		nucleustest.Start(t, nucleus.New().
			FromConfigFile(runtimeConfig(t, "")).
			WithoutDefaults().
			WithMail().
			Mount(accounts.FromRuntime()))
	}()
	if out := <-done; strings.Contains(out, "module policies DISCARDED") {
		t.Fatalf("the account flows open every route they serve to anonymous callers, and the boot log reports their rows as a loss:\n%s", out)
	}
}
