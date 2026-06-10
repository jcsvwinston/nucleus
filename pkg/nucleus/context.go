package nucleus

import (
	"encoding/xml"
	"net/http"

	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

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

// BindXML binds XML body to the given struct
func (c *Context) BindXML(v interface{}) error {
	if c.Context.Request == nil || c.Context.Request.Body == nil {
		return routerpkg.ErrNilContextRequest
	}
	dec := xml.NewDecoder(c.Context.Request.Body)
	return dec.Decode(v)
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

// HTML sends an HTML response
func (c *Context) HTML(code int, html string) error {
	c.Context.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Context.Writer.WriteHeader(code)
	_, err := c.Context.Writer.Write([]byte(html))
	return err
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
