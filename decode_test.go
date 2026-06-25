package actions

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

	t.Run("unsigned int query binds", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Limit uint `json:"-" query:"limit"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?limit=25", nil)
		got, err := decodeRequest[req](r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Limit != 25 {
			t.Fatalf("decoded = %+v, want Limit 25", got)
		}
	})

	t.Run("negative unsigned int query returns 422", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Limit uint `json:"-" query:"limit"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?limit=-5", nil)
		_, err := decodeRequest[req](r)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity {
			t.Fatalf("error = %v, want 422 *APIError", err)
		}
	})

	t.Run("time.Time query binds from RFC3339", func(t *testing.T) {
		t.Parallel()
		type req struct {
			From time.Time `json:"-" query:"from"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?from=2026-06-24T10:30:00Z", nil)
		got, err := decodeRequest[req](r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.From.Equal(time.Date(2026, 6, 24, 10, 30, 0, 0, time.UTC)) {
			t.Fatalf("decoded = %+v", got)
		}
	})

	t.Run("time.Time query binds from a bare date", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Day time.Time `json:"-" query:"day"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?day=2026-06-24", nil)
		got, err := decodeRequest[req](r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.Day.Equal(time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("decoded = %+v", got)
		}
	})

	t.Run("bad time.Time query returns 422", func(t *testing.T) {
		t.Parallel()
		type req struct {
			From time.Time `json:"-" query:"from"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?from=not-a-time", nil)
		_, err := decodeRequest[req](r)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity {
			t.Fatalf("error = %v, want 422 *APIError", err)
		}
	})

	t.Run("pointer query params allocate and bind", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Active *bool      `json:"-" query:"active"`
			Limit  *int       `json:"-" query:"limit"`
			From   *time.Time `json:"-" query:"from"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?active=false&limit=10&from=2026-06-24T00:00:00Z", nil)
		got, err := decodeRequest[req](r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Active == nil || *got.Active != false {
			t.Fatalf("Active = %v", got.Active)
		}
		if got.Limit == nil || *got.Limit != 10 {
			t.Fatalf("Limit = %v", got.Limit)
		}
		if got.From == nil || !got.From.Equal(time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("From = %v", got.From)
		}
	})

	t.Run("absent pointer query params stay nil", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Active *bool `json:"-" query:"active"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		got, err := decodeRequest[req](r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Active != nil {
			t.Fatalf("Active = %v, want nil", got.Active)
		}
	})

	t.Run("unbindable field kind returns 500 instead of panicking", func(t *testing.T) {
		t.Parallel()
		// A slice-kind query field is a server-side declaration error: it must
		// fail cleanly, not panic on reflect.Value.SetString.
		type req struct {
			Tags []string `json:"-" query:"tags"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?tags=a", nil)
		_, err := decodeRequest[req](r)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusInternalServerError {
			t.Fatalf("error = %v, want 500 *APIError", err)
		}
		if apiErr.Code != CodeInternal {
			t.Fatalf("code = %s, want %s", apiErr.Code, CodeInternal)
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
