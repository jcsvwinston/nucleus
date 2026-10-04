// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	goruntime "runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/openapi"
)

// APIDocumentSpec declares the OpenAPI document the framework DERIVES from
// the application and serves at Pattern (A10): every route the modules
// register, with its path parameters, its module as the tag and an
// operationId named after its handler; the request and response schemas of
// the routes registered as typed endpoints, read from their Go structs; and
// the security the application actually enforces — the bearer scheme when
// it verifies JWTs, and an explicit "no authentication" on every operation
// the authorization policy opens to anonymous callers.
//
// Base documents are merged UNDER the derived one: an operation, a schema
// or a security scheme a base declares is kept as written (it is what the
// author chose to say), and the derivation fills in everything the base
// does not mention. A project generated with internal/contracts passes its
// aggregator here, so the hand-written contract and the routes cannot
// drift apart in two documents:
//
//	nucleus.New().WithOpenAPIDocument("/openapi.json", contracts.NewDocument())
//
// Base is any json.Marshaler so this stable surface names no type of the
// experimental pkg/openapi (the reason DEP-2026-008 gives); an
// *openapi.Document is one.
//
// The document is built once, after every module has mounted, and served
// from memory. The route is readable by anonymous callers (a document
// describes the API; it carries no data) — an operator deny row on the
// pattern closes it again. `nucleus openapi` prints the same document: it
// boots the application the way `nucleus routes` does and reads it back.
type APIDocumentSpec struct {
	Pattern string
	Base    []json.Marshaler

	// ValidateRequests checks every request to a module route against the
	// operation the document declares for it before the handler runs: the
	// path, query and header parameters (converted to their declared types)
	// and the JSON body. A request that departs is answered 400 with one
	// entry per departure, naming the field. Set by WithOpenAPIValidation.
	ValidateRequests bool
}

// WithOpenAPIDocument serves the OpenAPI document derived from the
// application at pattern ("/openapi.json" when empty), merged over the
// optional base documents. See APIDocumentSpec. Calling it again replaces
// the previous declaration (last wins, like the other setters).
func (b *AppBuilder) WithOpenAPIDocument(pattern string, base ...json.Marshaler) *AppBuilder {
	if b.err != nil {
		return b
	}
	for i, m := range base {
		if m == nil || (reflect.ValueOf(m).Kind() == reflect.Pointer && reflect.ValueOf(m).IsNil()) {
			b.err = fmt.Errorf("nucleus: WithOpenAPIDocument: base document %d is nil", i)
			return b
		}
	}
	b.a.APIDocument = &APIDocumentSpec{Pattern: pattern, Base: append([]json.Marshaler(nil), base...)}
	return b
}

// WithOpenAPIValidation makes the document a contract the application
// enforces: every request to a module route is checked against the
// operation the document declares before the handler runs (see
// APIDocumentSpec.ValidateRequests). It needs WithOpenAPIDocument, in
// either order.
func (b *AppBuilder) WithOpenAPIValidation() *AppBuilder {
	if b.err != nil {
		return b
	}
	b.validateAPIRequests = true
	return b
}

// describedRoute is one registration a module made through its Router, as
// the derived document needs it: the full application path in the router's
// pattern syntax, the module that owns it, and the name of the handler.
type describedRoute struct {
	Method   string
	Path     string
	Module   string
	Handler  string
	Endpoint *endpointDoc
}

// endpointDoc is what a typed endpoint knows about itself and a plain
// handler does not: its input and output types and its success status.
type endpointDoc struct {
	In, Out     reflect.Type
	Status      int
	Summary     string
	Description string
}

// routeRecorder is the per-adapter view of the application's described
// routes: the module registering and the application path the adapter's mux
// is mounted at. Nil on an adapter nobody records for.
type routeRecorder struct {
	inv    *routeInventory
	module string
	base   string
}

// sub returns the recorder of a group mounted at prefix (already joined to
// the adapter's own prefix) below this one.
func (r *routeRecorder) sub(prefix string) *routeRecorder {
	if r == nil {
		return nil
	}
	return &routeRecorder{inv: r.inv, module: r.module, base: joinAppPath(r.base, prefix)}
}

func (r *routeRecorder) record(method, path string, handlers []Handler, ep *endpointDoc) {
	if r == nil || r.inv == nil {
		return
	}
	name := ""
	if len(handlers) > 0 {
		name = handlerName(handlers[len(handlers)-1])
	}
	r.inv.described = append(r.inv.described, describedRoute{
		Method:   method,
		Path:     joinAppPath(r.base, path),
		Module:   r.module,
		Handler:  name,
		Endpoint: ep,
	})
}

// recordResource records one verb of a Resource. The verb's method is the
// same on every controller (Index, Show…), so the operation is named after
// the verb and the resource's last path segment instead: indexTickets,
// showTickets.
func (r *routeRecorder) recordResource(method, path, verb, resourcePath string) {
	if r == nil || r.inv == nil {
		return
	}
	name := lowerFirst(verb)
	segs := strings.FieldsFunc(resourcePath, func(c rune) bool { return c == '/' })
	for i := len(segs) - 1; i >= 0; i-- {
		if !strings.HasPrefix(segs[i], "{") {
			name += upperFirstRune(segs[i])
			break
		}
	}
	r.inv.described = append(r.inv.described, describedRoute{
		Method:  method,
		Path:    joinAppPath(r.base, path),
		Module:  r.module,
		Handler: name,
	})
}

// validator returns the handler that checks a request against the operation
// the document declares for method and path, ahead of the route's own
// handlers; nil when the application does not validate. The document is
// built after every module has registered, so the handler reads it at
// request time.
func (r *routeRecorder) validator(method, path string) Handler {
	if r == nil || r.inv == nil || !r.inv.validate {
		return nil
	}
	inv := r.inv
	template, params := openAPIPath(joinAppPath(r.base, path))
	return func(c *Context) error {
		doc := inv.document
		if doc == nil {
			return c.Next()
		}
		op := doc.Paths[template].Operation(method)
		if op == nil {
			return c.Next()
		}
		route := openapi.Route{Template: template, Operation: op, Params: map[string]string{}}
		for _, name := range params {
			route.Params[name] = c.Request.PathValue(name)
		}
		if errs := doc.ValidateRequest(c.Request, route); len(errs) > 0 {
			return invalidRequestError(errs)
		}
		return c.Next()
	}
}

// invalidRequestError is the 400 a request that departs from the document
// gets: one entry per departure, each naming its field.
func invalidRequestError(errs openapi.ValidationErrors) error {
	return &gferrors.DomainError{
		Code:       "INVALID_REQUEST",
		Message:    "the request does not match the API document: " + errs.Error(),
		StatusCode: http.StatusBadRequest,
		Details:    map[string]any{"errors": []openapi.ValidationError(errs)},
	}
}

// joinAppPath joins a mount point and a path registered below it the way
// the router serves them: a root route ("/") under a mount answers at the
// mount point itself.
func joinAppPath(base, p string) string {
	base = strings.TrimRight(base, "/")
	if p == "" || p == "/" {
		if base == "" {
			return "/"
		}
		return base
	}
	return base + "/" + strings.TrimLeft(p, "/")
}

// handlerName is the bare name of a handler function: listArticles for
// the method value (*module).listArticles, Index for a controller's verb.
// An anonymous function has no name worth reading and answers "".
func handlerName(h any) string {
	v := reflect.ValueOf(h)
	if !v.IsValid() || v.Kind() != reflect.Func || v.IsNil() {
		return ""
	}
	fn := goruntime.FuncForPC(v.Pointer())
	if fn == nil {
		return ""
	}
	name := strings.TrimSuffix(fn.Name(), "-fm")
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	if strings.HasPrefix(name, "func") || name == "" {
		return ""
	}
	if _, err := strconv.Atoi(name); err == nil {
		return ""
	}
	return name
}

// patternParam matches one wildcard of a Go 1.22 mux pattern: {name},
// {name...} and the {$} end anchor.
var patternParam = regexp.MustCompile(`\{([^}]*)\}`)

// openAPIPath turns a router pattern into an OpenAPI path and the names of
// its path parameters: {id} stays {id}, {rest...} becomes {rest}, and the
// {$} anchor disappears.
func openAPIPath(pattern string) (string, []string) {
	var params []string
	path := patternParam.ReplaceAllStringFunc(pattern, func(m string) string {
		name := strings.TrimSuffix(m[1:len(m)-1], "...")
		if name == "$" {
			return ""
		}
		params = append(params, name)
		return "{" + name + "}"
	})
	if path == "" {
		path = "/"
	}
	return path, params
}

// operationID names an operation after its handler when the handler has a
// name, else after its method and path (GET /api/articles/{id} →
// getApiArticlesId). The caller makes it unique.
func operationID(rt describedRoute, path string) string {
	if rt.Handler != "" {
		return lowerFirst(rt.Handler)
	}
	var b strings.Builder
	b.WriteString(strings.ToLower(rt.Method))
	for _, seg := range strings.FieldsFunc(path, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		b.WriteString(upperFirstRune(seg))
	}
	return b.String()
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	// An initialism stays lower as a whole: ListAPI → listAPI, API → api.
	if len(r) > 1 && unicode.IsUpper(r[0]) && unicode.IsUpper(r[1]) {
		i := 0
		for i < len(r) && unicode.IsUpper(r[i]) {
			i++
		}
		if i == len(r) {
			return strings.ToLower(s)
		}
		for j := 0; j < i-1; j++ {
			r[j] = unicode.ToLower(r[j])
		}
		return string(r)
	}
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func upperFirstRune(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// bearerSchemeName is the security scheme the derived document declares for
// the JWT bearer the application verifies.
const bearerSchemeName = "bearerAuth"

// apiDocumentTitle and apiDocumentVersion name the document after the
// application's main module when the base documents do not: the last
// element of its path, and its version when the binary was built from a
// tagged module.
func apiDocumentInfo() (string, string) {
	title, version := "Nucleus application", "0.0.0"
	if bi, ok := debug.ReadBuildInfo(); ok {
		if p := bi.Main.Path; p != "" {
			if i := strings.LastIndex(p, "/"); i >= 0 {
				p = p[i+1:]
			}
			title = p
		}
		if v := strings.TrimPrefix(bi.Main.Version, "v"); v != "" && v != "(devel)" {
			version = v
		}
	}
	return title, version
}

// deriveAPIDocument builds the document from what the modules registered.
// core supplies the security posture: whether a JWT verifier is configured,
// and the live enforcer that says which routes anonymous callers reach.
func deriveAPIDocument(core *app.App, inv *routeInventory) *openapi.Document {
	title, version := apiDocumentInfo()
	doc := openapi.NewDocument(title, version)

	open := core == nil || core.OpenAuthz() || core.Authorizer == nil
	secured := !open && core.JWT != nil
	if secured {
		doc.AddSecurityScheme(bearerSchemeName, openapi.BearerAuthScheme("JWT"))
		doc.Security = []openapi.SecurityRequirement{openapi.Require(bearerSchemeName)}
	}

	var routes []describedRoute
	if inv != nil {
		routes = append(routes, inv.described...)
	}
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return methodOrder(routes[i].Method) < methodOrder(routes[j].Method)
	})

	used := map[string]int{}
	for _, rt := range routes {
		path, params := openAPIPath(rt.Path)
		op := &openapi.Operation{Responses: map[string]openapi.Response{}}
		if rt.Module != "" {
			op.Tags = []string{rt.Module}
		}
		id := operationID(rt, path)
		used[id]++
		if n := used[id]; n > 1 {
			id += strconv.Itoa(n)
		}
		op.OperationID = id

		describeOperation(doc, op, rt, params, core != nil && core.ProblemDetails())

		if !open && core.Authorizer != nil && anonymousMay(core.Authorizer, rt.Method, path) {
			op.Security = openapi.PublicSecurity()
		}

		item := doc.Paths[path]
		if !item.SetOperation(rt.Method, op) {
			continue
		}
		doc.Paths[path] = item
	}
	return doc
}

// anonymousMay asks the enforcer whether a caller with no identity may run
// method on path — the question the default-deny middleware asks of every
// request, with the HTTP method mapped to the same action.
func anonymousMay(e *authz.Enforcer, method, path string) bool {
	return e.Can(authz.BootstrapSubject, samplePath(path), authz.ActionForMethod(method))
}

func methodOrder(m string) int {
	switch m {
	case http.MethodGet:
		return 0
	case http.MethodPost:
		return 1
	case http.MethodPut:
		return 2
	case http.MethodPatch:
		return 3
	case http.MethodDelete:
		return 4
	}
	return 5
}

// describeOperation fills the parameters, body and responses of op. A
// plain handler says nothing about what it reads or writes, so it gets its
// path parameters as strings and a default response that says so; a typed
// endpoint gets every parameter, its body and its response from its Go
// types.
func describeOperation(doc *openapi.Document, op *openapi.Operation, rt describedRoute, pathParams []string, problem bool) {
	ep := rt.Endpoint
	declared := map[string]bool{}
	if ep != nil && ep.In != nil {
		op.Summary, op.Description = ep.Summary, ep.Description
		in := ep.In
		for in.Kind() == reflect.Pointer {
			in = in.Elem()
		}
		if in.Kind() == reflect.Struct {
			for _, p := range inputParameters(doc, in) {
				declared[p.In+":"+p.Name] = true
				op.Parameters = append(op.Parameters, p)
			}
			if methodCarriesBody(rt.Method) {
				if body, ok := jsonBodySchema(doc, in); ok {
					op.RequestBody = openapi.JSONRequestBody(body, true)
				}
			}
		}
	} else if ep != nil {
		op.Summary, op.Description = ep.Summary, ep.Description
	}
	for _, name := range pathParams {
		if declared["path:"+name] {
			continue
		}
		op.Parameters = append(op.Parameters, openapi.PathParameter(name, openapi.Schema{Type: "string"}, ""))
	}

	if ep == nil {
		// Any status, any body: what the framework knows of a plain
		// handler's answer. Declared as */* with the empty schema rather
		// than as no content, which would claim the handler writes no
		// body at all.
		op.Responses["default"] = openapi.Response{
			Description: "Not described: the handler is registered as a plain function, so its response shape is not known to the framework.",
			Content:     map[string]openapi.MediaType{"*/*": {Schema: openapi.Schema{}}},
		}
		return
	}
	status := ep.Status
	if status == 0 {
		status = http.StatusOK
	}
	switch {
	case ep.Out == nil || status == http.StatusNoContent:
		op.Responses[strconv.Itoa(status)] = openapi.EmptyResponse(http.StatusText(status))
	default:
		op.Responses[strconv.Itoa(status)] = openapi.JSONResponse(http.StatusText(status), doc.SchemaFor(ep.Out))
	}
	op.Responses["default"] = openapi.ErrorResponses("Error: the framework's envelope, or RFC 9457 problem details", problem)
}

func methodCarriesBody(m string) bool {
	return m == http.MethodPost || m == http.MethodPut || m == http.MethodPatch
}

// inputParameters lists the path, query and header parameters of a typed
// endpoint's input struct: fields tagged path, query or header (the tags
// the router's binders read), with their schemas from the Go types and
// their validate tags.
func inputParameters(doc *openapi.Document, t reflect.Type) []openapi.Parameter {
	var out []openapi.Parameter
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct && f.Tag.Get("json") == "" {
				out = append(out, inputParameters(doc, ft)...)
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		for _, in := range []string{"path", "query", "header"} {
			name := strings.Split(f.Tag.Get(in), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			schema, required := doc.FieldSchema(f)
			// A path parameter is always required; a query parameter or a
			// header only when its validate tag says so (FieldSchema's
			// other reasons — no omitempty — describe JSON, not a URL).
			required = in == "path" || (required && hasValidateRule(f.Tag.Get("validate"), "required"))
			if in == "header" {
				name = http.CanonicalHeaderKey(name)
			}
			out = append(out, openapi.Parameter{Name: name, In: in, Required: required, Schema: schema, Description: f.Tag.Get("doc")})
		}
	}
	return out
}

// jsonBodySchema is the schema of the body a typed endpoint decodes: the
// input struct's fields that are not bound from the path, the query or a
// header. ok is false when every field comes from one of those (a body
// would be ignored).
func jsonBodySchema(doc *openapi.Document, t reflect.Type) (openapi.Schema, bool) {
	var fields []reflect.StructField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Tag.Get("path") != "" || f.Tag.Get("query") != "" || f.Tag.Get("header") != "" {
			continue
		}
		if !f.IsExported() || f.Tag.Get("json") == "-" {
			continue
		}
		fields = append(fields, f)
	}
	if len(fields) == 0 {
		return openapi.Schema{}, false
	}
	if len(fields) == t.NumField() {
		// The whole struct is the body: reference it by name.
		return doc.SchemaFor(t), true
	}
	return doc.SchemaFor(reflect.StructOf(fields)), true
}

func hasValidateRule(tag, rule string) bool {
	for _, r := range strings.Split(tag, ",") {
		if k, _, _ := strings.Cut(strings.TrimSpace(r), "="); k == rule {
			return true
		}
		if strings.TrimSpace(r) == "dive" {
			return false
		}
	}
	return false
}

// mergeAPIDocuments lays the derived document over the bases: what a base
// declares is kept as written, the derivation fills what no base mentions.
func mergeAPIDocuments(derived *openapi.Document, bases []json.Marshaler) (*openapi.Document, error) {
	if len(bases) == 0 {
		return derived, nil
	}
	out := openapi.NewDocument(derived.Info.Title, derived.Info.Version)
	titled := false
	for i, b := range bases {
		raw, err := b.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("nucleus: OpenAPI base document %d: %w", i, err)
		}
		var base openapi.Document
		if err := json.Unmarshal(raw, &base); err != nil {
			return nil, fmt.Errorf("nucleus: OpenAPI base document %d is not an OpenAPI document: %w", i, err)
		}
		if !titled && base.Info.Title != "" {
			out.Info = base.Info
			titled = true
		}
		out.Servers = append(out.Servers, base.Servers...)
		if len(out.Security) == 0 {
			out.Security = base.Security
		}
		for p, item := range base.Paths {
			merged := out.Paths[p]
			for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
				if op := item.Operation(m); op != nil && merged.Operation(m) == nil {
					merged.SetOperation(m, op)
				}
			}
			out.Paths[p] = merged
		}
		for name, s := range base.Components.Schemas {
			if _, ok := out.Components.Schemas[name]; !ok {
				out.AddSchema(name, s)
			}
		}
		for name, s := range base.Components.SecuritySchemes {
			if _, ok := out.Components.SecuritySchemes[name]; !ok {
				out.AddSecurityScheme(name, s)
			}
		}
	}

	if len(out.Security) == 0 {
		out.Security = derived.Security
	}
	for name, s := range derived.Components.SecuritySchemes {
		if _, ok := out.Components.SecuritySchemes[name]; !ok {
			out.AddSecurityScheme(name, s)
		}
	}
	for name, s := range derived.Components.Schemas {
		if _, ok := out.Components.Schemas[name]; !ok {
			out.AddSchema(name, s)
		}
	}
	for p, item := range derived.Paths {
		merged := out.Paths[p]
		for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			dop := item.Operation(m)
			if dop == nil {
				continue
			}
			bop := merged.Operation(m)
			if bop == nil {
				merged.SetOperation(m, dop)
				continue
			}
			// The base wrote this operation; it keeps every word of it.
			// What it left out and the derivation knows — the
			// operationId, the tag, the security the policy enforces —
			// is filled in, so a hand-written contract stops claiming an
			// open API by omission (NU-97).
			if bop.OperationID == "" {
				bop.OperationID = dop.OperationID
			}
			if len(bop.Tags) == 0 {
				bop.Tags = dop.Tags
			}
			if bop.Security == nil {
				bop.Security = dop.Security
			}
		}
		out.Paths[p] = merged
	}
	return out, nil
}

// buildAPIDocument derives the document and merges the bases under it;
// the JSON is what the route serves and what NUCLEUS_PRINT_ROUTES prints,
// and the parsed document is what request validation reads.
func buildAPIDocument(core *app.App, inv *routeInventory, spec *APIDocumentSpec) ([]byte, error) {
	doc := deriveAPIDocument(core, inv)
	if spec != nil {
		merged, err := mergeAPIDocuments(doc, spec.Base)
		if err != nil {
			return nil, err
		}
		doc = merged
	}
	body, err := openapi.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if inv != nil {
		// The validator reads the document as served: decoded back from
		// the bytes, so what it enforces is exactly what a client reads.
		var served openapi.Document
		if err := json.Unmarshal(body, &served); err != nil {
			return nil, err
		}
		inv.document = &served
	}
	return body, nil
}

// apiDocumentPattern normalises the pattern the way the mount does.
func apiDocumentPattern(spec *APIDocumentSpec) string {
	if spec == nil {
		return ""
	}
	p := strings.TrimSpace(spec.Pattern)
	if p == "" {
		p = "/openapi.json"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// mountAPIDocument serves the document at the spec's pattern and opens the
// route to anonymous callers: a client generator, a gateway or a browser
// fetches it before it has any credential.
func mountAPIDocument(core *app.App, spec *APIDocumentSpec, body []byte) error {
	pattern := apiDocumentPattern(spec)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(body)
	})
	if err := core.MountOpenAPIHandler(pattern, handler); err != nil {
		return fmt.Errorf("nucleus: serving the derived OpenAPI document: %w", err)
	}
	if core.Authorizer != nil {
		if err := core.Authorizer.AddPolicy(authz.BootstrapSubject, pattern, "read"); err != nil {
			return fmt.Errorf("nucleus: opening the OpenAPI document route to anonymous callers: %w", err)
		}
	}
	return nil
}
