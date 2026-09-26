package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

func TestWriteError_RetryAfter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		retryAfter time.Duration
		want       string // "" means the header must be absent
	}{
		{name: "fractional seconds round up", retryAfter: 1500 * time.Millisecond, want: "2"},
		{name: "sub-second rounds up to one", retryAfter: 10 * time.Millisecond, want: "1"},
		{name: "whole seconds are exact", retryAfter: 90 * time.Second, want: "90"},
		{name: "zero omits the header", retryAfter: 0, want: ""},
		{name: "negative omits the header", retryAfter: -time.Second, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := NewRegistry()
			w := httptest.NewRecorder()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", nil)
			reg.writeError(w, r, &APIError{
				Status:     http.StatusTooManyRequests,
				Code:       CodeTooManyRequests,
				Message:    "slow down",
				RetryAfter: tc.retryAfter,
			})

			if w.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d, want 429", w.Code)
			}
			got, present := w.Header()["Retry-After"]
			switch {
			case tc.want == "" && present:
				t.Fatalf("Retry-After = %q, want absent", got)
			case tc.want != "" && w.Header().Get("Retry-After") != tc.want:
				t.Fatalf("Retry-After = %q, want %q", w.Header().Get("Retry-After"), tc.want)
			}
		})
	}

	t.Run("the envelope body is unchanged", func(t *testing.T) {
		t.Parallel()
		write := func(retryAfter time.Duration) string {
			reg := NewRegistry()
			w := httptest.NewRecorder()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", nil)
			r.Header.Set("X-Request-ID", "req-1")
			reg.writeError(w, r, &APIError{
				Status:     http.StatusTooManyRequests,
				Code:       CodeTooManyRequests,
				Message:    "slow down",
				RetryAfter: retryAfter,
			})
			return w.Body.String()
		}
		with, without := write(3*time.Second), write(0)
		if with != without {
			t.Fatalf("body with Retry-After = %q, without = %q", with, without)
		}
		var body errorResponse
		if err := json.Unmarshal([]byte(with), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Code != CodeTooManyRequests || body.Error != "slow down" || body.RequestID != "req-1" {
			t.Fatalf("body = %+v", body)
		}
	})

	t.Run("a custom mapper can set it", func(t *testing.T) {
		t.Parallel()
		mapper := func(error) APIError {
			return APIError{Status: http.StatusServiceUnavailable, Code: CodeServiceUnavailable, Message: "busy", RetryAfter: 5 * time.Second}
		}
		reg := NewRegistry(WithErrorMapper(mapper))
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		reg.writeError(w, r, errOpaque)
		if got := w.Header().Get("Retry-After"); got != "5" {
			t.Fatalf("Retry-After = %q, want 5", got)
		}
	})
}

func TestErrorCodeValues(t *testing.T) {
	t.Parallel()
	// The wire spelling of each code is part of the public contract — pin it so a
	// rename is a deliberate, breaking change rather than a silent one.
	cases := map[string]string{
		CodeValidation:         "VALIDATION_ERROR",
		CodeBadRequest:         "BAD_REQUEST",
		CodeUnauthorized:       "UNAUTHORIZED",
		CodeForbidden:          "FORBIDDEN",
		CodeNotFound:           "NOT_FOUND",
		CodeConflict:           "CONFLICT",
		CodePayloadTooLarge:    "PAYLOAD_TOO_LARGE",
		CodeTooManyRequests:    "TOO_MANY_REQUESTS",
		CodeInternal:           "INTERNAL_ERROR",
		CodeMethodNotAllowed:   "METHOD_NOT_ALLOWED",
		CodeTimeout:            "TIMEOUT",
		CodeServiceUnavailable: "SERVICE_UNAVAILABLE",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("code value = %q, want %q", got, want)
		}
	}
}

func TestWriteAPIErrorFlattensFieldDetails(t *testing.T) {
	reg := NewRegistry()
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", nil)
	reg.writeAPIError(w, r, APIError{
		Status: http.StatusUnprocessableEntity, Code: CodeValidation, Message: "ignored when fields are present",
		Fields: []FieldError{{Field: "name", Message: "is required"}, {Message: "coupon expired"}},
	})
	assert.JSONEq(t, `{"error":"validation failed: name: is required; coupon expired","code":"VALIDATION_ERROR"}`, w.Body.String())
}

func TestWriteErrorOnZeroValueRegistry(t *testing.T) {
	var reg Registry // no NewRegistry: errorMapper is nil
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
	reg.writeError(w, r, errPlain)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.JSONEq(t, `{"error":"an internal error occurred","code":"INTERNAL_ERROR"}`, w.Body.String())
}

// BenchmarkWriteAPIError measures writing a 422 with field details — the
// flattening and envelope encoding every validation failure pays.
func BenchmarkWriteAPIError(b *testing.B) {
	reg := NewRegistry()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/x", nil)
	r.Header.Set("X-Request-ID", "req-1")
	apiErr := APIError{
		Status: http.StatusUnprocessableEntity, Code: CodeValidation, Message: "validation failed",
		Fields: []FieldError{{Field: "name", Message: "is required"}, {Field: "email", Message: "must be a valid email address"}},
	}
	b.ReportAllocs()
	for b.Loop() {
		reg.writeAPIError(httptest.NewRecorder(), r, apiErr)
	}
}
