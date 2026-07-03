module github.com/jcsvwinston/nucleus/examples/showcase_demo

go 1.26.4

// The Quantum suite showcase: a Nucleus app whose domain runs on the Quark ORM,
// with the Orbit admin mounted on top — both integration bridges wired
// (quantum QADR-0006):
//   - orbit/quarkbridge publishes Quark's SQL onto the observability bus, so
//     Orbit's live feed shows it correlated to the request (Caso 1);
//   - orbit/quarkdatasource backs Data Studio with the Quark models (Caso 2).
//
// This example has its OWN module on purpose: Quark and Orbit must never enter
// the framework module's dependency graph.
//
// BUILD IT FROM THE QUANTUM SUITE WORKSPACE (quantum/go.work), which resolves
// all five modules locally — that is also how the suite's integration CI
// exercises it (Fase 4). Standalone module-proxy resolution needs Orbit's next
// tag (orbit/quarkdatasource pins its parent via an intra-repo replace that
// does not propagate to consumers); it unlocks with release-please in Fase 3.
require (
	github.com/jcsvwinston/nucleus v0.9.1-0.20260701170204-a46fad0ec1e3
	github.com/jcsvwinston/orbit v0.1.1-0.20260702220153-728c79ee79e0
	github.com/jcsvwinston/orbit/quarkbridge v0.0.0-20260702220153-728c79ee79e0
	github.com/jcsvwinston/orbit/quarkdatasource v0.0.0-20260702220153-728c79ee79e0
	github.com/jcsvwinston/quark v1.1.5
	modernc.org/sqlite v1.50.0
)
