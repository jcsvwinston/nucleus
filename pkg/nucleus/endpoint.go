// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"fmt"
	"net/http"
	"reflect"
)

// Typed endpoints (A10). A plain handler, func(*Context) error, says
// nothing about what it reads or writes, so the derived OpenAPI document
// can only list its path. A typed endpoint states both in its signature:
//
//	func (m *module) createArticle(c *nucleus.Context, in CreateArticle) (Article, error)
//
//	nucleus.Handle(r, http.MethodPost, "/api/articles", m.createArticle, nucleus.Status(http.StatusCreated))
//
// The framework binds In from the request — the fields tagged path, query
// and header from those, the rest from the JSON body — and validates it by
// its validate tags (c.BindRequest), calls the function, and writes Out as
// JSON with the success status. An error goes where a plain handler's
// error goes: the framework's error response. The document gets the
// operation's parameters, request body and response from In and Out (see
// openapi.SchemaOf), so the schemas, the generated client and the request
// validation all come from the same two types.
//
// In may be struct{} for an endpoint that reads nothing; Out may be
// struct{} for one that answers no content (204 unless Status says
// otherwise).

// EndpointOption adjusts a typed endpoint.
type EndpointOption func(*endpointDoc)

// Status sets the success status a typed endpoint answers with (200 by
// default; 204 when Out is struct{}).
func Status(code int) EndpointOption {
	return func(ep *endpointDoc) { ep.Status = code }
}

// Summary sets the operation's one-line summary in the document.
func Summary(text string) EndpointOption {
	return func(ep *endpointDoc) { ep.Summary = text }
}

// Description sets the operation's longer description in the document.
func Description(text string) EndpointOption {
	return func(ep *endpointDoc) { ep.Description = text }
}

var emptyStruct = reflect.TypeOf(struct{}{})

// Handle registers a typed endpoint for method and path on r. See the
// comment above for what it binds, writes and documents. method is one of
// GET, POST, PUT, PATCH and DELETE; another method, or a nil function, is a
// boot error naming the module (see registrationFailed).
func Handle[In, Out any](r Router, method, path string, fn func(*Context, In) (Out, error), opts ...EndpointOption) {
	switch {
	case fn == nil:
		registrationFailed(r, fmt.Errorf("nucleus.Handle(%s %s): nil function", method, path))
		return
	case !typedMethods[method]:
		registrationFailed(r, fmt.Errorf("nucleus.Handle(%s %s): unsupported method (GET, POST, PUT, PATCH or DELETE)", method, path))
		return
	}
	ep := &endpointDoc{
		In:  reflect.TypeOf((*In)(nil)).Elem(),
		Out: reflect.TypeOf((*Out)(nil)).Elem(),
	}
	for _, o := range opts {
		o(ep)
	}
	reads := ep.In != emptyStruct
	if !reads {
		ep.In = nil
	}
	if ep.Out == emptyStruct {
		ep.Out = nil
		if ep.Status == 0 {
			ep.Status = http.StatusNoContent
		}
	}
	if ep.Status == 0 {
		ep.Status = http.StatusOK
	}
	status := ep.Status
	h := func(c *Context) error {
		var in In
		if reads {
			if err := c.BindRequest(&in); err != nil {
				return err
			}
		}
		out, err := fn(c, in)
		if err != nil {
			return err
		}
		if ep.Out == nil || status == http.StatusNoContent {
			c.Writer.WriteHeader(status)
			return nil
		}
		return c.JSON(status, out)
	}

	if a, ok := r.(*routerAdapter); ok {
		a.handleTyped(method, path, h, handlerName(fn), ep)
		return
	}
	// A Router the framework did not build (a test double): the route
	// still works, it is only absent from the document.
	switch method {
	case http.MethodGet:
		r.Get(path, h)
	case http.MethodPost:
		r.Post(path, h)
	case http.MethodPut:
		r.Put(path, h)
	case http.MethodPatch:
		r.Patch(path, h)
	case http.MethodDelete:
		r.Delete(path, h)
	}
}

// typedMethods are the methods Handle registers.
var typedMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
}

// handleTyped registers a typed endpoint's handler, validated when the
// application validates requests, and records it with its types.
func (a *routerAdapter) handleTyped(method, path string, h Handler, name string, ep *endpointDoc) {
	handlers := a.validated(method, path, []Handler{h})
	joined := a.joinPath(path)
	switch method {
	case http.MethodGet:
		a.mux.Get(joined, adaptHandlers(handlers)...)
	case http.MethodPost:
		a.mux.Post(joined, adaptHandlers(handlers)...)
	case http.MethodPut:
		a.mux.Put(joined, adaptHandlers(handlers)...)
	case http.MethodPatch:
		a.mux.Patch(joined, adaptHandlers(handlers)...)
	case http.MethodDelete:
		a.mux.Delete(joined, adaptHandlers(handlers)...)
	}
	a.rec.recordNamed(method, joined, name, ep)
}
