package mail

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"strings"
	texttemplate "text/template"
)

// Templates renders messages from a set of named templates, so the wording
// of a verification or reset email lives in a file that a designer can edit
// rather than in a string literal inside a handler.
//
// One name maps to up to three files, and the extension decides which engine
// parses them:
//
//	<name>.subject.tmpl   required — text/template
//	<name>.txt.tmpl       required — text/template
//	<name>.html.tmpl      optional — html/template
//
// The HTML half goes through html/template and NOT text/template, which is
// the whole reason the two engines are kept apart here: a username rendered
// into an HTML mail is untrusted input, and only html/template escapes it
// per context. The plain-text half must exist even when the HTML one does,
// because a message with no text alternative is what a text-only client and
// a spam filter both receive badly.
type Templates struct {
	text *texttemplate.Template
	html *htmltemplate.Template
}

// ParseFS loads every .tmpl under the filesystem. An application embeds its
// own directory:
//
//	//go:embed mailtemplates
//	var mailFS embed.FS
//	t, err := mail.ParseFS(mailFS, "mailtemplates/*.tmpl")
func ParseFS(fsys fs.FS, patterns ...string) (*Templates, error) {
	if len(patterns) == 0 {
		patterns = []string{"*.tmpl"}
	}
	text, err := texttemplate.New("mail").ParseFS(fsys, patterns...)
	if err != nil {
		return nil, fmt.Errorf("parse text templates: %w", err)
	}
	// The HTML set is parsed from the same files: html/template ignores
	// what it is not asked to execute, and a set with no .html.tmpl at
	// all is a legitimate text-only application.
	html, err := htmltemplate.New("mail").ParseFS(fsys, patterns...)
	if err != nil {
		return nil, fmt.Errorf("parse html templates: %w", err)
	}
	return &Templates{text: text, html: html}, nil
}

// Names returns the template names that can be rendered — the <name> of
// every <name>.txt.tmpl found.
func (t *Templates) Names() []string {
	var out []string
	for _, tmpl := range t.text.Templates() {
		if name, ok := strings.CutSuffix(tmpl.Name(), ".txt.tmpl"); ok {
			out = append(out, name)
		}
	}
	return out
}

// Render builds a message from the named templates. base supplies everything
// the templates do not: From, To and any custom headers.
//
// A subject that renders with a newline in it is REFUSED rather than
// trimmed: a newline in a subject is how a header injection starts, and
// silently repairing one hides the input that produced it.
func (t *Templates) Render(name string, data any, base Message) (Message, error) {
	subject, err := t.execText(name+".subject.tmpl", data)
	if err != nil {
		return Message{}, err
	}
	if strings.ContainsAny(subject, "\r\n") {
		return Message{}, fmt.Errorf("mail: rendered subject for %q contains a newline", name)
	}

	body, err := t.execText(name+".txt.tmpl", data)
	if err != nil {
		return Message{}, err
	}

	msg := base
	msg.Subject = strings.TrimSpace(subject)
	msg.Body = body

	if tmpl := t.html.Lookup(name + ".html.tmpl"); tmpl != nil {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return Message{}, fmt.Errorf("render %s.html.tmpl: %w", name, err)
		}
		msg.HTML = buf.String()
	}
	return msg, nil
}

func (t *Templates) execText(file string, data any) (string, error) {
	tmpl := t.text.Lookup(file)
	if tmpl == nil {
		return "", fmt.Errorf("mail: template %q not found", file)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render %s: %w", file, err)
	}
	return buf.String(), nil
}
