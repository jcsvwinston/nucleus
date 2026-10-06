// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"net/http"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// NU-122 on the builder: the api starter's shape with rate_limit_requests
// set and no WithRateLimit still starts — what booted before keeps booting
// until the major (QADR-0010) — and refuses nothing; with WithRateLimit the
// limit is enforced. The ERROR line the first logs is pinned in pkg/app,
// where the logger is.
func TestBuilder_WithoutDefaults_RateLimitNeedsWithRateLimit(t *testing.T) {
	for name, withOption := range map[string]bool{
		"WithoutDefaults":                 false,
		"WithoutDefaults + WithRateLimit": true,
	} {
		t.Run(name, func(t *testing.T) {
			b := nucleus.New().
				FromConfigFile(starterConfig(t, "rate_limit_requests: 1\nrate_limit_window: 1m\n")).
				WithoutDefaults()
			if withOption {
				b = b.WithRateLimit()
			}
			srv := nucleustest.Start(t, b)
			// The kit's readiness probe spends from the same bucket (same
			// client address), so count refusals rather than pin positions.
			refused := 0
			for i := 0; i < 3; i++ {
				if srv.Get("/healthz").Status == http.StatusTooManyRequests {
					refused++
				}
			}
			if withOption && refused == 0 {
				t.Fatal("WithRateLimit() mounted no limiter: rate_limit_requests: 1 refused nothing in three requests")
			}
			if !withOption && refused > 0 {
				t.Fatalf("WithoutDefaults() without WithRateLimit() refused %d requests: the limit is enforced after all", refused)
			}
		})
	}
}
