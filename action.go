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
	Method      string // "GET", "POST", "PATCH", ...
	Path        string // chi-style, e.g. "/persons/{id}"
	Summary     string
	Description string
	Tags        []string
	Statuses    []StatusDoc

	// Deprecated marks the operation deprecated in the OpenAPI document.
	Deprecated bool
	// Security lists the security requirements for this operation, overriding any
	// registry-wide WithSecurity for this action. Each entry maps a scheme name
	// (declared via WithSecurityScheme) to its required scopes.
	Security []SecurityRequirement
	// Timeout, when > 0, bounds the handler with a context deadline; a handler
	// that honors ctx and overruns yields a 504.
	Timeout time.Duration
	// Middleware wraps only this action's handler, innermost-first. Use it for
	// per-route concerns such as authentication on a single endpoint.
	Middleware []Middleware

	Handle func(ctx context.Context, req Req) (Resp, error)
}

// StatusDoc documents one outcome of an action.
type StatusDoc struct {
	Code        int
	Description string
	Error       bool
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
// when zero. For OpenAPI schema generation it unwraps to its Body type.
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

	// runtime configuration
	middleware       []Middleware
	maxBodyBytes     int64
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

// Register is the only typed seam. It builds the http.HandlerFunc
// (decode → validate → Handle → encode) and stores it keyed by ID. It panics if
// the registry is already frozen.
func Register[Req, Resp any](reg *Registry, a Action[Req, Resp]) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.frozen {
		panic("actions: Register called after Freeze")
	}

	handle := a.Handle
	handler := func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeRequest[Req](r)
		if err != nil {
			reg.writeError(w, r, err)
			return
		}
		if verr := validateRequest(&req); verr != nil {
			reg.writeError(w, r, verr)
			return
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
		method:      a.Method,
		path:        a.Path,
		summary:     a.Summary,
		description: a.Description,
		tags:        a.Tags,
		statuses:    a.Statuses,
		deprecated:  a.Deprecated,
		security:    a.Security,
		timeout:     a.Timeout,
		middleware:  a.Middleware,
		reqType:     reflect.TypeFor[Req](),
		respType:    reflect.TypeFor[Resp](),
		handler:     handler,
	})
}

// Freeze seals the registry. It validates every action declaration and panics
// on a malformed action, a duplicate ID, or a duplicate Method+Path, then builds
// the OpenAPI document and the _actions index. After Freeze, Register panics.
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

// OpenAPIJSON returns the pre-built OpenAPI 3.1 JSON bytes. Valid after Freeze.
func (r *Registry) OpenAPIJSON() []byte { return r.openapiJSON }

// OpenAPIYAML returns the pre-built OpenAPI 3.1 YAML bytes. Valid after Freeze.
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
	seenRoute := make(map[string]bool, len(r.actions))
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
		}
		if seenID[a.id] {
			panic(fmt.Sprintf("actions: duplicate action ID %q", a.id))
		}
		seenID[a.id] = true

		route := a.method + " " + a.path
		if seenRoute[route] {
			panic(fmt.Sprintf("actions: duplicate route %q", route))
		}
		seenRoute[route] = true
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
