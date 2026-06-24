package actions

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEmptyMarshalsToObject verifies the Empty response envelope marshals to an
// empty JSON object — the first conformance check of the action framework's
// response types.
func TestEmptyMarshalsToObject(t *testing.T) {
	b, err := json.Marshal(Empty{})
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(b))
}
