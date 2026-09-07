// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package knownproviders

import "sort"

// SuiteModule is a sibling product of the Quantum suite that `nucleus new
// --with` can resolve for a project: the admin panel, the ORM and the two
// bridges between them. Like Provider, the table carries the NAME and the
// `go get` target of a module and none of its code: the framework never
// requires a sibling (the suite's dependency direction is orbit → nucleus,
// quark → nothing), so the scaffold fetches them from the module proxy at
// their published tags, exactly as `nucleus add` fetches a driver.
type SuiteModule struct {
	// Name is what the person types after --with.
	Name string
	// Kind is the one-line role the module plays in an application.
	Kind string
	// Module is the `go get` target.
	Module string
	// Adds says what mounting or wiring the module gives the project.
	Adds string
	// DriverModule, when non-empty, is the pattern of the module's own
	// per-engine driver module (the ORM registers its error classifier
	// through it); the scaffold substitutes the engine directory.
	DriverModule string
}

// suiteModules lists the siblings in the order the scaffold resolves them.
// The order is not alphabetical on purpose: orbit and quark are the two
// products, the bridges depend on both, and the usage text reads the same
// way every time.
var suiteModules = []SuiteModule{
	{
		Name:   "orbit",
		Kind:   "admin panel",
		Module: "github.com/jcsvwinston/orbit",
		Adds:   "the admin panel mounted under /admin with its own login gate, Data Studio and the live observability feed",
	},
	{
		Name:         "quark",
		Kind:         "ORM",
		Module:       "github.com/jcsvwinston/quark",
		Adds:         "the Quark ORM (Active Record models, migrations from the model registry) for the application's domain",
		DriverModule: "github.com/jcsvwinston/quark/drivers/%s",
	},
	{
		Name:   "quarkbridge",
		Kind:   "observability bridge",
		Module: "github.com/jcsvwinston/orbit/quarkbridge",
		Adds:   "a Quark middleware that publishes every statement on the observability bus, so orbit's live feed shows the SQL correlated to the request",
	},
	{
		Name:   "quarkdatasource",
		Kind:   "Data Studio adapter",
		Module: "github.com/jcsvwinston/orbit/quarkdatasource",
		Adds:   "the adapter that backs orbit's Data Studio with Quark models, so /admin browses and edits them",
	},
}

// SuiteModuleByName looks a sibling up by the name --with accepts.
func SuiteModuleByName(name string) (SuiteModule, bool) {
	for _, m := range suiteModules {
		if m.Name == name {
			return m, true
		}
	}
	return SuiteModule{}, false
}

// SuiteModules returns the catalogue in resolution order.
func SuiteModules() []SuiteModule {
	out := make([]SuiteModule, len(suiteModules))
	copy(out, suiteModules)
	return out
}

// SuiteModuleNames returns the names --with accepts, sorted, for an error
// message that lists them the same way every time.
func SuiteModuleNames() []string {
	names := make([]string, 0, len(suiteModules))
	for _, m := range suiteModules {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names
}
