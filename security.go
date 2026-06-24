package actions

// SecurityScheme describes one OpenAPI security scheme, emitted under
// components.securitySchemes. Declare schemes with WithSecurityScheme and
// reference them by name from WithSecurity (registry-wide) or Action.Security
// (per operation). The BearerAuth and APIKeyAuth constructors cover the common
// cases.
type SecurityScheme struct {
	Type         string // "apiKey" | "http" | "oauth2" | "openIdConnect"
	Description  string
	Name         string // apiKey: the header/query/cookie parameter name
	In           string // apiKey: "header" | "query" | "cookie"
	Scheme       string // http: "bearer" | "basic"
	BearerFormat string // http bearer: a hint such as "JWT"
}

// BearerAuth returns an HTTP bearer-token scheme. bearerFormat is an optional
// hint (e.g. "JWT") and may be "".
func BearerAuth(bearerFormat string) SecurityScheme {
	return SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: bearerFormat}
}

// APIKeyAuth returns an apiKey scheme carried in the given location ("header",
// "query", or "cookie") under the given parameter name.
func APIKeyAuth(in, name string) SecurityScheme {
	return SecurityScheme{Type: "apiKey", In: in, Name: name}
}

// toMap renders the scheme as an OpenAPI object, omitting empty fields.
func (s SecurityScheme) toMap() map[string]any {
	m := map[string]any{"type": s.Type}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if s.Name != "" {
		m["name"] = s.Name
	}
	if s.In != "" {
		m["in"] = s.In
	}
	if s.Scheme != "" {
		m["scheme"] = s.Scheme
	}
	if s.BearerFormat != "" {
		m["bearerFormat"] = s.BearerFormat
	}
	return m
}

// SecurityRequirement maps a security-scheme name to the scopes it requires.
// For apiKey and http schemes the scope list is empty, e.g.
// SecurityRequirement{"ApiKeyAuth": nil}.
type SecurityRequirement map[string][]string

// securityToList renders security requirements as an OpenAPI security array.
func securityToList(reqs []SecurityRequirement) []any {
	out := make([]any, len(reqs))
	for i, req := range reqs {
		m := make(map[string]any, len(req))
		for name, scopes := range req {
			if scopes == nil {
				scopes = []string{}
			}
			m[name] = scopes
		}
		out[i] = m
	}
	return out
}

// Server is one entry of the OpenAPI servers[] block. Declare them with
// WithServers so generated clients know the base URLs.
type Server struct {
	URL         string
	Description string
}
