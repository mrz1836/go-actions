package actions

import "net/http"

// openAPIInfo is the caller-configurable OpenAPI info block. It also seeds the
// title shown on the browsable _actions index.
type openAPIInfo struct {
	title       string
	description string
	version     string
}

// defaultInfo returns the neutral, generic OpenAPI info used when WithInfo is
// not supplied. It carries no project- or domain-specific naming.
func defaultInfo() openAPIInfo {
	return openAPIInfo{
		title:       "API",
		description: "This contract is generated from the action registry.",
		version:     "1.0.0",
	}
}

// Option configures a Registry at construction time.
type Option func(*Registry)

// WithInfo sets the OpenAPI info block (title, description, version). The title
// also names the browsable _actions index. Empty arguments leave the
// corresponding default in place.
func WithInfo(title, description, version string) Option {
	return func(r *Registry) {
		if title != "" {
			r.info.title = title
		}
		if description != "" {
			r.info.description = description
		}
		if version != "" {
			r.info.version = version
		}
	}
}

// WithErrorMapper installs a custom ErrorMapper, replacing the default generic
// mapping. A nil mapper is ignored.
func WithErrorMapper(m ErrorMapper) Option {
	return func(r *Registry) {
		if m != nil {
			r.errorMapper = m
		}
	}
}

// WithStripPrefix configures a namespace prefix to strip from each action's
// declared Path when building the router. Use it when the Registry is mounted
// under that same prefix (e.g. WithStripPrefix("/v1") for a Registry mounted at
// "/v1" whose actions are declared at "/v1/..."). By default no prefix is
// stripped and actions route at their declared Path.
func WithStripPrefix(prefix string) Option {
	return func(r *Registry) {
		r.stripPrefix = prefix
	}
}

// WithMiddleware appends registry-wide middleware applied to every route — the
// actions, the self-documentation endpoints, and the 404/405 responses. The
// first middleware is outermost. It composes with chi and any net/http
// middleware. Caller middleware runs outside the framework's request-id and
// panic-recovery layers.
func WithMiddleware(mw ...Middleware) Option {
	return func(r *Registry) {
		r.middleware = append(r.middleware, mw...)
	}
}

// WithMaxBodyBytes caps the request body size; a larger body is rejected with a
// 413. The default is 1 MiB. Pass 0 for no limit. Negative values are ignored.
func WithMaxBodyBytes(n int64) Option {
	return func(r *Registry) {
		if n >= 0 {
			r.maxBodyBytes = n
		}
	}
}

// WithObserver installs a per-request observability hook invoked after each
// action completes with its id, status, latency, and error (panics included).
// It is the seam for access logging, metrics, and tracing. A nil hook is
// ignored.
func WithObserver(fn ObserveFunc) Option {
	return func(r *Registry) {
		if fn != nil {
			r.observer = fn
		}
	}
}

// WithRequestIDGenerator overrides how a correlation id is minted when a request
// arrives without an X-Request-ID / X-Amzn-Request-Id header. The default emits
// a UUIDv4. A nil generator is ignored.
func WithRequestIDGenerator(gen func() string) Option {
	return func(r *Registry) {
		if gen != nil {
			r.requestIDGen = gen
		}
	}
}

// WithNotFoundHandler overrides the default JSON 404 response for unmatched
// routes. A nil handler is ignored.
func WithNotFoundHandler(h http.Handler) Option {
	return func(r *Registry) {
		if h != nil {
			r.notFound = h
		}
	}
}

// WithMethodNotAllowedHandler overrides the default JSON 405 response for a
// known path reached with an unsupported method. A nil handler is ignored.
func WithMethodNotAllowedHandler(h http.Handler) Option {
	return func(r *Registry) {
		if h != nil {
			r.methodNotAllowed = h
		}
	}
}

// WithServers sets the OpenAPI servers[] block so generated clients know the
// base URLs the contract is served at.
func WithServers(servers ...Server) Option {
	return func(r *Registry) {
		r.servers = append(r.servers, servers...)
	}
}

// WithSecurityScheme registers a named OpenAPI security scheme under
// components.securitySchemes. Reference it from WithSecurity or Action.Security.
func WithSecurityScheme(name string, scheme SecurityScheme) Option {
	return func(r *Registry) {
		if r.securitySchemes == nil {
			r.securitySchemes = map[string]SecurityScheme{}
		}
		r.securitySchemes[name] = scheme
	}
}

// WithSecurity sets the registry-wide default security requirements applied to
// every operation that does not declare its own Action.Security.
func WithSecurity(reqs ...SecurityRequirement) Option {
	return func(r *Registry) {
		r.security = append(r.security, reqs...)
	}
}

// WithOpenAPIVersion selects the declared OpenAPI dialect: "3.1.0" (default) or
// "3.0.3". The generated schema fragments are a common subset valid under both;
// only the declared version string changes. An unsupported value panics at
// construction.
func WithOpenAPIVersion(v string) Option {
	return func(r *Registry) {
		switch v {
		case "3.0.3", "3.1.0":
			r.openapiVersion = v
		default:
			panic("actions: WithOpenAPIVersion supports \"3.1.0\" or \"3.0.3\", got " + v)
		}
	}
}
