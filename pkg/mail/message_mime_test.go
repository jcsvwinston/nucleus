package mail

import (
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

// A message with only a body must render exactly as it did before HTML and
// attachments existed. The upgrade is invisible to everything already
// sending mail, or it is not an upgrade.
func TestBuildRFC822_PlainBodyUnchanged(t *testing.T) {
	got := string(buildRFC822Message(Message{
		From: "no-reply@example.test", To: []string{"ana@example.test"},
		Subject: "hello", Body: "plain body",
	}))
	want := strings.Join([]string{
		"From: no-reply@example.test",
		"To: ana@example.test",
		"Subject: hello",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"plain body",
	}, "\r\n")
	if got != want {
		t.Fatalf("plain rendering changed.\n got: %q\nwant: %q", got, want)
	}
}

// The text alternative comes FIRST (RFC 2046 §5.1.4: least faithful first),
// so a client that cannot render HTML shows the text.
func TestBuildRFC822_AlternativeOrder(t *testing.T) {
	parts := parseParts(t, Message{
		From: "no-reply@example.test", To: []string{"ana@example.test"},
		Subject: "verify", Body: "text version", HTML: "<p>html version</p>",
	}, "multipart/alternative")

	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if ct := parts[0].contentType; !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("first part is %q, want text/plain", ct)
	}
	if ct := parts[1].contentType; !strings.HasPrefix(ct, "text/html") {
		t.Errorf("second part is %q, want text/html", ct)
	}
	if !strings.Contains(parts[0].body, "text version") {
		t.Errorf("text part lost its content: %q", parts[0].body)
	}
	if !strings.Contains(parts[1].body, "html version") {
		t.Errorf("html part lost its content: %q", parts[1].body)
	}
}

// An attachment wraps the message in multipart/mixed and travels base64 in
// lines of at most 76 characters (RFC 2045): a single long line is what some
// servers mangle.
func TestBuildRFC822_AttachmentEncoding(t *testing.T) {
	content := []byte(strings.Repeat("nucleus", 200))
	parts := parseParts(t, Message{
		From: "no-reply@example.test", To: []string{"ana@example.test"},
		Subject: "invoice", Body: "attached",
		Attachments: []Attachment{{
			Filename: "invoice.pdf", ContentType: "application/pdf", Content: content,
		}},
	}, "multipart/mixed")

	if len(parts) != 2 {
		t.Fatalf("expected body + attachment, got %d parts", len(parts))
	}
	att := parts[1]
	disp, dispParams, err := mime.ParseMediaType(att.disposition)
	if err != nil || disp != "attachment" || dispParams["filename"] != "invoice.pdf" {
		t.Errorf("disposition is %q (%v)", att.disposition, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(att.body), "\n") {
		if len(strings.TrimRight(line, "\r")) > 76 {
			t.Fatalf("base64 line longer than 76 characters: %d", len(line))
		}
	}
	decodedContent, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\r", "", "\n", "").Replace(att.body))
	if err != nil {
		t.Fatalf("attachment does not decode: %v", err)
	}
	if string(decodedContent) != string(content) {
		t.Fatal("attachment content did not survive the round trip")
	}
}

// A filename is input: it reaches a header and a recipient's disk. A name
// carrying CRLF must not become a header of its own — the test asks the
// PARSER, because "the string does not appear" would also pass for a name
// that was merely mangled.
func TestBuildRFC822_FilenameCannotForgeAHeader(t *testing.T) {
	msg := Message{
		From: "no-reply@example.test", To: []string{"ana@example.test"},
		Subject: "probe", Body: "body",
		Attachments: []Attachment{{
			Filename: "in\"voice\r\nBcc: attacker@evil.test\r\n.pdf",
			Content:  []byte("x"),
		}},
	}
	payload := string(buildRFC822Message(msg))
	parsed, err := mail.ReadMessage(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("the payload is not parseable: %v", err)
	}
	if got := parsed.Header.Get("Bcc"); got != "" {
		t.Fatalf("a filename forged a Bcc header: %q", got)
	}
	for _, part := range parseParts(t, msg, "multipart/mixed") {
		if strings.Contains(part.disposition, "\n") {
			t.Fatalf("disposition carries a newline: %q", part.disposition)
		}
	}
	// And the name still has to be a usable filename, not an empty one.
	parts := parseParts(t, msg, "multipart/mixed")
	_, params, err := mime.ParseMediaType(parts[1].disposition)
	if err != nil || strings.TrimSpace(params["filename"]) == "" {
		t.Fatalf("the attachment lost its filename: %q (%v)", parts[1].disposition, err)
	}
}

// A subject with accents is encoded per RFC 2047; an ASCII one is left
// readable.
func TestBuildRFC822_SubjectEncoding(t *testing.T) {
	payload := string(buildRFC822Message(Message{
		From: "no-reply@example.test", To: []string{"ana@example.test"},
		Subject: "Confirmación", Body: "body",
	}))
	if !strings.Contains(payload, "Subject: =?UTF-8?q?") {
		t.Fatalf("non-ASCII subject was not encoded:\n%s", payload)
	}
	decoded, err := new(mime.WordDecoder).DecodeHeader(headerValue(t, payload, "Subject"))
	if err != nil || decoded != "Confirmación" {
		t.Fatalf("subject did not survive encoding: %q (%v)", decoded, err)
	}
}

// --- helpers ---------------------------------------------------------------

type renderedPart struct {
	contentType string
	disposition string
	body        string
}

func parseParts(t *testing.T, msg Message, wantType string) []renderedPart {
	t.Helper()
	parsed, err := mail.ReadMessage(strings.NewReader(string(buildRFC822Message(msg))))
	if err != nil {
		t.Fatalf("the payload is not a parseable message: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("content type: %v", err)
	}
	if mediaType != wantType {
		t.Fatalf("content type is %q, want %q", mediaType, wantType)
	}

	reader := multipart.NewReader(parsed.Body, params["boundary"])
	var out []renderedPart
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read part body: %v", err)
		}
		out = append(out, renderedPart{
			contentType: part.Header.Get("Content-Type"),
			disposition: part.Header.Get("Content-Disposition"),
			body:        string(body),
		})
	}
	return out
}

func headerValue(t *testing.T, payload, name string) string {
	t.Helper()
	parsed, err := mail.ReadMessage(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	return parsed.Header.Get(name)
}
