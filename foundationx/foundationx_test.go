package foundationx_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	foundation "github.com/mrz1836/go-foundation/models"
	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/go-actions"
	"github.com/mrz1836/go-actions/foundationx"
)

// errOpaque is a static, non-API error used to exercise the redacted-500 path.
var errOpaque = errors.New("connection string with secrets")

func TestNewErrorMapper(t *testing.T) {
	t.Parallel()
	mapper := foundationx.NewErrorMapper()

	t.Run("APIError passes through unchanged", func(t *testing.T) {
		t.Parallel()
		in := &actions.APIError{Status: http.StatusConflict, Code: actions.CodeConflict, Message: "dup"}
		got := mapper(in)
		assert.Equal(t, http.StatusConflict, got.Status)
		assert.Equal(t, actions.CodeConflict, got.Code)
		assert.Equal(t, "dup", got.Message)
	})

	t.Run("wrapped APIError is unwrapped", func(t *testing.T) {
		t.Parallel()
		in := &actions.APIError{Status: http.StatusBadRequest, Code: actions.CodeBadRequest}
		got := mapper(fmt.Errorf("layer: %w", in))
		assert.Equal(t, http.StatusBadRequest, got.Status)
	})

	t.Run("ValidationError maps to 422 with field detail", func(t *testing.T) {
		t.Parallel()
		ve := foundation.NewValidationError("email", "is not a valid email address")
		got := mapper(ve)
		assert.Equal(t, http.StatusUnprocessableEntity, got.Status)
		assert.Equal(t, actions.CodeValidation, got.Code)
		if assert.Len(t, got.Fields, 1) {
			assert.Equal(t, "email", got.Fields[0].Field)
			assert.Equal(t, "is not a valid email address", got.Fields[0].Message)
		}
	})

	t.Run("ErrNotFound maps to 404", func(t *testing.T) {
		t.Parallel()
		got := mapper(foundation.ErrNotFound)
		assert.Equal(t, http.StatusNotFound, got.Status)
		assert.Equal(t, actions.CodeNotFound, got.Code)
	})

	t.Run("wrapped ErrNotFound maps to 404", func(t *testing.T) {
		t.Parallel()
		got := mapper(fmt.Errorf("lookup person: %w", foundation.ErrNotFound))
		assert.Equal(t, http.StatusNotFound, got.Status)
	})

	t.Run("opaque error redacts to a 500", func(t *testing.T) {
		t.Parallel()
		got := mapper(errOpaque)
		assert.Equal(t, http.StatusInternalServerError, got.Status)
		assert.Equal(t, actions.CodeInternal, got.Code)
		assert.NotContains(t, got.Message, "secrets")
	})
}

// TestMapperWiresIntoRegistry proves the adapter satisfies actions.ErrorMapper
// and is installable via WithErrorMapper.
func TestMapperWiresIntoRegistry(t *testing.T) {
	t.Parallel()
	reg := actions.NewRegistry(actions.WithErrorMapper(foundationx.NewErrorMapper()))
	assert.NotNil(t, reg)
}
