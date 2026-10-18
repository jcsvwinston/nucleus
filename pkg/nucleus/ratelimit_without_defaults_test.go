// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// NU-122 on the builder: a WithoutDefaults() application whose nucleus.yml
// sets rate_limit_requests refuses to start without WithRateLimit()
// (DEP-2026-016); with the option the limit is enforced. The refusal's
// wording is pinned in pkg/app, where it is built.
func TestBuilder_WithoutDefaults_RateLimitNeedsWithRateLimit(t *testing.T) {
	block := "rate_limit_requests: 1\nrate_limit_window: 1m\n"
	t.Run("WithoutDefaults", func(t *testing.T) {
		err := bootError(t, nucleus.New().FromConfigFile(starterConfig(t, block)).WithoutDefaults())
		if err == nil {
			t.Fatal("WithoutDefaults() without WithRateLimit() started with a declared rate limit")
		}
		for _, want := range []string{"rate_limit_requests IGNORED", "WithRateLimit()", "DEP-2026-016"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal does not say %q:\n%v", want, err)
			}
		}
	})
	t.Run("WithoutDefaults + WithRateLimit", func(t *testing.T) {
		srv := nucleustest.Start(t, nucleus.New().FromConfigFile(starterConfig(t, block)).WithoutDefaults().WithRateLimit())
		// The kit's readiness probe spends from the same bucket (same
		// client address), so count refusals rather than pin positions.
		refused := 0
		for i := 0; i < 3; i++ {
			if srv.Get("/healthz").Status == http.StatusTooManyRequests {
				refused++
			}
		}
		if refused == 0 {
			t.Fatal("WithRateLimit() mounted no limiter: rate_limit_requests: 1 refused nothing in three requests")
		}
	})
}
