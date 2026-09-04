package nucleus

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/validate"
)

// maxXMLBodyBytes caps a BindXML body the way router.Bind caps a JSON one:
// 1 MiB, before the decoder reads a byte. XML is the worse of the two to
// leave open — a decoder that expands entities on the way in makes a small
// body expensive as well as large.
const maxXMLBodyBytes = 1 << 20

// Context wraps the router Context with simplified methods
type Context struct {
	*routerpkg.Context
}

// BindJSON binds JSON body to the given struct
func (c *Context) BindJSON(v interface{}) error {
	if c.Context.Request == nil {
		return routerpkg.ErrNilContextRequest
	}
	return routerpkg.Bind(c.Context.Request, v)
}

// BindXML binds an XML body to the given struct with the same discipline
// as BindJSON: the body is capped at 1 MiB (413 beyond it), a malformed
// document is a 400, and the decoded value is validated against its
// `validate` tags before it is returned.
func (c *Context) BindXML(v interface{}) error {
	if c.Context.Request == nil || c.Context.Request.Body == nil {
		return routerpkg.ErrNilContextRequest
	}
	r := c.Context.Request
	// nil ResponseWriter: MaxBytesReader only uses it to flag the
	// connection for closure; the error return is what matters here
	// (same pattern as router.Bind).
	r.Body = http.MaxBytesReader(nil, r.Body, maxXMLBodyBytes)

	dec := xml.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return &gferrors.DomainError{
				Code:       "PAYLOAD_TOO_LARGE",
				Message:    fmt.Sprintf("request body exceeds %d bytes", maxErr.Limit),
				StatusCode: http.StatusRequestEntityTooLarge,
			}
		}
		return gferrors.BadRequest("invalid XML: " + err.Error())
	}

	if err := validate.Validate(v); err != nil {
		var domErr *gferrors.DomainError
		if errors.As(err, &domErr) {
			return domErr
		}
		return gferrors.BadRequest(err.Error())
	}
	return nil
}

// BindForm binds urlencoded or multipart form data to the given struct with
// typed conversion (ints, floats, bools, time.Time, pointers; `form:`/`json:`
// tags), then validates it using struct validate tags — same discipline as
// BindJSON. See router.BindForm for the full binding rules.
func (c *Context) BindForm(v interface{}) error {
	if c.Context.Request == nil {
		return routerpkg.ErrNilContextRequest
	}
	return routerpkg.BindForm(c.Context.Request, v)
}

// Query returns query parameters
func (c *Context) Query(key string) string {
	return c.Context.Query(key)
}

// Param returns URL path parameter
func (c *Context) Param(key string) string {
	return c.Context.Param(key)
}

// JSON sends a JSON response
func (c *Context) JSON(code int, v interface{}) error {
	return c.Context.JSON(code, v)
}

// XML sends an XML response
func (c *Context) XML(code int, v interface{}) error {
	c.Context.Writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
	c.Context.Writer.WriteHeader(code)
	enc := xml.NewEncoder(c.Context.Writer)
	return enc.Encode(v)
}

// HTML sends a RAW HTML string as the response body. No template is
// involved and nothing is escaped — the string is written as-is.
//
// NOTE the shadowing (AN-07): the embedded router.Context also has an HTML
// method, with a different signature and a different job — it renders a
// NAMED TEMPLATE through the application's template engine. This method
// hides it, which is why generated code used to spell c.Context.HTML for a
// template render. For templates use Render on this Context, which forwards
// to the engine; use HTML only when you already hold a fully-formed (and
// trusted) HTML string.
func (c *Context) HTML(code int, html string) error {
	c.Context.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Context.Writer.WriteHeader(code)
	_, err := c.Context.Writer.Write([]byte(html))
	return err
}

// Render renders a named template through the application's template engine,
// merging the request's bound data with the given data — it forwards to the
// embedded router.Context's engine-backed HTML method under an unshadowed
// name (AN-07). The template name is the file's path relative to
// templates_dir (or `<module>/<path>` for module-embedded templates), e.g.
//
//	return c.Render(http.StatusOK, "blog/index.html", map[string]interface{}{"title": "Blog"})
//
// Contrast with HTML on this Context, which writes a raw HTML string and
// touches no template.
func (c *Context) Render(code int, templateName string, data map[string]interface{}) error {
	return c.Context.HTML(code, templateName, data)
}

// String sends a plain text response
func (c *Context) String(code int, s string) error {
	c.Context.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Context.Writer.WriteHeader(code)
	_, err := c.Context.Writer.Write([]byte(s))
	return err
}

// Status sends only status code
func (c *Context) Status(code int) {
	c.Context.Writer.WriteHeader(code)
}

// NoContent sends 204 No Content
func (c *Context) NoContent() error {
	c.Context.Writer.WriteHeader(http.StatusNoContent)
	return nil
}

// Redirect redirects to the given URL
func (c *Context) Redirect(code int, url string) error {
	http.Redirect(c.Context.Writer, c.Context.Request, url, code)
	return nil
}

// Set sets a value in context (for templates)
func (c *Context) Set(key string, value interface{}) {
	c.Context.Set(key, value)
}

// Get retrieves a value from context
func (c *Context) Get(key string) interface{} {
	return c.Context.Data()[key]
}

// RequestID returns the request ID
func (c *Context) RequestID() string {
	return routerpkg.GetReqID(c.Context.Request.Context())
}

// SessionGetString reads a string value from session
func (c *Context) SessionGetString(key string) string {
	return c.Context.SessionGetString(key)
}

// SessionPutString writes a string value to session
func (c *Context) SessionPutString(key, value string) error {
	return c.Context.SessionPutString(key, value)
}
