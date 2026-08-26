// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

// Arco B: la superficie que una extensión puede tocar deja de ser un
// cheque en blanco.
//
// app.Extension receives the whole *App, and its documentation used to say
// an extension "may set fields on App". That is power with no contract:
// whatever an extension reached for became API in practice, while being
// covered by nothing that could be promised across versions. An ecosystem
// cannot be built on that — a plugin author needs to know which ground
// will still be there next minor.
//
// This freezes the App fields an extension may rely on. Adding one is an
// additive, deliberate act; removing one is a break that has to be seen.
// It is the same mechanism as the API baseline, aimed at the surface the
// API baseline does not describe.
package contracts

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func TestExtensionSurface_Frozen(t *testing.T) {
	current := exportedAppFields()

	path := filepath.Join("baseline", "extension_surface.txt")
	if os.Getenv("NUCLEUS_UPDATE_CONTRACT_BASELINE") == "1" {
		if err := os.WriteFile(path, []byte(strings.Join(current, "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("write baseline: %v", err)
		}
		t.Logf("extension surface baseline rewritten (%d fields)", len(current))
		return
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read baseline (regenerate with NUCLEUS_UPDATE_CONTRACT_BASELINE=1): %v", err)
	}
	baseline := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			baseline = append(baseline, line)
		}
	}

	inCurrent := map[string]struct{}{}
	for _, f := range current {
		inCurrent[f] = struct{}{}
	}
	var removed []string
	for _, f := range baseline {
		if _, ok := inCurrent[f]; !ok {
			removed = append(removed, f)
		}
	}
	if len(removed) > 0 {
		t.Fatalf("extension-facing App fields REMOVED: %s\n"+
			"Every extension that reads one of these breaks. If the removal is intended, "+
			"it needs a deprecation cycle first, and then the baseline regenerated in the same change.",
			strings.Join(removed, ", "))
	}

	inBaseline := map[string]struct{}{}
	for _, f := range baseline {
		inBaseline[f] = struct{}{}
	}
	var added []string
	for _, f := range current {
		if _, ok := inBaseline[f]; !ok {
			added = append(added, f)
		}
	}
	if len(added) > 0 {
		t.Fatalf("extension-facing App fields ADDED without updating the baseline: %s\n"+
			"An addition is fine — it is a promise to plugin authors, so it is made deliberately: "+
			"regenerate with NUCLEUS_UPDATE_CONTRACT_BASELINE=1 go test ./contracts/ -run ExtensionSurface",
			strings.Join(added, ", "))
	}
}

func exportedAppFields() []string {
	t := reflect.TypeOf(app.App{})
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported: not reachable by an extension
		}
		out = append(out, f.Name+" "+f.Type.String())
	}
	sort.Strings(out)
	return out
}
