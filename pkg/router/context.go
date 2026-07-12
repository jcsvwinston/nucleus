package router

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

type contextKey string

const (
	sessionKey   contextKey = "gf_session"
	templatesKey contextKey = "gf_templates"
	// csrfTokenKey carries the exact CSRF token the CSRF middleware resolved for
	// the current request, so CSRFToken can return it regardless of storage mode
	// (cookie/session) or the configured session key. See pkg/router/csrf.go.
	csrfTokenKey contextKey = "gf_csrf_token"
)

var (
	ErrNilContextWriter        = errors.New("router.Context: response writer is nil")
	ErrNilContextRequest       = errors.New("router.Context: request is nil")
	ErrTemplateEngineNotSet    = errors.New("router.Context: template engine is not configured")
	ErrTemplateNameRequired    = errors.New("router.Context: template name is required")
	ErrFilePathRequired        = errors.New("router.Context: file path is required")
	ErrSessionManagerNotSet    = errors.New("router.Context: session manager is not configured")
	ErrDownloadFilenameInvalid = errors.New("router.Context: download filename is invalid")
)

// Handler is a function that processes a request and returns an error.
type Handler func(c *Context) error

// ContextHandlerFunc is an alias for Handler to maintain backward compatibility.
type ContextHandlerFunc = Handler

// HTTPError represents an error with an associated HTTP status code.
type HTTPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *HTTPError) Error() string {
	return e.Message
}

// NewHTTPError creates a new HTTPError.
func NewHTTPError(code int, message string) *HTTPError {
	return &HTTPError{Code: code, Message: message}
}

// Context is a unified request context for handlers.
// It wraps http.ResponseWriter and *http.Request and adds helpers for:
// - URL/query/form access
// - sessions
// - template binding/rendering
// - typed responses (JSON/XML/file/download)
type Context struct {
	Writer    http.ResponseWriter
	Request   *http.Request
	binds     map[string]interface{}
	session   *auth.SessionManager
	templates *template.Template
	handlers  []Handler
	index     int
}

// ContextOption configures a Context.
type ContextOption func(*Context)

// Next executes the next handler in the chain.
func (c *Context) Next() error {
	c.index++
	if c.index < len(c.handlers) {
		return c.handlers[c.index](c)
	}
	return nil
}

// NewContext creates a Context from an HTTP request/response pair.
func NewContext(w http.ResponseWriter, r *http.Request, handlers []Handler, opts ...ContextOption) *Context {
	ctx := &Context{
		Writer:   w,
		Request:  r,
		binds:    make(map[string]interface{}),
		handlers: handlers,
		index:    -1,
	}

	// Pull dependencies from request context if injected by Mux
	if sm, ok := r.Context().Value(sessionKey).(*auth.SessionManager); ok {
		ctx.session = sm
	}
	if tpl, ok := r.Context().Value(templatesKey).(*template.Template); ok {
		ctx.templates = tpl
	}

	for _, opt := range opts {
		if opt != nil {
			opt(ctx)
		}
	}
	return ctx
}

// ContextHandler adapts a Handler to http.HandlerFunc.
func ContextHandler(handlers ...Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(handlers) == 0 {
			http.Error(w, "no handlers defined", http.StatusInternalServerError)
			return
		}
		c := NewContext(w, r, handlers)
		if err := c.Next(); err != nil {
			handleError(c, err)
		}
	}
}

// FromHTTP adapts a standard http.HandlerFunc to a router.Handler.
func FromHTTP(h http.HandlerFunc) Handler {
	return func(c *Context) error {
		h(c.Writer, c.Request)
		return nil
	}
}

// FromHandler adapts a standard http.Handler to a router.Handler.
func FromHandler(h http.Handler) Handler {
	return func(c *Context) error {
		h.ServeHTTP(c.Writer, c.Request)
		return nil
	}
}

func handleError(c *Context, err error) {
	var domainErr *gferrors.DomainError
	if errors.As(err, &domainErr) {
		_ = c.JSON(domainErr.StatusCode, map[string]interface{}{
			"error": domainErr,
		})
		return
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		_ = c.JSON(httpErr.Code, map[string]interface{}{
			"error": httpErr.Message,
		})
		return
	}
	_ = c.JSON(http.StatusInternalServerError, map[string]interface{}{
		"error": err.Error(),
	})
}

// WithSession injects an auth session manager into Context.
func WithSession(sm *auth.SessionManager) ContextOption {
	return func(c *Context) {
		if c != nil {
			c.session = sm
		}
	}
}

// WithTemplates injects a template engine into Context.
func WithTemplates(t *template.Template) ContextOption {
	return func(c *Context) {
		if c != nil {
			c.templates = t
		}
	}
}

// Param reads a path parameter from a route pattern (Go 1.22 path value).
func (c *Context) Param(name string) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return strings.TrimSpace(c.Request.PathValue(strings.TrimSpace(name)))
}

// Query reads a query string parameter.
func (c *Context) Query(name string) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return strings.TrimSpace(c.Request.URL.Query().Get(strings.TrimSpace(name)))
}

// Form reads a form parameter.
func (c *Context) Form(name string) string {
	if c == nil || c.Request == nil {
		return ""
	}
	_ = c.Request.ParseForm()
	return strings.TrimSpace(c.Request.FormValue(strings.TrimSpace(name)))
}

// Value returns a parameter from path, then query string, then form data.
func (c *Context) Value(name string) string {
	if v := c.Param(name); v != "" {
		return v
	}
	if v := c.Query(name); v != "" {
		return v
	}
	return c.Form(name)
}

// Bind decodes request JSON and validates using validate tags.
func (c *Context) Bind(v interface{}) error {
	if c == nil || c.Request == nil {
		return ErrNilContextRequest
	}
	return Bind(c.Request, v)
}

// Set stores one key/value pair for template binding.
func (c *Context) Set(key string, value interface{}) {
	if c == nil {
		return
	}
	if c.binds == nil {
		c.binds = make(map[string]interface{})
	}
	k := strings.TrimSpace(key)
	if k == "" {
		return
	}
	c.binds[k] = value
}

// BindData merges values into template binding data.
func (c *Context) BindData(values map[string]interface{}) {
	if c == nil || len(values) == 0 {
		return
	}
	if c.binds == nil {
		c.binds = make(map[string]interface{}, len(values))
	}
	for k, v := range values {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		c.binds[k] = v
	}
}

// Data returns a copy of current template binding values.
func (c *Context) Data() map[string]interface{} {
	if c == nil || len(c.binds) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(c.binds))
	for k, v := range c.binds {
		out[k] = v
	}
	return out
}

// SessionManager returns the injected session manager.
func (c *Context) SessionManager() *auth.SessionManager {
	if c == nil {
		return nil
	}
	return c.session
}

// SessionGetString reads a string value from session.
func (c *Context) SessionGetString(key string) string {
	if c == nil || c.session == nil {
		return ""
	}
	return c.session.GetString(c.requestContext(), key)
}

// SessionPutString writes a string value to session.
func (c *Context) SessionPutString(key, value string) error {
	if c == nil || c.session == nil {
		return ErrSessionManagerNotSet
	}
	c.session.Put(c.requestContext(), key, value)
	return nil
}

// SessionGetInt reads an int value from session.
func (c *Context) SessionGetInt(key string) int {
	if c == nil || c.session == nil {
		return 0
	}
	return c.session.GetInt(c.requestContext(), key)
}

// SessionPutInt writes an int value to session.
func (c *Context) SessionPutInt(key string, value int) error {
	if c == nil || c.session == nil {
		return ErrSessionManagerNotSet
	}
	c.session.PutInt(c.requestContext(), key, value)
	return nil
}

// SessionGetBool reads a bool value from session.
func (c *Context) SessionGetBool(key string) bool {
	if c == nil || c.session == nil {
		return false
	}
	return c.session.GetBool(c.requestContext(), key)
}

// SessionPutBool writes a bool value to session.
func (c *Context) SessionPutBool(key string, value bool) error {
	if c == nil || c.session == nil {
		return ErrSessionManagerNotSet
	}
	c.session.PutBool(c.requestContext(), key, value)
	return nil
}

// SessionRemove removes one key from session.
func (c *Context) SessionRemove(key string) error {
	if c == nil || c.session == nil {
		return ErrSessionManagerNotSet
	}
	c.session.Remove(c.requestContext(), key)
	return nil
}

// SessionDestroy destroys the current session.
func (c *Context) SessionDestroy() error {
	if c == nil || c.session == nil {
		return ErrSessionManagerNotSet
	}
	return c.session.Destroy(c.requestContext())
}

// SessionRenewToken renews the current session token.
func (c *Context) SessionRenewToken() error {
	if c == nil || c.session == nil {
		return ErrSessionManagerNotSet
	}
	return c.session.RenewToken(c.requestContext())
}

// JSON writes a JSON response.
func (c *Context) JSON(status int, data interface{}) error {
	if c == nil || c.Writer == nil {
		return ErrNilContextWriter
	}
	JSON(c.Writer, status, data)
	return nil
}

// XML writes an XML response.
func (c *Context) XML(status int, data interface{}) error {
	if c == nil || c.Writer == nil {
		return ErrNilContextWriter
	}
	c.Writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
	c.Writer.WriteHeader(status)
	if data == nil {
		return nil
	}
	return xml.NewEncoder(c.Writer).Encode(data)
}

// HTML renders a named template using merged bound data and call data.
// Values from data override previously bound keys.
func (c *Context) HTML(status int, templateName string, data map[string]interface{}) error {
	if c == nil || c.Writer == nil {
		return ErrNilContextWriter
	}
	if c.templates == nil {
		return ErrTemplateEngineNotSet
	}
	name := strings.TrimSpace(templateName)
	if name == "" {
		return ErrTemplateNameRequired
	}

	payload := c.Data()
	for k, v := range data {
		payload[k] = v
	}

	var buf bytes.Buffer
	if err := c.templates.ExecuteTemplate(&buf, name, payload); err != nil {
		return err
	}

	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Writer.WriteHeader(status)
	_, err := c.Writer.Write(buf.Bytes())
	return err
}

// File serves a file as-is.
func (c *Context) File(path string) error {
	if c == nil || c.Writer == nil {
		return ErrNilContextWriter
	}
	if c.Request == nil {
		return ErrNilContextRequest
	}
	resolved, err := validateFilePath(path)
	if err != nil {
		return err
	}
	http.ServeFile(c.Writer, c.Request, resolved)
	return nil
}

// Download serves a file with attachment content disposition.
func (c *Context) Download(path, filename string) error {
	if c == nil || c.Writer == nil {
		return ErrNilContextWriter
	}
	if c.Request == nil {
		return ErrNilContextRequest
	}
	resolved, err := validateFilePath(path)
	if err != nil {
		return err
	}

	downloadName := strings.TrimSpace(filename)
	if downloadName == "" {
		downloadName = filepath.Base(resolved)
	}
	if downloadName == "" || downloadName == "." || downloadName == string(filepath.Separator) {
		return ErrDownloadFilenameInvalid
	}

	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(downloadName)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", downloadName))
	http.ServeFile(c.Writer, c.Request, resolved)
	return nil
}

// Redirect sends an HTTP redirect.
func (c *Context) Redirect(status int, location string) error {
	if c == nil || c.Writer == nil {
		return ErrNilContextWriter
	}
	if c.Request == nil {
		return ErrNilContextRequest
	}
	http.Redirect(c.Writer, c.Request, location, status)
	return nil
}

// NoContent writes a 204 response.
func (c *Context) NoContent() error {
	if c == nil || c.Writer == nil {
		return ErrNilContextWriter
	}
	NoContent(c.Writer)
	return nil
}

func (c *Context) requestContext() context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}

func validateFilePath(path string) (string, error) {
	resolved := strings.TrimSpace(path)
	if resolved == "" {
		return "", ErrFilePathRequired
	}
	// Reject path-traversal escapes. After Clean, a leading ".." element means
	// the path climbs above its base — a common vector when c.File is handed
	// untrusted input (c.File(userInput)). Callers that need to serve files
	// from a specific directory should still resolve against a fixed root.
	cleaned := filepath.Clean(resolved)
	for _, part := range strings.Split(filepath.ToSlash(cleaned), "/") {
		if part == ".." {
			return "", fmt.Errorf("router.Context: path traversal not allowed in %q", path)
		}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("router.Context: %s is a directory", resolved)
	}
	return resolved, nil
}
