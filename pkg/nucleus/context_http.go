// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// One shape for the input: the query string, the path parameters and the
// headers bind into a struct by their tags and validate by their
// `validate` tags, like the body does with BindJSON. The tags, the types
// they convert and the errors they return are router.BindQuery's:
//
//	type ShowNote struct {
//	    ID      int64  `path:"id"`
//	    Expand  bool   `query:"expand"`
//	    TraceID string `header:"X-Trace-Id"`
//	}
//
//	func show(c *nucleus.Context) error {
//	    var in ShowNote
//	    if err := c.BindRequest(&in); err != nil {
//	        return err // 400 naming the parameter, or 422 naming the field
//	    }
//	    ...
//	}

// BindQuery binds the query string into v by its `query:"name"` tags and
// validates it. See router.BindQuery.
func (c *Context) BindQuery(v any) error {
	if c.Context.Request == nil {
		return routerpkg.ErrNilContextRequest
	}
	return routerpkg.BindQuery(c.Context.Request, v)
}

// BindPath binds the route's path parameters into v by its `path:"name"`
// tags and validates it. See router.BindPath.
func (c *Context) BindPath(v any) error {
	if c.Context.Request == nil {
		return routerpkg.ErrNilContextRequest
	}
	return routerpkg.BindPath(c.Context.Request, v)
}

// BindHeaders binds the request headers into v by its `header:"X-Name"`
// tags and validates it. See router.BindHeaders.
func (c *Context) BindHeaders(v any) error {
	if c.Context.Request == nil {
		return routerpkg.ErrNilContextRequest
	}
	return routerpkg.BindHeaders(c.Context.Request, v)
}

// BindRequest binds the whole request into v — path, query and headers by
// their tags, then a JSON body into the `json`-tagged fields — and
// validates it once. A body cannot overwrite a path, query or header
// field. See router.BindRequest.
func (c *Context) BindRequest(v any) error {
	if c.Context.Request == nil {
		return routerpkg.ErrNilContextRequest
	}
	return routerpkg.BindRequest(c.Context.Request, v)
}

// Negotiate answers v with code in the representation the client's Accept
// header asks for — JSON, XML or plain text — and returns a 406 error (in
// the framework's error shape once the handler returns it) when it accepts
// none of them. See router.Negotiate.
//
//	return c.Negotiate(http.StatusOK, note) // JSON for most clients, XML for one that asks
func (c *Context) Negotiate(code int, v any) error {
	return routerpkg.Negotiate(c.Context.Writer, c.Context.Request, code, v)
}

// RawHTML sends html, a complete HTML document or fragment, as the
// response body: no template is involved and nothing is escaped, so html
// must be trusted. For a page rendered from a template use Render.
//
// It is what HTML(code, html) did under a name that said otherwise: the
// embedded router.Context's HTML renders a NAMED TEMPLATE, and the two
// meanings under one name are why HTML is deprecated in favour of this
// method and Render.
func (c *Context) RawHTML(code int, html string) error {
	c.Context.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Context.Writer.WriteHeader(code)
	_, err := c.Context.Writer.Write([]byte(html))
	return err
}
