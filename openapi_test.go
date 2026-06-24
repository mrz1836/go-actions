package actions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mrz1836/go-actions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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
