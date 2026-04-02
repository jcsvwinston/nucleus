package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMessageExtensions(t *testing.T) {
	exts, err := parseMessageExtensions(".go,html,.templ")
	if err != nil {
		t.Fatalf("parseMessageExtensions failed: %v", err)
	}
	if _, ok := exts[".go"]; !ok {
		t.Fatal("expected .go extension")
	}
	if _, ok := exts[".html"]; !ok {
		t.Fatal("expected .html extension")
	}
	if _, ok := exts[".templ"]; !ok {
		t.Fatal("expected .templ extension")
	}
}

func TestExtractMessageHits(t *testing.T) {
	content := `
package demo

func handler() {
	_ = T("Welcome")
	_ = _("Goodbye")
}

// template-like usage
// {{ trans "From template" }}
`
	hits := extractMessageHits(content, "demo.go")
	if len(hits) != 3 {
		t.Fatalf("expected 3 hits, got %d", len(hits))
	}

	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	joined := strings.Join(ids, ",")
	if !strings.Contains(joined, "Welcome") || !strings.Contains(joined, "Goodbye") || !strings.Contains(joined, "From template") {
		t.Fatalf("unexpected extracted ids: %s", joined)
	}
}

func TestCollectMessageCatalog(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "internal", "web", "handler.go"), `package web
func f() {
	_ = T("One")
	_ = _("Two")
}`)
	writeTestFile(t, filepath.Join(dir, "internal", "web", "view.html"), `{{ trans "Three" }}`)

	exts, err := parseMessageExtensions(".go,.html")
	if err != nil {
		t.Fatalf("parseMessageExtensions failed: %v", err)
	}
	entries, files, err := collectMessageCatalog(dir, exts)
	if err != nil {
		t.Fatalf("collectMessageCatalog failed: %v", err)
	}
	if files != 2 {
		t.Fatalf("expected 2 scanned files, got %d", files)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 message entries, got %d", len(entries))
	}
}

func TestParsePOCatalogAndBuildCompiledMessages(t *testing.T) {
	raw := []byte(`# header
msgid ""
msgstr ""
"Language: es\n"

msgid "Welcome"
msgstr "Bienvenido"

msgid "Goodbye"
msgstr ""
`)
	entries, err := parsePOCatalog(raw)
	if err != nil {
		t.Fatalf("parsePOCatalog failed: %v", err)
	}
	if got := entries["Welcome"]; got != "Bienvenido" {
		t.Fatalf("unexpected translated value: %q", got)
	}
	if got := entries["Goodbye"]; got != "" {
		t.Fatalf("expected empty msgstr for Goodbye, got %q", got)
	}

	compiled := buildCompiledMessages("es", "messages", entries)
	if compiled.Entries["Welcome"] != "Bienvenido" {
		t.Fatalf("unexpected compiled Welcome: %q", compiled.Entries["Welcome"])
	}
	if compiled.Entries["Goodbye"] != "Goodbye" {
		t.Fatalf("expected fallback to msgid for Goodbye, got %q", compiled.Entries["Goodbye"])
	}
}

func TestDiscoverCompileMessageJobs(t *testing.T) {
	dir := t.TempDir()
	poPath := filepath.Join(dir, "es", "LC_MESSAGES", "messages.po")
	writeTestFile(t, poPath, `msgid "a"
msgstr "b"
`)

	jobs, err := discoverCompileMessageJobs(dir, "", "messages")
	if err != nil {
		t.Fatalf("discoverCompileMessageJobs failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].locale != "es" {
		t.Fatalf("unexpected locale: %s", jobs[0].locale)
	}
	if !strings.HasSuffix(jobs[0].jsonPath, "messages.json") {
		t.Fatalf("unexpected json output path: %s", jobs[0].jsonPath)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}
