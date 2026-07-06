// Command showcase_demo is the Quantum suite showcase: a Nucleus application
// whose domain runs on the Quark ORM, with the Orbit admin mounted on top and
// both QADR-0006 bridges wired.
//
//   - The shop module's HTTP handlers query Quark through a client wrapped with
//     orbit/quarkbridge, so every statement appears in Orbit's live SQL feed,
//     correlated to the request (Caso 1).
//   - Orbit's Data Studio is backed by orbit/quarkdatasource over the same
//     Quark models, so /admin browses and edits them (Caso 2).
//
// # Quick start (from the quantum suite workspace)
//
//	go run ./nucleus/examples/showcase_demo
//
//	curl -s localhost:8091/api/articles | jq .
//	curl -s -X POST localhost:8091/api/articles \
//	    -H 'Content-Type: application/json' \
//	    -d '{"author_id":1,"title":"probe","body":"live feed"}' | jq .
//
// Admin: http://localhost:8091/admin (user admin / password showcase-demo).
// Watch the live SQL view while hitting the API; browse Author/Article in
// Data Studio.
package main

import (
	"context"
	"log"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/orbit"
	"github.com/jcsvwinston/orbit/quarkdatasource"
	"github.com/jcsvwinston/quark"
	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/examples/showcase_demo/shop"
)

func main() {
	ctx := context.Background()

	// Quark owns the domain schema. It shares the sqlite file with the Nucleus
	// app database (which Orbit uses for the admin_users table).
	client, err := quark.New("sqlite", "showcase_demo.db")
	if err != nil {
		log.Fatalf("showcase: quark client: %v", err)
	}
	defer client.Close()

	if err := shop.Migrate(ctx, client); err != nil {
		log.Fatalf("showcase: migrate/seed: %v", err)
	}

	// Data Studio speaks Orbit's datasource contract; back it with Quark.
	ds := quarkdatasource.New(client)
	if err := quarkdatasource.Register[shop.Author](ds); err != nil {
		log.Fatalf("showcase: register Author: %v", err)
	}
	if err := quarkdatasource.Register[shop.Article](ds); err != nil {
		log.Fatalf("showcase: register Article: %v", err)
	}

	app, err := nucleus.New().
		FromConfigFile("nucleus.yaml").
		Mount(shop.Module(client)).
		Mount(orbit.Module(orbit.Config{
			Prefix:     "/admin",
			Title:      "Quantum Showcase",
			DataSource: ds,

			// Local demo credentials — change them anywhere beyond a laptop.
			BootstrapUsername: "admin",
			BootstrapEmail:    "admin@example.com",
			BootstrapPassword: "showcase-demo",
		})).
		Build()
	if err != nil {
		log.Fatalf("showcase: %v", err)
	}

	// The shop API is public in this demo: skip the framework's default-deny
	// RBAC (ADR-004). Orbit still enforces its own session auth under /admin.
	app.Options = append(app.Options, nucleus.WithOpenAuthz())

	// Optional fleet leg (build tag "fleet", suite-workspace only): attaches
	// the orbit cluster agent when ORBIT_ADMIN_ENDPOINT is set, so an admin
	// server's fleet UI shows this app as a live node with real streams and
	// host metrics. The default build has no orbit/agent dependency, keeping
	// the example resolvable from the module proxy. See fleet.go.
	app.Options = append(app.Options, fleetOptions()...)

	if err := nucleus.Run(app); err != nil {
		log.Fatalf("showcase: %v", err)
	}
}
