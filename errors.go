package actions

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

// Stable, transport-level error codes. These are generic HTTP error codes with
// no domain coupling; a custom ErrorMapper may emit any code it likes.
const (
	CodeValidation       = "VALIDATION_ERROR"
	CodeBadRequest       = "BAD_REQUEST"
	CodeNotFound         = "NOT_FOUND"
	CodeConflict         = "CONFLICT"
	CodeInternal         = "INTERNAL_ERROR"
	CodeMethodNotAllowed = "METHOD_NOT_ALLOWED"
	CodePayloadTooLarge  = "PAYLOAD_TOO_LARGE"
	CodeTimeout          = "TIMEOUT"
)

// FieldError is one per-field validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// APIError is the framework's transport-level error. It carries an HTTP status,
// a stable code, a human message, and an optional field-keyed detail list. It
// implements error, so handlers may return it directly, and an ErrorMapper may
// produce it from any domain error.
type APIError struct {
	Status  int
	Code    string
	Message string
	Fields  []FieldError
}

// Error implements the error interface.
func (e *APIError) Error() string { return e.Message }

// ErrorMapper maps any handler error to an APIError, decoupling the framework
// from a caller's domain error model. Install one with WithErrorMapper; when
// none is set, defaultErrorMapper is used.
type ErrorMapper func(error) APIError

// defaultErrorMapper is the generic, domain-free mapping. An *APIError (such as
// the 400/422 errors produced by decode and validate) passes through with its
// status and code intact; every other error maps to a redacted 500 so internal
// detail never reaches the wire.
func defaultErrorMapper(err error) APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return *apiErr
	}
	return APIError{
		Status:  http.StatusInternalServerError,
		Code:    CodeInternal,
		Message: "an internal error occurred",
	}
}

// errorResponse is the JSON error envelope written on the wire:
// {"error": "...", "code": "...", "request_id": "..."}.
type errorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"request_id,omitempty"`
}

// requestID extracts the request correlation id from the common forwarding
// headers, falling back to "".
func requestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	return r.Header.Get("X-Amzn-Request-Id")
}

// writeError maps err through the registry's ErrorMapper and writes the result
// as the standard JSON error envelope. A 5xx is logged in full and its message
// redacted to the mapped message.
func (r *Registry) writeError(w http.ResponseWriter, req *http.Request, err error) {
	mapper := r.errorMapper
	if mapper == nil {
		mapper = defaultErrorMapper
	}
	apiErr := mapper(err)
	if apiErr.Status == 0 {
		apiErr.Status = http.StatusInternalServerError
	}
	if apiErr.Status >= http.StatusInternalServerError {
		slog.ErrorContext(req.Context(), "actions: handler error", "error", err)
	}
	// Surface the handled error to the observe middleware, which runs outside the
	// typed handler and otherwise sees only the status code.
	if st, ok := req.Context().Value(observeStateKey).(*observeState); ok {
		st.err = err
	}
	r.writeAPIError(w, req, apiErr)
}

// writeAPIError writes an already-mapped APIError as the standard JSON error
// envelope, flattening any field details into the message. It is the single
// write path shared by handler errors, panics, and the 404/405 defaults.
func (r *Registry) writeAPIError(w http.ResponseWriter, req *http.Request, apiErr APIError) {
	message := apiErr.Message
	if len(apiErr.Fields) > 0 {
		parts := make([]string, len(apiErr.Fields))
		for i, fe := range apiErr.Fields {
			parts[i] = fe.Field + ": " + fe.Message
		}
		message = "validation failed: " + strings.Join(parts, "; ")
	}
	writeJSON(w, apiErr.Status, errorResponse{
		Error:     message,
		Code:      apiErr.Code,
		RequestID: requestID(req),
	})
}
