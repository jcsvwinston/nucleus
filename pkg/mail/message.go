package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"sort"
	"strings"
)

// buildRFC822Message renders one message as an RFC 5322 payload.
//
// The shape depends on what the message carries, and only on that: a plain
// body stays a single text/plain part — byte for byte what this function
// emitted before HTML and attachments existed, which is what keeps an
// upgrade invisible to everything already sending mail. An HTML body adds a
// multipart/alternative with the plain text FIRST, because that is the order
// RFC 2046 gives the reader ("least faithful first"): a client that cannot
// render HTML shows the text instead of a base64 wall. Attachments wrap the
// lot in a multipart/mixed.
func buildRFC822Message(msg Message) []byte {
	var body bytes.Buffer
	headers := baseHeaders(msg)

	html := strings.TrimSpace(msg.HTML)
	switch {
	case html == "" && len(msg.Attachments) == 0:
		headers = append(headers, "Content-Type: text/plain; charset=UTF-8")
		headers = append(headers, customHeaders(msg)...)
		return []byte(strings.Join(append(headers, "", msg.Body), "\r\n"))

	case len(msg.Attachments) == 0:
		w := multipart.NewWriter(&body)
		headers = append(headers, `Content-Type: multipart/alternative; boundary="`+w.Boundary()+`"`)
		writeAlternative(w, msg)
		_ = w.Close()

	default:
		w := multipart.NewWriter(&body)
		headers = append(headers, `Content-Type: multipart/mixed; boundary="`+w.Boundary()+`"`)
		if html == "" {
			writeTextPart(w, "text/plain; charset=UTF-8", msg.Body)
		} else {
			// The alternative lives inside its own part with its own
			// boundary, so a reader walking the mixed tree finds one body
			// with two representations rather than two bodies.
			alt := &bytes.Buffer{}
			iw := multipart.NewWriter(alt)
			writeAlternative(iw, msg)
			_ = iw.Close()
			part, err := w.CreatePart(textproto.MIMEHeader{
				"Content-Type": {`multipart/alternative; boundary="` + iw.Boundary() + `"`},
			})
			if err == nil {
				_, _ = part.Write(alt.Bytes())
			}
		}
		for _, a := range msg.Attachments {
			writeAttachment(w, a)
		}
		_ = w.Close()
	}

	headers = append(headers, customHeaders(msg)...)
	out := strings.Join(append(headers, "", ""), "\r\n")
	return append([]byte(out), body.Bytes()...)
}

func baseHeaders(msg Message) []string {
	return []string{
		"From: " + strings.TrimSpace(msg.From),
		"To: " + strings.Join(cleanRecipients(msg.To), ", "),
		// A subject is user text and routinely carries accents. Encoding it
		// only when it needs it keeps ASCII subjects readable in a raw log.
		"Subject: " + mime.QEncoding.Encode("UTF-8", strings.TrimSpace(msg.Subject)),
		"MIME-Version: 1.0",
	}
}

func customHeaders(msg Message) []string {
	if len(msg.Headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(msg.Headers))
	for k := range msg.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(msg.Headers[key])
		if value == "" {
			continue
		}
		out = append(out, fmt.Sprintf("%s: %s", strings.TrimSpace(key), value))
	}
	return out
}

func writeAlternative(w *multipart.Writer, msg Message) {
	writeTextPart(w, "text/plain; charset=UTF-8", msg.Body)
	writeTextPart(w, "text/html; charset=UTF-8", msg.HTML)
}

// writeTextPart writes one text part quoted-printable, which is what keeps a
// long HTML line from breaking the 998-octet limit RFC 5322 sets on a line.
func writeTextPart(w *multipart.Writer, contentType, content string) {
	part, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {contentType},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return
	}
	qp := quotedprintable.NewWriter(part)
	_, _ = qp.Write([]byte(content))
	_ = qp.Close()
}

func writeAttachment(w *multipart.Writer, a Attachment) {
	contentType := strings.TrimSpace(a.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	disposition := "attachment"
	if a.Inline {
		disposition = "inline"
	}
	// FormatMediaType quotes and encodes the filename per RFC 2045/2231,
	// which is what keeps a name with a space, a quote or a non-ASCII
	// character from ending the parameter early.
	header := textproto.MIMEHeader{
		"Content-Type":              {contentType},
		"Content-Transfer-Encoding": {"base64"},
		"Content-Disposition":       {mime.FormatMediaType(disposition, map[string]string{"filename": sanitizeFilename(a.Filename)})},
	}
	if cid := strings.TrimSpace(a.ContentID); cid != "" {
		header.Set("Content-ID", "<"+cid+">")
	}
	part, err := w.CreatePart(header)
	if err != nil {
		return
	}
	writeBase64Wrapped(part, a.Content)
}

// sanitizeFilename strips what a filename must never carry into a header or
// onto a recipient's disk: line breaks, which is how a header injection
// starts, and path separators, which is how an attachment lands somewhere it
// was not meant to. A caller's filename is input, not decoration.
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "attachment"
	}
	replacer := strings.NewReplacer("\r", "", "\n", "", `"`, "", `\`, "", "/", "_")
	name = replacer.Replace(name)
	if name == "" {
		return "attachment"
	}
	return name
}

func cleanRecipients(in []string) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		trimmed := strings.TrimSpace(r)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// writeBase64Wrapped writes content base64-encoded in 76-character lines.
// The wrapping is not cosmetic: RFC 2045 sets that limit, and a single long
// line is what makes some servers reject or mangle an attachment.
func writeBase64Wrapped(w io.Writer, content []byte) {
	encoded := base64.StdEncoding.EncodeToString(content)
	for len(encoded) > 76 {
		_, _ = io.WriteString(w, encoded[:76]+"\r\n")
		encoded = encoded[76:]
	}
	if encoded != "" {
		_, _ = io.WriteString(w, encoded+"\r\n")
	}
}
