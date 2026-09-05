// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"strings"
	"testing"
)

// NU-32: two modules registering the same route used to crash the process
// with the ServeMux panic; mountModule now returns it as an error.
func TestMountModule_DuplicateRouteIsAnError(t *testing.T) {
	core, _ := newMountCore("production")
	spec := Module[struct{}]{
		Name:   "blog",
		Routes: func(r Router, _ struct{}) { r.Get("/blogs", nopHandler) },
	}.Build()
	if err := mountModule(core, spec, nil); err != nil {
		t.Fatalf("first mount: %v", err)
	}
	twin := Module[struct{}]{
		Name:   "blog-twin",
		Routes: func(r Router, _ struct{}) { r.Get("/blogs", nopHandler) },
	}.Build()
	err := mountModule(core, twin, nil)
	if err == nil || !strings.Contains(err.Error(), `mounting module "blog-twin"`) {
		t.Fatalf("duplicate route: err=%v, want the mounting error naming the module", err)
	}
}
