package actions_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/mrz1836/go-actions"
)

// annotatedRegistry returns a frozen registry in the given dialect with three
// GET actions: one read-only and idempotent, one destructive, idempotent, and
// open-world, and one without annotations.
func annotatedRegistry(t *testing.T, version string) *actions.Registry {
	t.Helper()
	reg := actions.NewRegistry(actions.WithOpenAPIVersion(version))
	annotated := func(id, path string, ann *actions.Annotations) actions.Action[struct{}, pingResp] {
		return actions.Action[struct{}, pingResp]{
			ID: id, Method: http.MethodGet, Path: path,
			Statuses:    []actions.StatusDoc{{Code: http.StatusOK}},
			Annotations: ann,
			Handle:      okHandle[struct{}, pingResp],
		}
	}
	actions.Register(reg, annotated("tools.read", "/read", &actions.Annotations{ReadOnly: true, Idempotent: true}))
	actions.Register(reg, annotated("tools.wipe", "/wipe", &actions.Annotations{Destructive: true, Idempotent: true, OpenWorld: true}))
	actions.Register(reg, annotated("tools.plain", "/plain", nil))
	reg.Freeze()
	return reg
}

// operation returns the operation at method and path of a decoded document.
func operation(t *testing.T, doc map[string]any, method, path string) map[string]any {
	t.Helper()
	paths, _ := doc["paths"].(map[string]any)
	item, _ := paths[path].(map[string]any)
	op, ok := item[method].(map[string]any)
	require.True(t, ok, "no %s operation at %s", method, path)
	return op
}

func TestOpenAPIWritesToolAnnotations(t *testing.T) {
	for _, version := range []string{"3.1.0", "3.0.3"} {
		reg := annotatedRegistry(t, version)

		var fromJSON map[string]any
		require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &fromJSON))
		var fromYAML map[string]any
		require.NoError(t, yaml.Unmarshal(reg.OpenAPIYAML(), &fromYAML))

		for format, doc := range map[string]map[string]any{"json": fromJSON, "yaml": fromYAML} {
			t.Run(version+" "+format, func(t *testing.T) {
				assert.Equal(t, map[string]any{
					"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false,
				}, operation(t, doc, "get", "/read")["x-mcp-annotations"])
				assert.Equal(t, map[string]any{
					"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": true,
				}, operation(t, doc, "get", "/wipe")["x-mcp-annotations"])
				assert.NotContains(t, operation(t, doc, "get", "/plain"), "x-mcp-annotations",
					"an action without annotations writes no extension")
			})
		}
	}
}
