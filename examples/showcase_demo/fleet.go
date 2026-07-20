//go:build fleet

package main

// The fleet leg depends on orbit/agent — a published tag, pinned in go.mod
// like every other dependency — but is only compiled in with the "fleet"
// build tag, so the default build does not carry the agent. Run it with:
//
//	ORBIT_ADMIN_ENDPOINT=http://127.0.0.1:9090 go run -tags fleet .

import (
	"os"

	orbitagent "github.com/jcsvwinston/orbit/agent"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func fleetOptions() []nucleus.Option {
	ep := os.Getenv("ORBIT_ADMIN_ENDPOINT")
	if ep == "" {
		return nil
	}
	return []nucleus.Option{nucleus.WithExtensions(
		orbitagent.NewExtension(orbitagent.ExtensionConfig{
			Endpoints: []string{ep},
		}, ".orbit-agent-state", "v1.3.3"),
	)}
}
