package nucleus

import (
	"reflect"
	"testing"
)

// TestResolveModulePolicyObjects_RootEmitsBothSpellings closes a row that no
// request could ever match (QCD-FW-19).
//
// The godoc separates two spellings by intent: `Object ""` is "a module whose
// root is public while its subtree is not". That half did not work:
//
//	Object=""   GET /ns7 → 307 Location="/ns7/" → 403   Can(/ns7)=true Can(/ns7/)=false
//	Object="/"  GET /ns7 → 307 Location="/ns7/" → 200
//
// `""` emitted the exact `<prefix>` row, but the only way to reach a module's
// root over HTTP is `<prefix>/` — net/http's mux issues an unconditional 307
// to the trailing-slash form — and that spelling was deliberately excluded.
// The row was dead on arrival, and the intent it expressed was not reachable
// by any other spelling.
//
// Both spellings of the exact root, still no subtree: that is what keeps ""
// different from "/".
func TestResolveModulePolicyObjects_RootEmitsBothSpellings(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		object string
		want   []string
	}{
		{"root only: both spellings, no subtree", "/ns7", "", []string{"/ns7", "/ns7/"}},
		{"root only: trailing slash in prefix", "/ns7/", "", []string{"/ns7", "/ns7/"}},
		{"whole surface still adds the subtree", "/ns7", "/", []string{"/ns7", "/ns7/*"}},
		{"an explicit path is untouched", "/ns7", "/notes", []string{"/ns7/notes"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveModulePolicyObjects(tc.prefix, tc.object)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("resolveModulePolicyObjects(%q, %q) = %v, want %v", tc.prefix, tc.object, got, tc.want)
			}
		})
	}

	// The two spellings must stay DIFFERENT intents: "" must never emit the
	// subtree wildcard, or declaring a public root would silently publish
	// everything under it.
	for _, prefix := range []string{"/ns7", "/a/b"} {
		for _, o := range resolveModulePolicyObjects(prefix, "") {
			if len(o) > 1 && o[len(o)-1] == '*' {
				t.Errorf("Object \"\" emitted a subtree row %q for prefix %q", o, prefix)
			}
		}
	}
}
