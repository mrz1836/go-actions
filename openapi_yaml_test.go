package actions

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFreezeLeavesTheOpenAPIYAMLUnrendered proves Freeze builds the OpenAPI
// JSON and leaves the YAML for its first reader: rendering a large document as
// YAML costs far more than building it, and most registries never serve it.
func TestFreezeLeavesTheOpenAPIYAMLUnrendered(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	Register(reg, Action[Empty, Empty]{
		ID: "test.lazy", Method: http.MethodPost, Path: "/lazy",
		Statuses: []StatusDoc{{Code: http.StatusNoContent}},
		Handle:   func(context.Context, Empty) (Empty, error) { return Empty{}, nil },
	})
	reg.Freeze()

	require.NotEmpty(t, reg.OpenAPIJSON(), "Freeze builds the JSON")
	assert.Nil(t, reg.openapiYAML, "Freeze leaves the YAML unrendered")

	yamlBytes := reg.OpenAPIYAML()
	require.NotEmpty(t, yamlBytes, "the first reader renders the YAML")
	require.NotEmpty(t, reg.openapiYAML, "and the registry keeps it")
	assert.Same(t, &yamlBytes[0], &reg.openapiYAML[0], "the reader gets the kept rendering")
}
