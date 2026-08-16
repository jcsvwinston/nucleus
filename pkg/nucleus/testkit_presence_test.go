// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// DX-22 (DX audit 2026-08-16): 40.7% of a real app's E2E lines (522 of
// 1281) were scaffolding, because Start() blocks and exposes no
// programmatic shutdown — every suite re-invents `go build` +
// exec.Command + /healthz polling + duplicated startServer/bgStartServer
// helpers. The framework must ship an in-process test kit: an app that
// starts and stops inside the test process, plus a token minter.
//
// This presence probe is the red half; the kit's own tests
// (pkg/nucleustest) carry the behavioral contract once it exists.
package nucleus

import (
	"os/exec"
	"testing"
)

func TestInProcessTestKitPackageExists(t *testing.T) {
	out, err := exec.Command("go", "list", "github.com/jcsvwinston/nucleus/pkg/nucleustest").CombinedOutput()
	if err != nil {
		t.Fatalf("no in-process test kit package (DX-22): %v\n%s", err, out)
	}
}
