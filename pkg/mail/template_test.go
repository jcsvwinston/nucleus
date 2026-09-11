package mail

import (
	"strings"
	"testing"
	"testing/fstest"
)

func templateFS() fstest.MapFS {
	return fstest.MapFS{
		"verify.subject.tmpl": {Data: []byte("Confirm {{.Name}}")},
		"verify.txt.tmpl":     {Data: []byte("Hello {{.Name}}, open {{.Link}}")},
		"verify.html.tmpl":    {Data: []byte(`<p>Hello {{.Name}}, <a href="{{.Link}}">open</a></p>`)},
		"reset.subject.tmpl":  {Data: []byte("Reset")},
		"reset.txt.tmpl":      {Data: []byte("Open {{.Link}}")},
	}
}

func TestTemplates_RenderBothRepresentations(t *testing.T) {
	tmpl, err := ParseFS(templateFS())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	msg, err := tmpl.Render("verify", map[string]string{
		"Name": "Ana", "Link": "https://app.example/verify?t=abc",
	}, Message{From: "no-reply@example.test", To: []string{"ana@example.test"}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if msg.Subject != "Confirm Ana" {
		t.Errorf("subject is %q", msg.Subject)
	}
	if !strings.Contains(msg.Body, "open https://app.example/verify?t=abc") {
		t.Errorf("text body is %q", msg.Body)
	}
	if !strings.Contains(msg.HTML, `href="https://app.example/verify?t=abc"`) {
		t.Errorf("html body is %q", msg.HTML)
	}
	if msg.From != "no-reply@example.test" || len(msg.To) != 1 {
		t.Errorf("the base message was not carried through: %+v", msg)
	}
}

// A text-only template set is legitimate, and must not invent an HTML part.
func TestTemplates_TextOnly(t *testing.T) {
	tmpl, _ := ParseFS(templateFS())
	msg, err := tmpl.Render("reset", map[string]string{"Link": "https://x"}, Message{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if msg.HTML != "" {
		t.Fatalf("an HTML part appeared out of nowhere: %q", msg.HTML)
	}
}

// The HTML half goes through html/template, so a name that carries markup is
// escaped per context. This is the reason the two engines are kept apart: the
// same data through text/template would inject the script.
func TestTemplates_HTMLEscapesUntrustedData(t *testing.T) {
	tmpl, _ := ParseFS(templateFS())
	msg, err := tmpl.Render("verify", map[string]string{
		"Name": `<script>alert(1)</script>`, "Link": "https://app.example/verify",
	}, Message{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(msg.HTML, "<script>") {
		t.Fatalf("the HTML body was not escaped: %q", msg.HTML)
	}
	// The plain-text half is not markup and must NOT be mangled: text is
	// escaped by its medium, not by the template.
	if !strings.Contains(msg.Body, "<script>") {
		t.Fatalf("the text body was escaped as if it were HTML: %q", msg.Body)
	}
}

// A newline in a rendered subject is refused rather than trimmed: it is how
// header injection starts, and repairing it silently hides the input.
func TestTemplates_SubjectWithNewlineIsRefused(t *testing.T) {
	tmpl, err := ParseFS(fstest.MapFS{
		"evil.subject.tmpl": {Data: []byte("Hello {{.Name}}")},
		"evil.txt.tmpl":     {Data: []byte("body")},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := tmpl.Render("evil", map[string]string{"Name": "Ana\r\nBcc: attacker@evil.test"}, Message{}); err == nil {
		t.Fatal("a subject with a newline was accepted")
	}
}

func TestTemplates_MissingTemplateIsNamed(t *testing.T) {
	tmpl, _ := ParseFS(templateFS())
	_, err := tmpl.Render("nope", nil, Message{})
	if err == nil || !strings.Contains(err.Error(), "nope.subject.tmpl") {
		t.Fatalf("the error does not name the missing file: %v", err)
	}
}

func TestTemplates_Names(t *testing.T) {
	tmpl, _ := ParseFS(templateFS())
	names := tmpl.Names()
	if len(names) != 2 {
		t.Fatalf("expected two renderable names, got %v", names)
	}
}
