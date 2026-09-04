// Command mvc_api is the Nucleus mvc_api reference application.
//
// It demonstrates the canonical three-surface fluent builder pattern
// with a single REST resource: notes.
//
// # Quick start
//
//	cd examples/mvc_api
//
//	# 1. Run migrations (creates the notes table in examples_mvc_api.db)
//	nucleus migrate --config config/nucleus.yaml --migrations migrations up
//
//	# 2. Start the server
//	go run .
//
//	# 3. Try the API
//	curl -s http://localhost:8090/notes | jq .
//	curl -s -X POST http://localhost:8090/notes \
//	    -H 'Content-Type: application/json' \
//	    -d '{"title":"hello","body":"world"}' | jq .
package main

import (
	"log"

	"github.com/jcsvwinston/nucleus/examples/mvc_api/internal/notes"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"

	// The framework links no database driver: each ships as its own module
	// (ADR-031) and the application imports the one it uses, the way
	// database/sql drivers have always been wired. Drop this line and the
	// build still succeeds — startup then stops with the line to add back.
	_ "github.com/jcsvwinston/nucleus/drivers/sqlite"
)

func main() {
	err := nucleus.New().
		FromConfigFile("config/nucleus.yaml").
		// No WithoutDefaults() here (DX-11): this example is the model the
		// quickstart tells you to copy onto the mvc scaffold, so it runs
		// with the same barriers the scaffold turns on — default-deny authz
		// and CSRF. Its config supplies the policy rows and the CSRF
		// exemption; copy those too if you write your own config.
		Mount(notes.Module()).
		Start()
	if err != nil {
		log.Fatalf("mvc_api: %v", err)
	}
}
