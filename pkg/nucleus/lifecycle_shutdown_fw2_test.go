package nucleus

import (
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// TestLifecycleShutdownTimeout is the FW-2 guard: the app-level
// Lifecycle.OnShutdown hook must run under a bounded deadline rather than the
// previous context.Background() with no timeout. The budget mirrors pkg/app's
// withTimeoutFromConfig — prefer write_timeout, fall back to 10s — and must
// never return a non-positive duration.
func TestLifecycleShutdownTimeout(t *testing.T) {
	tests := []struct {
		name string
		core *app.App
		want time.Duration
	}{
		{
			name: "nil app falls back to 10s",
			core: nil,
			want: 10 * time.Second,
		},
		{
			name: "nil config falls back to 10s",
			core: &app.App{},
			want: 10 * time.Second,
		},
		{
			name: "zero write timeout falls back to 10s",
			core: &app.App{Config: &app.Config{WriteTimeout: 0}},
			want: 10 * time.Second,
		},
		{
			name: "negative write timeout falls back to 10s",
			core: &app.App{Config: &app.Config{WriteTimeout: -5 * time.Second}},
			want: 10 * time.Second,
		},
		{
			name: "positive write timeout is used",
			core: &app.App{Config: &app.Config{WriteTimeout: 25 * time.Second}},
			want: 25 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lifecycleShutdownTimeout(tt.core)
			if got != tt.want {
				t.Fatalf("lifecycleShutdownTimeout = %v, want %v", got, tt.want)
			}
			if got <= 0 {
				t.Fatalf("lifecycleShutdownTimeout must be positive, got %v", got)
			}
		})
	}
}
