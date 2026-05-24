// Command mvc_api is the Nucleus mvc_api reference application.
//
// It demonstrates the canonical three-surface fluent builder pattern
// (ADR-010 Phase 4, Slice 1) with a single REST resource: notes.
//
// # Quick start
//
//	# 1. Run migrations (creates the notes table in examples_mvc_api.db)
//	go run ./cmd/nucleus migrate --config examples/mvc_api/config/nucleus.yaml \
//	    --migrations examples/mvc_api/migrations up
//
//	# 2. Start the server
//	go run ./examples/mvc_api
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
)

func main() {
	err := nucleus.New().
		FromConfigFile("examples/mvc_api/config/nucleus.yaml").
		WithoutDefaults(). // lightweight: no admin, storage, mail, authz
		Mount(notes.Module()).
		Start()
	if err != nil {
		log.Fatalf("mvc_api: %v", err)
	}
}
