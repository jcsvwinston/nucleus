// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package knownproviders

import "sort"

// The suite products are the catalog rows that ship InSuite: the admin
// panel, the ORM and the two bridges between them. Like every other row, the
// table carries the NAME and the `go get` target of a module and none of its
// code: the framework never requires a sibling (the suite's dependency
// direction is orbit → nucleus, quark → nothing).

// SuiteModuleByName looks a suite product up by its catalog name.
func SuiteModuleByName(name string) (SuiteModule, bool) {
	for _, e := range catalog {
		if e.Ships == InSuite && e.Name == name {
			return e, true
		}
	}
	return SuiteModule{}, false
}

// SuiteModules returns the suite products in resolution order: the products
// before the bridges that depend on both.
func SuiteModules() []SuiteModule {
	var out []SuiteModule
	for _, e := range catalog {
		if e.Ships == InSuite {
			out = append(out, e)
		}
	}
	return out
}

// SuiteModuleNames returns the suite products' names, sorted.
func SuiteModuleNames() []string {
	var names []string
	for _, e := range SuiteModules() {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	return names
}
