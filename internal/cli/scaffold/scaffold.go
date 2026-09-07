// Package scaffold renders the `nucleus new` starter project from a tree of
// embedded template files instead of inline Go string literals.
//
// Templates live under templates/ in two layers:
//
//   - templates/_common/ — files shared by every starter template.
//   - templates/<name>/  — files specific to one template (api, mvc, suite).
//
// The path of a template file UNDER its layer directory mirrors its path in
// the generated project. A template layer may carry a file the _common layer
// also has (the suite's README is not the empty skeleton's): the template's
// copy wins, and the project gets one file at that path. Go source files carry a ".go.tmpl" suffix (and other
// rendered files a ".tmpl" suffix) so the Go toolchain ignores them in this
// module; the suffix is stripped on render. Files with a real extension and no
// ".tmpl" suffix (e.g. .gitignore, home.html, *.sql, *.csv) are copied
// verbatim and never run through text/template, so literal "{{ ... }}"
// sequences (such as the HTML home page) survive intact.
package scaffold

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"text/template"
)

//go:embed all:templates
var templatesFS embed.FS

// TemplateData carries the values interpolated into rendered templates via the
// placeholders {{.Module}}, {{.ProjectName}}, {{.Port}}, {{.FrameworkVersion}},
// {{.GoVersion}}, {{.Toolchain}}, {{.Database}}, {{.DatabaseURL}},
// {{.DriverModule}}, {{.QuarkDriver}}, {{.QuarkDSN}}, {{.QuarkDriverModule}}
// and the {{.With}} list a template asks about with {{if .Has "orbit"}}.
type TemplateData struct {
	Module           string
	ProjectName      string
	Port             int
	FrameworkVersion string
	// GoVersion is the `go` directive written into the generated go.mod
	// (e.g. "1.26.4"). Toolchain is the `toolchain` directive ("" omits the
	// line, the current state). Both track the framework's own go.mod; see
	// resolveGoDirectives in internal/cli/new.go (enforced by
	// TestScaffoldGoDirectivesTrackGoMod).
	GoVersion string
	Toolchain string
	// Database is the engine name the project starts on ("sqlite",
	// "postgres", …), DatabaseURL the databases.default.url written into
	// nucleus.yml, and DriverModule the driver module main.go imports for
	// it. See scaffoldDatabases in internal/cli/new.go.
	Database     string
	DatabaseURL  string
	DriverModule string
	// QuarkDriver is the database/sql driver name the Quark ORM opens the
	// same engine with, QuarkDSN the data source it takes (a file for
	// sqlite, a URL or DSN for the servers) and QuarkDriverModule the
	// Quark driver module that registers the engine's error classifier.
	// Only the suite template and `--with quark` read them.
	QuarkDriver       string
	QuarkDSN          string
	QuarkDriverModule string
	// With lists the suite modules the project was scaffolded with (the
	// names `--with` accepts: orbit, quark, quarkbridge, quarkdatasource),
	// in catalogue order. Templates branch on it through Has.
	With []string
}

// Has reports whether the project was scaffolded with the named suite
// module, for template conditionals such as {{if .Has "orbit"}}.
func (d TemplateData) Has(name string) bool {
	for _, w := range d.With {
		if w == name {
			return true
		}
	}
	return false
}

// File is a single rendered output: a slash-separated path relative to the
// project root and the rendered body. Callers convert RelPath to an OS path.
type File struct {
	RelPath string
	Body    string
}

const (
	commonLayer = "_common"
	rootDir     = "templates"
	tmplSuffix  = ".tmpl"
)

// Render walks templates/_common then templates/<tmpl>, executing every
// ".tmpl" file through text/template and copying every other file verbatim.
// The returned files use slash-separated, project-relative paths with the
// ".tmpl" suffix stripped. The _common layer is emitted before the
// template-specific layer so callers preserve a deterministic order.
func Render(tmpl string, data TemplateData) ([]File, error) {
	tmpl = strings.TrimSpace(tmpl)
	// Allow-list of selectable templates. Adding a new starter template means
	// adding both its templates/<name>/ tree AND a case here; this also rejects
	// the empty name and the _common layer without special-casing them.
	switch tmpl {
	case "api", "mvc", "suite":
		// selectable
	default:
		return nil, fmt.Errorf("scaffold: unknown template %q (supported: api, mvc, suite)", tmpl)
	}

	var files []File
	for _, layer := range []string{commonLayer, tmpl} {
		layerFiles, err := renderLayer(layer, data)
		if err != nil {
			return nil, err
		}
		files = mergeLayer(files, layerFiles)
	}
	return files, nil
}

// mergeLayer appends the files of a later layer, replacing in place any
// file an earlier layer already produced at the same path, so a template
// can override a _common file and the output order stays deterministic.
func mergeLayer(files, layer []File) []File {
	index := make(map[string]int, len(files))
	for i, f := range files {
		index[f.RelPath] = i
	}
	for _, f := range layer {
		if i, ok := index[f.RelPath]; ok {
			files[i] = f
			continue
		}
		index[f.RelPath] = len(files)
		files = append(files, f)
	}
	return files
}

func renderLayer(layer string, data TemplateData) ([]File, error) {
	base := path.Join(rootDir, layer)
	if _, err := fs.Stat(templatesFS, base); err != nil {
		return nil, fmt.Errorf("scaffold: template layer %q not found: %w", layer, err)
	}

	var files []File
	walkErr := fs.WalkDir(templatesFS, base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		raw, readErr := templatesFS.ReadFile(p)
		if readErr != nil {
			return fmt.Errorf("scaffold: read %s: %w", p, readErr)
		}

		// rel is the path inside the layer, e.g. "internal/models/article.go.tmpl".
		// embed.FS always uses forward slashes, and p is always under base (from
		// WalkDir), so a slash-only TrimPrefix is correct and OS-independent.
		rel := strings.TrimPrefix(p, base+"/")

		body := string(raw)
		if strings.HasSuffix(rel, tmplSuffix) {
			rendered, execErr := execTemplate(p, body, data)
			if execErr != nil {
				return execErr
			}
			body = rendered
			rel = strings.TrimSuffix(rel, tmplSuffix)
		}

		files = append(files, File{RelPath: rel, Body: body})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return files, nil
}

func execTemplate(name, body string, data TemplateData) (string, error) {
	t, err := template.New(name).Option("missingkey=error").Parse(body)
	if err != nil {
		return "", fmt.Errorf("scaffold: parse %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("scaffold: execute %s: %w", name, err)
	}
	return buf.String(), nil
}
