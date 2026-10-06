// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// NU-109: the go tool ignores a go.mod at the root of the system temp
// directory ("go: warning: ignoring go.mod in system temp root"), and the
// CLI's upward search for the project took it. In any folder under $TMPDIR
// with no project of its own, a stray go.mod left at the temp root (a copy
// of orbit's, when the umbrella certified 1.39.0) made `nucleus routes`
// build a module the go tool did not consider the project, and the command
// failed with go list's error instead of stating its limit. The search now
// follows the go tool's rule, and these tests hold it to the go tool's own
// answer.
package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setSystemTempDir points os.TempDir() — for this process and for the go
// commands it starts — at dir, for the rest of the test. Nothing is ever
// written into the real temp root.
func setSystemTempDir(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("TMPDIR", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("TMP", dir)
		t.Setenv("TEMP", dir)
	}
	if filepath.Clean(os.TempDir()) != filepath.Clean(dir) {
		t.Skipf("os.TempDir() is %q here, not taken from the environment", os.TempDir())
	}
}

func writeStrayGoMod(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A copy of a real module's go.mod, the way the stray one was: the
	// module is somebody else's and its requirements are not resolvable
	// from here.
	body := "module github.com/jcsvwinston/orbit\n\ngo 1.22\n\nrequire github.com/jcsvwinston/nucleus v1.31.0\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// goToolModFile is the go.mod the go tool itself takes as the main module
// from dir (`go env GOMOD`), "" when it takes none.
func goToolModFile(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("go", "env", "GOMOD")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GO111MODULE=on", "GOTOOLCHAIN=local")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env GOMOD in %s: %v\n%s", dir, err, stderr.String())
	}
	got := strings.TrimSpace(string(out))
	if got == os.DevNull {
		return ""
	}
	return got
}

func resolved(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The search answers what the go tool answers: the nearest go.mod, unless
// it sits at the root of the system temp directory — then no project, and
// no further look up — compared through symbolic links on either side.
func TestFindModuleRootFollowsTheGoToolOnTheTempRoot(t *testing.T) {
	_, goErr := exec.LookPath("go")

	type layout struct {
		tmp     string // what TMPDIR is set to
		start   string // the --dir the search starts from
		root    string // the project the search must find; "" for none
		ignored bool   // the reason is the ignored temp-root go.mod
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, base string) layout
	}{
		{"a stray go.mod at the temp root is no project", func(t *testing.T, base string) layout {
			tmp := filepath.Join(base, "tmp")
			writeStrayGoMod(t, tmp)
			mkdirs(t, filepath.Join(tmp, "work", "a7"))
			return layout{tmp: tmp, start: filepath.Join(tmp, "work", "a7"), ignored: true}
		}},
		{"run at the temp root itself", func(t *testing.T, base string) layout {
			tmp := filepath.Join(base, "tmp")
			writeStrayGoMod(t, tmp)
			return layout{tmp: tmp, start: tmp, ignored: true}
		}},
		{"a project under the temp root is a project", func(t *testing.T, base string) layout {
			tmp := filepath.Join(base, "tmp")
			proj := filepath.Join(tmp, "proj")
			writeStrayGoMod(t, tmp)
			writeStrayGoMod(t, proj)
			mkdirs(t, filepath.Join(proj, "cmd", "app"))
			return layout{tmp: tmp, start: filepath.Join(proj, "cmd", "app"), root: proj}
		}},
		{"the ignored temp-root go.mod stops the search", func(t *testing.T, base string) layout {
			tmp := filepath.Join(base, "tmp")
			writeStrayGoMod(t, base)
			writeStrayGoMod(t, tmp)
			mkdirs(t, filepath.Join(tmp, "sub"))
			return layout{tmp: tmp, start: filepath.Join(tmp, "sub"), ignored: true}
		}},
		{"a go.mod above an empty temp root is the project", func(t *testing.T, base string) layout {
			tmp := filepath.Join(base, "tmp")
			writeStrayGoMod(t, base)
			mkdirs(t, filepath.Join(tmp, "sub"))
			return layout{tmp: tmp, start: filepath.Join(tmp, "sub"), root: base}
		}},
		{"a temp dir written with a trailing separator", func(t *testing.T, base string) layout {
			tmp := filepath.Join(base, "tmp")
			writeStrayGoMod(t, tmp)
			mkdirs(t, filepath.Join(tmp, "sub"))
			return layout{tmp: tmp + string(filepath.Separator), start: filepath.Join(tmp, "sub"), ignored: true}
		}},
		{"a temp dir that is a link, searched from its target", func(t *testing.T, base string) layout {
			real, link := filepath.Join(base, "real"), filepath.Join(base, "link")
			writeStrayGoMod(t, real)
			mkdirs(t, filepath.Join(real, "sub"))
			if err := os.Symlink(real, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return layout{tmp: link, start: filepath.Join(real, "sub"), ignored: true}
		}},
		{"a temp dir searched through a link to it", func(t *testing.T, base string) layout {
			real, link := filepath.Join(base, "real"), filepath.Join(base, "link")
			writeStrayGoMod(t, real)
			mkdirs(t, filepath.Join(real, "sub"))
			if err := os.Symlink(real, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return layout{tmp: real, start: filepath.Join(link, "sub"), ignored: true}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The physical path, so the link cases control every link
			// involved (macOS's own /var -> /private/var included).
			base := resolved(t, t.TempDir())
			l := tc.setup(t, base)
			setSystemTempDir(t, l.tmp)

			root, outside := findModuleRoot(l.start)
			switch {
			case l.root != "":
				if outside != "" || resolved(t, root) != resolved(t, l.root) {
					t.Fatalf("findModuleRoot(%s) = %q, %q; want the project %s", l.start, root, outside, l.root)
				}
			case l.ignored:
				want := "ignoring go.mod in system temp root " + os.TempDir() + ", as the go tool does"
				if root != "" || outside != want {
					t.Fatalf("findModuleRoot(%s) = %q, %q; want no project: %q", l.start, root, outside, want)
				}
			}

			if goErr != nil {
				return
			}
			gomod := goToolModFile(t, l.start)
			if l.root == "" {
				if gomod != "" {
					t.Fatalf("the go tool takes %s from %s; the search says there is no project (%s)", gomod, l.start, outside)
				}
				return
			}
			if gomod == "" || resolved(t, filepath.Dir(gomod)) != resolved(t, root) {
				t.Fatalf("the go tool takes %q from %s; the search found %s", gomod, l.start, root)
			}
		})
	}
}

// The regression as the umbrella's exit-0 guard met it (A7): a bare
// `nucleus routes` in a folder under $TMPDIR with no project of its own and
// a stray go.mod at the temp root. It must state its limit exactly as it
// does with no go.mod at all — exit 0, the same listing, the same note —
// naming the ignored go.mod as the reason, and never reach go list.
func TestRoutesIgnoresAGoModAtTheSystemTempRoot(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "tmp")
	work := filepath.Join(tmp, "work", "a7")
	mkdirs(t, work)
	setSystemTempDir(t, tmp)
	t.Setenv("NUCLEUS_CONFIG", "")
	t.Chdir(work)
	if root, _ := findModuleRoot("."); root != "" {
		t.Skipf("%s holds a go.mod above the test's directories: the folder is inside a project even without the stray one", root)
	}

	run := func() (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"routes"}, strings.NewReader(""), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}

	code, without, errOut := run()
	if code != 0 {
		t.Fatalf("routes with no go.mod at all exited %d: %s", code, errOut)
	}

	writeStrayGoMod(t, tmp)
	code, stray, errOut := run()
	if code != 0 {
		t.Fatalf("routes under a stray go.mod at the temp root exited %d, want 0 like with no go.mod:\nstdout: %s\nstderr: %s", code, stray, errOut)
	}
	reason := "ignoring go.mod in system temp root " + os.TempDir() + ", as the go tool does"
	if !strings.Contains(stray, "NOTE: "+reason+": listing framework-owned routes only.") {
		t.Errorf("the note must name the ignored go.mod as the reason:\n%s", stray)
	}
	assertGuardPhrase(t, stray)
	if got := strings.Replace(stray, reason, "no go.mod at or above .", 1); got != without {
		t.Errorf("apart from its reason, the output must be the one with no go.mod at all:\n--- stray go.mod ---\n%s\n--- no go.mod ---\n%s", stray, without)
	}
}

// `nucleus dev` refuses to run outside a project, and a stray go.mod at the
// temp root does not make one.
func TestDevIgnoresAGoModAtTheSystemTempRoot(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "tmp")
	work := filepath.Join(tmp, "work")
	writeStrayGoMod(t, tmp)
	mkdirs(t, work)
	setSystemTempDir(t, tmp)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"dev", "--dir", work}, strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatalf("dev under a stray go.mod at the temp root succeeded, want the no-project error")
	}
	want := "ignoring go.mod in system temp root " + os.TempDir() + ", as the go tool does: dev builds and runs the main package of a Go project"
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("dev must refuse with the no-project error naming the ignored go.mod, got:\n%s", stderr.String())
	}
}
