// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package knownproviders

import (
	"strings"
	"testing"
)

// The suite catalogue is the promise `nucleus new --with` makes: each name
// is a module the suite publishes, fetched at its published tag. A renamed
// module or a mistyped path here would surface as a `go get` failure on the
// reader's machine, not in this repository.
func TestSuiteCatalogueNamesPublishedModules(t *testing.T) {
	want := map[string]string{
		"orbit":           "github.com/jcsvwinston/orbit",
		"quark":           "github.com/jcsvwinston/quark",
		"quarkbridge":     "github.com/jcsvwinston/orbit/quarkbridge",
		"quarkdatasource": "github.com/jcsvwinston/orbit/quarkdatasource",
	}
	if got := SuiteModuleNames(); strings.Join(got, ",") != "orbit,quark,quarkbridge,quarkdatasource" {
		t.Errorf("SuiteModuleNames must list the four siblings sorted, got %v", got)
	}
	for name, module := range want {
		m, ok := SuiteModuleByName(name)
		if !ok {
			t.Errorf("%q is a suite module and must be in the catalogue", name)
			continue
		}
		if m.Module != module {
			t.Errorf("%q points at %q, want %q", name, m.Module, module)
		}
		if m.Kind == "" || m.Adds == "" {
			t.Errorf("%q must say what kind of module it is and what it adds (usage text reads them)", name)
		}
	}
	if _, ok := SuiteModuleByName("admin"); ok {
		t.Error("an unknown name must not resolve")
	}
	// The ORM ships its error classifier per engine as a driver module the
	// scaffold names by the engine directory; the products and the bridges
	// have none.
	quark, _ := SuiteModuleByName("quark")
	if !strings.Contains(quark.DriverModule, "%s") {
		t.Errorf("quark's DriverModule must carry the engine placeholder, got %q", quark.DriverModule)
	}
	for _, name := range []string{"orbit", "quarkbridge", "quarkdatasource"} {
		m, _ := SuiteModuleByName(name)
		if m.DriverModule != "" {
			t.Errorf("%q has no per-engine driver module, got %q", name, m.DriverModule)
		}
	}
	// Resolution order: the products before the bridges that depend on them.
	var order []string
	for _, m := range SuiteModules() {
		order = append(order, m.Name)
	}
	if strings.Join(order, ",") != "orbit,quark,quarkbridge,quarkdatasource" {
		t.Errorf("SuiteModules must resolve products before bridges, got %v", order)
	}
}
