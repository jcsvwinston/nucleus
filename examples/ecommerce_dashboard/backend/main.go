package main

import (
	"fmt"

	"github.com/jcsvwinston/nucleus/examples/ecommerce_dashboard/backend/seed"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// main demonstrates the ADR-010 fluent surface for pkg/nucleus.
//
// The application configuration lives in a typed `nucleus.App` value
// (here built inline; in larger apps it would come from
// `nucleus.New().FromConfigFile("config.yaml")` or from a
// user-authored `bootstrap.New()` helper). The Module is defined in
// module.go and mounted as a ModuleSpec. The SPA fallback wires the
// frontend bundle once the API routes are registered.
func main() {
	seed.Database()

	err := nucleus.New().
		FromStruct(nucleus.App{
			Config: app.Config{
				Host: "127.0.0.1",
				Port: 8080,
				Databases: map[string]app.DatabaseConfig{
					"default": {URL: "sqlite://ecommerce.db"},
				},
			},
			SPA: nucleus.SPAConfig{
				Dir:       "../frontend/dist",
				IndexFile: "index.html",
				APIPrefix: "/api",
			},
		}).
		Mount(Module.Build()).
		Start()

	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
