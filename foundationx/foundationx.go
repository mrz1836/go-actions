package foundationx

import (
	"errors"
	"net/http"

	"github.com/mrz1836/go-actions"
	foundation "github.com/mrz1836/go-foundation/models"
)

// NewErrorMapper returns an actions.ErrorMapper that maps the go-foundation
// error model to transport-level APIErrors:
//
//   - an *actions.APIError passes through unchanged (handlers may return one
//     directly);
//   - a *foundation.ValidationError maps to 422 with its field detail;
//   - foundation.ErrNotFound (and anything wrapping it) maps to 404;
//   - every other error maps to a redacted 500.
//
// It deliberately does not handle gorm or any other domain-specific sentinel;
// callers that need those compose their own mapper or wrap this one.
func NewErrorMapper() actions.ErrorMapper {
	return mapFoundationError
}

// mapFoundationError is the concrete mapping installed by NewErrorMapper.
func mapFoundationError(err error) actions.APIError {
	var apiErr *actions.APIError
	if errors.As(err, &apiErr) {
		return *apiErr
	}

	var ve *foundation.ValidationError
	if errors.As(err, &ve) {
		return actions.APIError{
			Status:  http.StatusUnprocessableEntity,
			Code:    actions.CodeValidation,
			Message: ve.Error(),
			Fields:  []actions.FieldError{{Field: ve.Field, Message: ve.Message}},
		}
	}

	if errors.Is(err, foundation.ErrNotFound) {
		return actions.APIError{
			Status:  http.StatusNotFound,
			Code:    actions.CodeNotFound,
			Message: "resource not found",
		}
	}

	return actions.APIError{
		Status:  http.StatusInternalServerError,
		Code:    actions.CodeInternal,
		Message: "an internal error occurred",
	}
}
