package actions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/mrz1836/go-actions"
)

func frozenPingRegistry(t *testing.T, opts ...actions.Option) *actions.Registry {
	t.Helper()
	reg := actions.NewRegistry(opts...)
	actions.Register(reg, pingAction())
	reg.Freeze()
	return reg
}

func TestOpenAPI_DocumentComplete(t *testing.T) {
	t.Parallel()
	reg := frozenPingRegistry(t)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &doc))

	assert.Equal(t, "3.1.0", doc["openapi"])

	paths, _ := doc["paths"].(map[string]any)
	require.Contains(t, paths, "/ping")

	pathItem, _ := paths["/ping"].(map[string]any)
	post, _ := pathItem["post"].(map[string]any)
	assert.Equal(t, "test.ping", post["operationId"])

	responses, _ := post["responses"].(map[string]any)
	assert.Contains(t, responses, "202")

	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	assert.Contains(t, schemas, "Error")
}

// TestOpenAPI_InfoIsConfigurable verifies WithInfo drives the info block and
// that the default info stays neutral with no project-specific naming.
func TestOpenAPI_InfoIsConfigurable(t *testing.T) {
	t.Parallel()

	t.Run("default info is neutral", func(t *testing.T) {
		t.Parallel()
		reg := frozenPingRegistry(t)
		var doc map[string]any
		require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &doc))
		info, _ := doc["info"].(map[string]any)
		assert.Equal(t, "API", info["title"])
		assert.Equal(t, "1.0.0", info["version"])
	})

	t.Run("WithInfo overrides title, description, and version", func(t *testing.T) {
		t.Parallel()
		reg := frozenPingRegistry(t, actions.WithInfo("Widgets", "the widget contract", "2.4.0"))
		var doc map[string]any
		require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &doc))
		info, _ := doc["info"].(map[string]any)
		assert.Equal(t, "Widgets", info["title"])
		assert.Equal(t, "the widget contract", info["description"])
		assert.Equal(t, "2.4.0", info["version"])
	})
}

type listReq struct {
	ID     string `json:"-" path:"id" validate:"required"`
	Cursor string `json:"-" query:"cursor"`
	Limit  int    `json:"-" query:"limit" validate:"min=1,max=100"`
	ReqID  string `json:"-" header:"X-Request-ID"`
}

type item struct {
	Name string `json:"name"`
}

func getItemAction() actions.Action[listReq, actions.Created[item]] {
	return actions.Action[listReq, actions.Created[item]]{
		ID:      "test.get_item",
		Method:  http.MethodGet,
		Path:    "/items/{id}",
		Summary: "Get an item",
		Statuses: []actions.StatusDoc{
			{Code: 201, Description: "created"},
			{Code: 404, Description: "not found", Error: true},
		},
		Handle: func(_ context.Context, _ listReq) (actions.Created[item], error) {
			return actions.Created[item]{Body: item{Name: "x"}}, nil
		},
	}
}

type deleteReq struct {
	ID string `json:"-" path:"id"`
}

func deleteItemAction() actions.Action[deleteReq, actions.Empty] {
	return actions.Action[deleteReq, actions.Empty]{
		ID:      "test.delete_item",
		Method:  http.MethodDelete,
		Path:    "/items/{id}",
		Summary: "Delete an item",
		Statuses: []actions.StatusDoc{
			{Code: 204, Description: "deleted"},
			{Code: 404, Description: "not found", Error: true},
		},
		Handle: func(_ context.Context, _ deleteReq) (actions.Empty, error) {
			return actions.Empty{}, nil
		},
	}
}

func TestOpenAPI_ParametersResponsesAndErrors(t *testing.T) {
	t.Parallel()
	reg := actions.NewRegistry()
	actions.Register(reg, getItemAction())
	actions.Register(reg, deleteItemAction())
	reg.Freeze()

	var doc map[string]any
	require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &doc))
	paths, _ := doc["paths"].(map[string]any)
	pathItem, _ := paths["/items/{id}"].(map[string]any)

	get, _ := pathItem["get"].(map[string]any)
	params, _ := get["parameters"].([]any)
	require.Len(t, params, 4)
	locations := map[string]string{}
	for _, p := range params {
		pm, _ := p.(map[string]any)
		name, _ := pm["name"].(string)
		in, _ := pm["in"].(string)
		locations[name] = in
	}
	assert.Equal(t, "path", locations["id"])
	assert.Equal(t, "query", locations["cursor"])
	assert.Equal(t, "query", locations["limit"])
	assert.Equal(t, "header", locations["X-Request-ID"])

	getResponses, _ := get["responses"].(map[string]any)
	require.Contains(t, getResponses, "201")
	require.Contains(t, getResponses, "404")
	notFound, _ := getResponses["404"].(map[string]any)
	content, _ := notFound["content"].(map[string]any)
	appJSON, _ := content["application/json"].(map[string]any)
	schema, _ := appJSON["schema"].(map[string]any)
	assert.Equal(t, "#/components/schemas/Error", schema["$ref"])

	// The Empty (204) response carries no body.
	del, _ := pathItem["delete"].(map[string]any)
	delResponses, _ := del["responses"].(map[string]any)
	noContent, _ := delResponses["204"].(map[string]any)
	_, hasContent := noContent["content"]
	assert.False(t, hasContent, "204 must not carry a response body")
}

func TestOpenAPI_JSONYAMLParity(t *testing.T) {
	t.Parallel()
	reg := frozenPingRegistry(t)

	var fromJSON, fromYAML any
	require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &fromJSON))
	require.NoError(t, yaml.Unmarshal(reg.OpenAPIYAML(), &fromYAML))

	// Re-encode both as JSON so the comparison ignores representation.
	jsonCanon, err := json.Marshal(fromJSON)
	require.NoError(t, err)
	yamlCanon, err := json.Marshal(normalizeYAML(fromYAML))
	require.NoError(t, err)
	assert.JSONEq(t, string(jsonCanon), string(yamlCanon),
		"the JSON and YAML serializations must carry identical content")
}

// collectRefs walks an arbitrary JSON tree and records every "$ref" string value.
func collectRefs(v any, out map[string]bool) {
	switch typed := v.(type) {
	case map[string]any:
		for k, val := range typed {
			if k == "$ref" {
				if s, ok := val.(string); ok {
					out[s] = true
				}
			}
			collectRefs(val, out)
		}
	case []any:
		for _, val := range typed {
			collectRefs(val, out)
		}
	}
}

// TestOpenAPI_RefIntegrityAndCompleteness proves the generated contract is
// internally consistent: every $ref resolves to a defined component, and every
// registered action appears with its operationId. A dangling $ref would make the
// published contract unusable by downstream code generators.
func TestOpenAPI_RefIntegrityAndCompleteness(t *testing.T) {
	t.Parallel()
	reg := actions.NewRegistry()
	actions.Register(reg, getItemAction())
	actions.Register(reg, deleteItemAction())
	reg.Freeze()

	var doc map[string]any
	require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &doc))

	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)

	refs := map[string]bool{}
	collectRefs(doc, refs)
	require.NotEmpty(t, refs, "expected at least one $ref (the Error schema)")
	for ref := range refs {
		name, ok := strings.CutPrefix(ref, "#/components/schemas/")
		require.Truef(t, ok, "ref %q must point into components/schemas", ref)
		assert.Containsf(t, schemas, name, "dangling $ref: %s", ref)
	}

	// Completeness: every registered action is represented by its operationId.
	paths, _ := doc["paths"].(map[string]any)
	found := map[string]bool{}
	for _, pi := range paths {
		pathItem, _ := pi.(map[string]any)
		for _, op := range pathItem {
			if operation, ok := op.(map[string]any); ok {
				if id, ok := operation["operationId"].(string); ok {
					found[id] = true
				}
			}
		}
	}
	for _, id := range []string{"test.get_item", "test.delete_item"} {
		assert.Truef(t, found[id], "operationId %s missing from the contract", id)
	}
}

// BenchmarkBuildOpenAPI measures the one-time setup cost of validating actions
// and generating the OpenAPI document plus the _actions index at Freeze time.
func BenchmarkBuildOpenAPI(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		reg := actions.NewRegistry()
		actions.Register(reg, getItemAction())
		actions.Register(reg, deleteItemAction())
		reg.Freeze()
		_ = reg.OpenAPIJSON()
	}
}

// normalizeYAML converts yaml.v3's map[string]interface{} / map[interface{}]
// interface{} trees into JSON-marshalable form.
func normalizeYAML(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, val := range typed {
			out[k] = normalizeYAML(val)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, val := range typed {
			out[i] = normalizeYAML(val)
		}
		return out
	default:
		return v
	}
}
