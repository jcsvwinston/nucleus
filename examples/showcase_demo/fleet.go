//go:build fleet

package main

// The fleet leg depends on orbit/agent (and transitively orbit/proto), which
// have no published tags yet — so it only resolves inside the Quantum suite
// workspace. Run it with:
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
		}, ".orbit-agent-state", "v0.10.0"),
	)}
}
