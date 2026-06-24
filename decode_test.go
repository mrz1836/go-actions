package actions

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func withURLParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

//nolint:gocognit,gocyclo // Test function with multiple sub-tests
func TestDecodeRequest(t *testing.T) {
	t.Parallel()

	t.Run("body", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Phone string `json:"phone"`
			Email string `json:"email"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/leads",
			strings.NewReader(`{"phone":"+13055551234","email":"jane@example.com"}`))
		r.Header.Set("Content-Type", "application/json")

		got, err := decodeRequest[req](r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Phone != "+13055551234" || got.Email != "jane@example.com" {
			t.Fatalf("decoded = %+v", got)
		}
	})

	t.Run("non-struct request type is a no-op", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		got, err := decodeRequest[int](r)
		if err != nil || got != 0 {
			t.Fatalf("decoded = %v, err = %v", got, err)
		}
	})

	t.Run("empty body is not an error", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Phone string `json:"phone"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/leads", strings.NewReader(``))
		r.Header.Set("Content-Type", "application/json")
		got, err := decodeRequest[req](r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Phone != "" {
			t.Fatalf("decoded = %+v", got)
		}
	})

	t.Run("non-json content type skips body decode", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Phone string `json:"phone"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/leads",
			strings.NewReader(`not json`))
		r.Header.Set("Content-Type", "text/plain")
		got, err := decodeRequest[req](r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Phone != "" {
			t.Fatalf("decoded = %+v", got)
		}
	})

	t.Run("malformed body", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Phone string `json:"phone"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/leads", strings.NewReader(`{"phone":`))
		r.Header.Set("Content-Type", "application/json")

		_, err := decodeRequest[req](r)
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error = %v, want *APIError", err)
		}
		if apiErr.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", apiErr.Status)
		}
	})

	t.Run("path, query, and header", func(t *testing.T) {
		t.Parallel()
		type req struct {
			ID     string `json:"-" path:"id"`
			Cursor string `json:"-" query:"cursor"`
			Limit  int    `json:"-" query:"limit"`
			ReqID  string `json:"-" header:"X-Request-ID"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/persons/abc?cursor=c123&limit=25", nil)
		r.Header.Set("X-Request-ID", "req-9")
		r = withURLParam(r, "id", "abc")

		got, err := decodeRequest[req](r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != "abc" || got.Cursor != "c123" || got.Limit != 25 || got.ReqID != "req-9" {
			t.Fatalf("decoded = %+v", got)
		}
	})

	t.Run("scalar conversions", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Active bool    `json:"-" query:"active"`
			Score  float64 `json:"-" query:"score"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?active=true&score=1.5", nil)
		got, err := decodeRequest[req](r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.Active || got.Score != 1.5 {
			t.Fatalf("decoded = %+v", got)
		}
	})

	t.Run("bad query int returns 422", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Limit int `json:"-" query:"limit"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?limit=abc", nil)
		_, err := decodeRequest[req](r)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity {
			t.Fatalf("error = %v, want 422 *APIError", err)
		}
	})

	t.Run("bad query bool returns 422", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Active bool `json:"-" query:"active"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?active=maybe", nil)
		_, err := decodeRequest[req](r)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity {
			t.Fatalf("error = %v, want 422 *APIError", err)
		}
	})

	t.Run("bad query float returns 422", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Score float64 `json:"-" query:"score"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?score=high", nil)
		_, err := decodeRequest[req](r)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity {
			t.Fatalf("error = %v, want 422 *APIError", err)
		}
	})
}

// FuzzDecodeRequest proves decodeRequest never panics on arbitrary body bytes
// for a representative request type.
func FuzzDecodeRequest(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{}`),
		[]byte(`{"phone":"+13055551234","email":"jane@example.com"}`),
		[]byte(`{"phone":`),
		[]byte(``),
		[]byte(`null`),
		[]byte(`[]`),
		[]byte(`{"phone":123,"email":["array"]}`),
		[]byte("\x00\x01\x02"),
	} {
		f.Add(seed)
	}
	type req struct {
		Phone string `json:"phone"`
		Email string `json:"email"`
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/leads",
			bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		_, _ = decodeRequest[req](r) // contract: must not panic
	})
}

// BenchmarkDecodeRequest measures the cost of decoding a small JSON body into
// a request struct via the actions framework.
func BenchmarkDecodeRequest(b *testing.B) {
	type req struct {
		Phone string `json:"phone"`
		Email string `json:"email"`
	}
	body := []byte(`{"phone":"+13055551234","email":"jane@example.com"}`)
	b.ReportAllocs()
	for b.Loop() {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
			"/leads", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if _, err := decodeRequest[req](r); err != nil {
			b.Fatal(err)
		}
	}
}
