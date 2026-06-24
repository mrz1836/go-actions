package actions

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
)

// Action is one route declared once. Req and Resp are the typed request and
// response. A zero-field Req is allowed; Resp may be Empty, Created[T],
// Accepted[T], or any struct.
type Action[Req, Resp any] struct {
	ID          string // operationId, dotted snake — unique across the registry
	Method      string // "GET", "POST", "PATCH", ...
	Path        string // chi-style, e.g. "/persons/{id}"
	Summary     string
	Description string
	Tags        []string
	Statuses    []StatusDoc
	Handle      func(ctx context.Context, req Req) (Resp, error)
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
	reqType     reflect.Type
	respType    reflect.Type
	handler     http.HandlerFunc
}

// Registry stores registered actions and, after Freeze, the pre-built
// self-documentation artifacts.
type Registry struct {
	mu      sync.Mutex
	actions []anyAction
	frozen  bool

	info        openAPIInfo
	errorMapper ErrorMapper
	stripPrefix string

	openapiJSON   []byte
	openapiYAML   []byte
	indexHTML     []byte
	indexMarkdown []byte
}

// NewRegistry returns an empty Registry configured by the given options. With no
// options it uses a neutral OpenAPI info block, the default ErrorMapper, and
// mounts actions at their declared paths (no prefix stripping).
func NewRegistry(opts ...Option) *Registry {
	r := &Registry{
		info:        defaultInfo(),
		errorMapper: defaultErrorMapper,
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
		if verr := validateRequest(req); verr != nil {
			reg.writeError(w, r, verr)
			return
		}
		resp, herr := handle(r.Context(), req)
		if herr != nil {
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
	for _, a := range r.actions {
		mux.Method(a.method, r.mountPath(a.path), a.handler)
	}
	mux.Get("/openapi.json", r.serveBytes("application/json", r.openapiJSON))
	mux.Get("/openapi.yaml", r.serveBytes("application/yaml", r.openapiYAML))
	mux.Get("/_actions", r.serveIndex)
	return mux
}

// OpenAPIJSON returns the pre-built OpenAPI 3.1 JSON bytes. Valid after Freeze.
func (r *Registry) OpenAPIJSON() []byte { return r.openapiJSON }

// OpenAPIYAML returns the pre-built OpenAPI 3.1 YAML bytes. Valid after Freeze.
func (r *Registry) OpenAPIYAML() []byte { return r.openapiYAML }

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
