package actions

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
