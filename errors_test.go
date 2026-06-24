package actions

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// errOpaque is a static, non-API error used to exercise the redacted-500 path.
var errOpaque = errors.New("database password leaked in message")

func TestAPIError_Error(t *testing.T) {
	t.Parallel()
	err := &APIError{Status: http.StatusConflict, Code: CodeConflict, Message: "already exists"}
	if err.Error() != "already exists" {
		t.Fatalf("Error() = %q", err.Error())
	}
}

func TestDefaultErrorMapper(t *testing.T) {
	t.Parallel()

	t.Run("APIError passes through with status and code intact", func(t *testing.T) {
		t.Parallel()
		in := &APIError{Status: http.StatusBadRequest, Code: CodeBadRequest, Message: "bad"}
		got := defaultErrorMapper(in)
		if got.Status != http.StatusBadRequest || got.Code != CodeBadRequest || got.Message != "bad" {
			t.Fatalf("mapped = %+v", got)
		}
	})

	t.Run("wrapped APIError is unwrapped", func(t *testing.T) {
		t.Parallel()
		base := &APIError{Status: http.StatusNotFound, Code: CodeNotFound, Message: "missing"}
		got := defaultErrorMapper(fmt.Errorf("context: %w", base))
		if got.Status != http.StatusNotFound || got.Code != CodeNotFound {
			t.Fatalf("mapped = %+v", got)
		}
	})

	t.Run("opaque error redacts to a 500", func(t *testing.T) {
		t.Parallel()
		got := defaultErrorMapper(errOpaque)
		if got.Status != http.StatusInternalServerError || got.Code != CodeInternal {
			t.Fatalf("mapped = %+v", got)
		}
		if got.Message == "database password leaked in message" {
			t.Fatal("internal error message must be redacted, not echoed")
		}
	})
}

func TestWriteError(t *testing.T) {
	t.Parallel()

	t.Run("validation fields are flattened into the message", func(t *testing.T) {
		t.Parallel()
		reg := NewRegistry()
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", nil)
		err := &APIError{
			Status: http.StatusUnprocessableEntity,
			Code:   CodeValidation,
			Fields: []FieldError{{Field: "name", Message: "is required"}},
		}
		reg.writeError(w, r, err)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d", w.Code)
		}
		var body errorResponse
		if decErr := json.Unmarshal(w.Body.Bytes(), &body); decErr != nil {
			t.Fatalf("decode: %v", decErr)
		}
		if body.Code != CodeValidation {
			t.Fatalf("code = %q", body.Code)
		}
		if body.Error == "" || body.Error == "validation failed" {
			t.Fatalf("error message not expanded: %q", body.Error)
		}
	})

	t.Run("request id propagates from X-Request-ID", func(t *testing.T) {
		t.Parallel()
		reg := NewRegistry()
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		r.Header.Set("X-Request-ID", "req-77")
		reg.writeError(w, r, &APIError{Status: http.StatusNotFound, Code: CodeNotFound, Message: "nope"})

		var body errorResponse
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.RequestID != "req-77" {
			t.Fatalf("request_id = %q, want req-77", body.RequestID)
		}
	})

	t.Run("request id falls back to X-Amzn-Request-Id", func(t *testing.T) {
		t.Parallel()
		reg := NewRegistry()
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		r.Header.Set("X-Amzn-Request-Id", "amzn-9")
		reg.writeError(w, r, &APIError{Status: http.StatusNotFound, Code: CodeNotFound, Message: "nope"})

		var body errorResponse
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.RequestID != "amzn-9" {
			t.Fatalf("request_id = %q, want amzn-9", body.RequestID)
		}
	})

	t.Run("custom mapper is honored", func(t *testing.T) {
		t.Parallel()
		mapper := func(error) APIError {
			return APIError{Status: http.StatusTeapot, Code: "TEAPOT", Message: "short and stout"}
		}
		reg := NewRegistry(WithErrorMapper(mapper))
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		reg.writeError(w, r, errOpaque)

		if w.Code != http.StatusTeapot {
			t.Fatalf("status = %d, want 418", w.Code)
		}
		var body errorResponse
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.Code != "TEAPOT" {
			t.Fatalf("code = %q, want TEAPOT", body.Code)
		}
	})

	t.Run("a mapped zero status defaults to 500", func(t *testing.T) {
		t.Parallel()
		mapper := func(error) APIError { return APIError{Code: "WEIRD", Message: "no status"} }
		reg := NewRegistry(WithErrorMapper(mapper))
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		reg.writeError(w, r, errOpaque)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", w.Code)
		}
	})
}
