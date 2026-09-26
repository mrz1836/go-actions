package actions

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Action is one route declared once. Req and Resp are the typed request and
// response. A zero-field Req is allowed; Resp may be Empty, Created[T],
// Accepted[T], Response[T], or any struct.
type Action[Req, Resp any] struct {
	ID          string // operationId, dotted snake — unique across the registry
	Method      string // "GET", "POST", "PATCH", ... (case-insensitive; uppercased at Register)
	Path        string // chi-style, e.g. "/persons/{id}"
	Summary     string
	Description string
	Tags        []string
	Statuses    []StatusDoc

	// Deprecated marks the operation deprecated in the OpenAPI document.
	Deprecated bool
	// Security lists the security requirements for this operation, overriding any
	// registry-wide WithSecurity for this action. Each entry maps a scheme name
	// (declared via WithSecurityScheme) to its required scopes. A nil slice
	// inherits the registry-wide default; a non-nil slice overrides it, and an
	// explicitly empty slice ([]SecurityRequirement{}) marks the operation public
	// — it emits "security: []" so the operation opts out of any global
	// requirement.
	Security []SecurityRequirement
	// Timeout, when > 0, bounds the request context with a deadline. A handler
	// that returns an error once the context's deadline has passed yields a
	// 504 TIMEOUT, whatever error it returned; one that returns successfully is
	// encoded as usual. The handler must honor ctx for the deadline to cut it
	// short.
	Timeout time.Duration
	// Middleware wraps only this action's handler; the first entry is the
	// outermost. It runs inside the framework's per-action layers (observer,
	// panic recovery, timeout, body cap) and before the request is decoded. Use
	// it for per-route concerns such as authentication on a single endpoint.
	Middleware []Middleware

	// Handle serves the decoded, validated request. It must not be nil.
	Handle func(ctx context.Context, req Req) (Resp, error)
}

// StatusDoc documents one outcome of an action.
type StatusDoc struct {
	Code        int
	Description string
	Error       bool
	// Headers documents the response headers this outcome sets, such as
	// Retry-After on a 429. Each becomes an entry in the response's OpenAPI
	// headers object. Optional.
	Headers []HeaderDoc
}

// HeaderDoc documents one response header of an action outcome.
type HeaderDoc struct {
	// Name is the header name as written on the wire, e.g. "Retry-After".
	Name string
	// Description explains the header's value.
	Description string
	// Type is the JSON Schema type of the value ("string", "integer", ...);
	// empty means "string".
	Type string
	// Required marks a header the outcome always sets.
	Required bool
}

// Empty is a Resp marker — the encoder emits 204 No Content.
type Empty struct{}

// Created wraps a Resp body — the encoder emits 201 Created.
type Created[T any] struct{ Body T }

// Accepted wraps a Resp body — the encoder emits 202 Accepted.
type Accepted[T any] struct{ Body T }

// Response wraps a body with an explicit status and optional response headers,
// for handlers that need control beyond Created/Accepted/Empty — e.g. setting
// Cache-Control or ETag, or returning a non-standard 2xx. Status defaults to 200
// when zero. Headers are added to the response before the body is written. For
// OpenAPI schema generation it unwraps to its Body type; because its status is
// chosen at runtime, Freeze cannot check that the status is documented.
type Response[T any] struct {
	Status int
	Header http.Header
	Body   T
}

// Page is a conventional cursor-paginated list body: a slice of items plus an
// opaque next cursor and a has-more flag. It is an ordinary 200 response body,
// offered so paginated endpoints across services share one wire shape.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// ListMeta carries count metadata for a List response body.
type ListMeta struct {
	// Total is the number of items in the result set.
	Total int `json:"total"`
}

// List is a conventional non-paginated collection body: a slice of items plus a
// metadata block carrying the total count. Like Page it is an ordinary 200
// response body, offered so simple list endpoints across services share one
// wire shape — {"items": [...], "meta": {"total": N}}. Use Page instead when the
// collection is cursor-paginated. Construct it with NewList.
type List[T any] struct {
	Items []T      `json:"items"`
	Meta  ListMeta `json:"meta"`
}

// NewList wraps items in a List with Meta.Total set to len(items). A nil slice
// is normalized to an empty slice so the body encodes "items": [] rather than
// "items": null.
func NewList[T any](items []T) List[T] {
	if items == nil {
		items = []T{}
	}
	return List[T]{Items: items, Meta: ListMeta{Total: len(items)}}
}

// anyAction is the type-erased registry entry: action metadata, the reflected
// request/response types for schema generation, and the built handler.
type anyAction struct {
	id          string
	method      string
	path        string
	summary     string
	description string
	tags        []string
	statuses    []StatusDoc
	deprecated  bool
	security    []SecurityRequirement
	timeout     time.Duration
	middleware  []Middleware
	reqType     reflect.Type
	respType    reflect.Type
	hasHandle   bool
	handler     http.HandlerFunc
}

// defaultMaxBodyBytes is the request-body cap applied unless WithMaxBodyBytes
// overrides it (or sets 0 for unlimited). 1 MiB suits JSON request bodies.
const defaultMaxBodyBytes = 1 << 20

// defaultOpenAPIVersion is the OpenAPI dialect declared unless overridden.
const defaultOpenAPIVersion = "3.1.0"

// Registry stores registered actions and, after Freeze, the pre-built
// self-documentation artifacts.
type Registry struct {
	mu      sync.Mutex
	actions []anyAction
	frozen  bool

	info        openAPIInfo
	errorMapper ErrorMapper
	stripPrefix string
	// errorCodes are the extra codes from WithErrorCodes; nil when it was not
	// used, which leaves the Error schema's code un-enumerated.
	errorCodes []string

	// runtime configuration
	middleware       []Middleware
	maxBodyBytes     int64
	strictDecoding   bool
	observer         ObserveFunc
	requestIDGen     func() string
	notFound         http.Handler
	methodNotAllowed http.Handler

	// OpenAPI contract configuration
	openapiVersion  string
	servers         []Server
	securitySchemes map[string]SecurityScheme
	security        []SecurityRequirement

	openapiJSON   []byte
	openapiYAML   []byte
	indexHTML     []byte
	indexMarkdown []byte
}

// NewRegistry returns an empty Registry configured by the given options. With no
// options it uses a neutral OpenAPI info block, the default ErrorMapper, a 1 MiB
// request-body cap, panic recovery, request-id propagation, and JSON 404/405
// responses, and mounts actions at their declared paths (no prefix stripping).
func NewRegistry(opts ...Option) *Registry {
	r := &Registry{
		info:           defaultInfo(),
		errorMapper:    defaultErrorMapper,
		maxBodyBytes:   defaultMaxBodyBytes,
		openapiVersion: defaultOpenAPIVersion,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register is the only typed seam. It uppercases the action's Method, compiles
// the request's binding and validation plans once, builds the http.HandlerFunc
// (decode → validate → Handle → encode), and stores it for Freeze to check. It
// panics if the registry is already frozen; every other declaration error is
// reported by Freeze.
func Register[Req, Resp any](reg *Registry, a Action[Req, Resp]) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.frozen {
		panic("actions: Register called after Freeze")
	}

	method := strings.ToUpper(a.Method)
	reqType := reflect.TypeFor[Req]()
	binder := binderFor(reqType)
	var val *validator
	if st := structType(reqType); st != nil {
		val = validatorFor(st)
	}
	withBody := methodHasBody(method)
	handle := a.Handle
	strict := reg.strictDecoding
	handler := func(w http.ResponseWriter, r *http.Request) {
		var req Req
		rv := reflect.ValueOf(&req).Elem()
		if err := binder.bind(r, rv, withBody, strict); err != nil {
			reg.writeError(w, r, err)
			return
		}
		if val != nil {
			for rv.Kind() == reflect.Pointer {
				rv = rv.Elem() // allocated by bind
			}
			if verr := val.validate(rv, nil); verr != nil {
				reg.writeError(w, r, verr)
				return
			}
		}
		resp, herr := handle(r.Context(), req)
		if herr != nil {
			// A handler that honored a per-action Timeout and overran reports the
			// deadline on its context; surface that as a 504 regardless of what
			// the handler returned.
			if errors.Is(r.Context().Err(), context.DeadlineExceeded) {
				herr = &APIError{Status: http.StatusGatewayTimeout, Code: CodeTimeout, Message: "request timed out"}
			}
			reg.writeError(w, r, herr)
			return
		}
		encodeResponse(w, resp)
	}

	reg.actions = append(reg.actions, anyAction{
		id:          a.ID,
		method:      method,
		path:        a.Path,
		summary:     a.Summary,
		description: a.Description,
		tags:        a.Tags,
		statuses:    a.Statuses,
		deprecated:  a.Deprecated,
		security:    a.Security,
		timeout:     a.Timeout,
		middleware:  a.Middleware,
		reqType:     reqType,
		respType:    reflect.TypeFor[Resp](),
		hasHandle:   handle != nil,
		handler:     handler,
	})
}

// structType returns t, or the type it points to, when that is a struct, and
// nil otherwise.
func structType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	return t
}

// Freeze seals the registry. It validates every action declaration, then builds
// the OpenAPI document and the _actions index; after Freeze, Register panics.
// Calling Freeze again is a no-op. It panics on:
//
//   - an empty ID or Method, or a Path that does not start with "/";
//   - no Statuses, a nil Handle, or a HeaderDoc with an empty Name;
//   - a Resp whose success status (204 for Empty, 201 for Created, 202 for
//     Accepted, 200 otherwise) is not documented by a non-error StatusDoc —
//     Response[T], whose status is chosen at runtime, is exempt;
//   - a duplicate ID, or two actions routing the same Method and path;
//   - two distinct types sharing a component schema name, a type named Error,
//     or a <Name>Input name already taken (see the schema generator).
func (r *Registry) Freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return
	}

	r.validateActions()
	slices.SortFunc(r.actions, func(a, b anyAction) int { return cmp.Compare(a.id, b.id) })
	r.buildArtifacts()
	r.frozen = true
}

// Handler returns the http.Handler mounting every action plus the three
// self-documentation endpoints (/openapi.json, /openapi.yaml, /_actions). It
// panics if the registry is not frozen.
func (r *Registry) Handler() http.Handler {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.frozen {
		panic("actions: Handler called before Freeze")
	}

	mux := chi.NewRouter()
	mux.NotFound(r.notFoundHandler().ServeHTTP)
	mux.MethodNotAllowed(r.methodNotAllowedHandler().ServeHTTP)

	for _, a := range r.actions {
		mux.Method(a.method, r.mountPath(a.path), r.wrapAction(a))
	}
	mux.Get("/openapi.json", r.serveBytes("application/json", r.openapiJSON))
	mux.Get("/openapi.yaml", r.serveBytes("application/yaml", r.openapiYAML))
	mux.Get("/_actions", r.serveIndex)

	// Mux-level middleware wraps every route (actions, self-docs, 404/405):
	// caller middleware outermost, then request-id propagation, then a panic
	// backstop, then the router.
	muxMW := make([]Middleware, 0, len(r.middleware)+2)
	muxMW = append(muxMW, r.middleware...)
	muxMW = append(muxMW, r.requestIDMiddleware, r.recoverMiddleware)
	return chain(mux, muxMW...)
}

// OpenAPIJSON returns the pre-built OpenAPI document, in the declared dialect
// (see WithOpenAPIVersion), as indented JSON. It is nil before Freeze.
func (r *Registry) OpenAPIJSON() []byte { return r.openapiJSON }

// OpenAPIYAML returns the same OpenAPI document as OpenAPIJSON, serialized as
// YAML. It is nil before Freeze.
func (r *Registry) OpenAPIYAML() []byte { return r.openapiYAML }

// wrapAction composes one action's handler with its framework and per-action
// middleware, outermost-first: observe (if configured) → recover → timeout (if
// set) → body cap (if set) → the action's own Middleware → the typed handler.
func (r *Registry) wrapAction(a anyAction) http.Handler {
	mw := make([]Middleware, 0, 4+len(a.middleware))
	if r.observer != nil {
		mw = append(mw, r.observeMiddleware(a.id, a.method, a.path))
	}
	mw = append(mw, r.recoverMiddleware)
	if a.timeout > 0 {
		mw = append(mw, timeoutMiddleware(a.timeout))
	}
	if r.maxBodyBytes > 0 {
		mw = append(mw, r.maxBytesMiddleware)
	}
	mw = append(mw, a.middleware...)
	return chain(a.handler, mw...)
}

// notFoundHandler returns the configured 404 handler, or a JSON default.
func (r *Registry) notFoundHandler() http.Handler {
	if r.notFound != nil {
		return r.notFound
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.writeAPIError(w, req, APIError{
			Status:  http.StatusNotFound,
			Code:    CodeNotFound,
			Message: "resource not found",
		})
	})
}

// methodNotAllowedHandler returns the configured 405 handler, or a JSON default.
func (r *Registry) methodNotAllowedHandler() http.Handler {
	if r.methodNotAllowed != nil {
		return r.methodNotAllowed
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.writeAPIError(w, req, APIError{
			Status:  http.StatusMethodNotAllowed,
			Code:    CodeMethodNotAllowed,
			Message: "method not allowed",
		})
	})
}

// validateActions panics on any structurally invalid or conflicting action.
func (r *Registry) validateActions() {
	seenID := make(map[string]bool, len(r.actions))
	seenRoute := make(map[string]string, len(r.actions))
	for _, a := range r.actions {
		switch {
		case a.id == "":
			panic("actions: an action has an empty ID")
		case a.method == "":
			panic(fmt.Sprintf("actions: action %q has an empty Method", a.id))
		case !strings.HasPrefix(a.path, "/"):
			panic(fmt.Sprintf("actions: action %q has an invalid Path %q", a.id, a.path))
		case len(a.statuses) == 0:
			panic(fmt.Sprintf("actions: action %q documents no Statuses", a.id))
		case !a.hasHandle:
			panic(fmt.Sprintf("actions: action %q has a nil Handle", a.id))
		}
		validateHeaderDocs(a)
		validateSuccessStatus(a)
		if seenID[a.id] {
			panic(fmt.Sprintf("actions: duplicate action ID %q", a.id))
		}
		seenID[a.id] = true

		// Compare mounted paths: with WithStripPrefix, "/v1/x" and "/x" route
		// alike and chi would silently keep only one.
		route := a.method + " " + r.mountPath(a.path)
		if other, dup := seenRoute[route]; dup {
			panic(fmt.Sprintf("actions: duplicate route %q (actions %q and %q)", route, other, a.id))
		}
		seenRoute[route] = a.id
	}
}

// headerEnvelopeType is the reflect.Type of the headerEnvelope interface,
// implemented only by Response[T].
//
//nolint:gochecknoglobals // a cached reflect.Type is an intentional package global
var headerEnvelopeType = reflect.TypeFor[headerEnvelope]()

// successStatus returns the status the encoder writes for a successful
// response of type t, and false when that status is chosen at runtime: by a
// Response[T], or by the dynamic type behind an interface.
func successStatus(t reflect.Type) (int, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t.Kind() == reflect.Interface, t.Implements(headerEnvelopeType):
		return 0, false
	case t.Implements(envelopeType):
		env, _ := reflect.Zero(t).Interface().(responseEnvelope)
		return env.envelopeStatus(), true
	default:
		return http.StatusOK, true
	}
}

// validateSuccessStatus panics when an action's success status is not
// documented by a non-error StatusDoc, so the contract states the status the
// encoder actually writes.
func validateSuccessStatus(a anyAction) {
	status, fixed := successStatus(a.respType)
	if !fixed {
		return
	}
	for _, sd := range a.statuses {
		if sd.Code == status && !sd.Error {
			return
		}
	}
	panic(fmt.Sprintf("actions: action %q responds %d on success (Resp %s) but documents no non-error %d StatusDoc",
		a.id, status, a.respType, status))
}

// validateHeaderDocs panics on a documented response header without a name.
func validateHeaderDocs(a anyAction) {
	for _, sd := range a.statuses {
		for _, h := range sd.Headers {
			if h.Name == "" {
				panic(fmt.Sprintf("actions: action %q documents a %d response header with an empty Name", a.id, sd.Code))
			}
		}
	}
}

// serveBytes returns a handler writing pre-built bytes with the given type.
func (r *Registry) serveBytes(contentType string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType+"; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// mountPath maps a declared action path to its router path. By default it is the
// identity; when a strip prefix is configured (WithStripPrefix), that namespace
// prefix is removed so a Registry mounted under the prefix resolves an action
// declared with the prefix in its Path.
func (r *Registry) mountPath(p string) string {
	if r.stripPrefix == "" {
		return p
	}
	trimmed := strings.TrimPrefix(p, r.stripPrefix)
	if trimmed == "" {
		return "/"
	}
	return trimmed
}
