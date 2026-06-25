package actions_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/go-actions"
)

func TestOpenAPI_Security(t *testing.T) {
	t.Parallel()
	apiKeyScheme := actions.APIKeyAuth("header", "X-API-Key")
	apiKeyScheme.Description = "Project API key"
	reg := actions.NewRegistry(
		actions.WithSecurityScheme("ApiKeyAuth", apiKeyScheme),
		actions.WithSecurityScheme("BearerAuth", actions.BearerAuth("JWT")),
		actions.WithSecurity(actions.SecurityRequirement{"ApiKeyAuth": nil}),
	)
	a := pingAction() // POST /ping
	a.Security = []actions.SecurityRequirement{{"BearerAuth": {"admin"}}}
	actions.Register(reg, a)
	reg.Freeze()

	var doc map[string]any
	require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &doc))

	components, _ := doc["components"].(map[string]any)
	schemes, _ := components["securitySchemes"].(map[string]any)
	require.Contains(t, schemes, "ApiKeyAuth")
	require.Contains(t, schemes, "BearerAuth")

	apiKey, _ := schemes["ApiKeyAuth"].(map[string]any)
	assert.Equal(t, "apiKey", apiKey["type"])
	assert.Equal(t, "header", apiKey["in"])
	assert.Equal(t, "X-API-Key", apiKey["name"])

	bearer, _ := schemes["BearerAuth"].(map[string]any)
	assert.Equal(t, "http", bearer["type"])
	assert.Equal(t, "bearer", bearer["scheme"])
	assert.Equal(t, "JWT", bearer["bearerFormat"])

	// Registry-wide default security at the document root.
	topSec, _ := doc["security"].([]any)
	require.Len(t, topSec, 1)

	// Per-action security overrides the default at the operation.
	paths, _ := doc["paths"].(map[string]any)
	post, _ := paths["/ping"].(map[string]any)["post"].(map[string]any)
	opSec, _ := post["security"].([]any)
	require.Len(t, opSec, 1)
	first, _ := opSec[0].(map[string]any)
	scopes, _ := first["BearerAuth"].([]any)
	require.Len(t, scopes, 1)
	assert.Equal(t, "admin", scopes[0])
}

func TestOpenAPI_SecurityInheritAndPublicOverride(t *testing.T) {
	t.Parallel()
	reg := actions.NewRegistry(
		actions.WithSecurityScheme("BearerAuth", actions.BearerAuth("JWT")),
		actions.WithSecurity(actions.SecurityRequirement{"BearerAuth": nil}),
	)

	// A nil Security inherits the registry-wide default (no per-op security key).
	inherit := pingAction() // POST /ping
	actions.Register(reg, inherit)

	// An explicitly empty Security marks the operation public: "security: []".
	public := panicAction() // POST /panic
	public.Security = []actions.SecurityRequirement{}
	actions.Register(reg, public)

	reg.Freeze()

	var doc map[string]any
	require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &doc))
	paths, _ := doc["paths"].(map[string]any)

	inheritOp, _ := paths["/ping"].(map[string]any)["post"].(map[string]any)
	_, hasSecurity := inheritOp["security"]
	assert.False(t, hasSecurity, "a nil Security must omit the operation security key (inherit global)")

	publicOp, _ := paths["/panic"].(map[string]any)["post"].(map[string]any)
	publicSec, ok := publicOp["security"].([]any)
	require.True(t, ok, "an explicitly empty Security must emit a security key")
	assert.Empty(t, publicSec, "an explicitly empty Security must emit security: [] (public)")
}

func TestOpenAPI_ServersDeprecatedVersion(t *testing.T) {
	t.Parallel()
	reg := actions.NewRegistry(
		actions.WithServers(actions.Server{URL: "https://api.example.com", Description: "production"}),
		actions.WithOpenAPIVersion("3.0.3"),
	)
	a := pingAction()
	a.Deprecated = true
	actions.Register(reg, a)
	reg.Freeze()

	var doc map[string]any
	require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &doc))

	assert.Equal(t, "3.0.3", doc["openapi"])

	servers, _ := doc["servers"].([]any)
	require.Len(t, servers, 1)
	s0, _ := servers[0].(map[string]any)
	assert.Equal(t, "https://api.example.com", s0["url"])
	assert.Equal(t, "production", s0["description"])

	paths, _ := doc["paths"].(map[string]any)
	post, _ := paths["/ping"].(map[string]any)["post"].(map[string]any)
	assert.Equal(t, true, post["deprecated"])
}

func TestWithOpenAPIVersion_Invalid(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() {
		actions.NewRegistry(actions.WithOpenAPIVersion("2.0"))
	})
}

func TestDefaultOpenAPIVersionIs31(t *testing.T) {
	t.Parallel()
	reg := actions.NewRegistry()
	actions.Register(reg, pingAction())
	reg.Freeze()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &doc))
	assert.Equal(t, "3.1.0", doc["openapi"])
}
