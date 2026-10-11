package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// BenchmarkEncodeResponse covers the three envelope shapes plus a plain
// struct fallthrough.
func BenchmarkEncodeResponse(b *testing.B) {
	body := map[string]string{"id": "01900000-0000-7000-8000-000000000001"}

	b.Run("empty", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			w := httptest.NewRecorder()
			encodeResponse(w, Empty{})
		}
	})
	b.Run("created", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			w := httptest.NewRecorder()
			encodeResponse(w, Created[map[string]string]{Body: body})
		}
	})
	b.Run("plain", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			w := httptest.NewRecorder()
			encodeResponse(w, body)
		}
	})
}

//nolint:gocognit // Test function with multiple sub-tests
func TestEncodeResponse(t *testing.T) {
	t.Parallel()

	t.Run("empty writes 204 with no body", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		encodeResponse(w, Empty{})
		if w.Code != 204 {
			t.Fatalf("status = %d, want 204", w.Code)
		}
		if w.Body.Len() != 0 {
			t.Fatalf("body = %q, want empty", w.Body.String())
		}
	})

	t.Run("created writes 201 with JSON body", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		encodeResponse(w, Created[map[string]string]{Body: map[string]string{"id": "x"}})
		if w.Code != 201 {
			t.Fatalf("status = %d, want 201", w.Code)
		}
		if w.Body.String() != `{"id":"x"}` {
			t.Fatalf("body = %q", w.Body.String())
		}
		if got := w.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}
	})

	t.Run("accepted writes 202", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		encodeResponse(w, Accepted[map[string]int]{Body: map[string]int{"n": 1}})
		if w.Code != 202 {
			t.Fatalf("status = %d, want 202", w.Code)
		}
		var got map[string]int
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("body not valid JSON: %v", err)
		}
		if got["n"] != 1 {
			t.Fatalf("body = %v, want {n:1}", got)
		}
	})

	t.Run("plain struct defaults to 200", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		encodeResponse(w, map[string]string{"ok": "yes"})
		if w.Code != 200 {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})

	t.Run("unmarshalable body writes a 500 envelope", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		// A channel cannot be JSON-marshaled, forcing writeJSON's error path.
		encodeResponse(w, map[string]any{"bad": make(chan int)})
		if w.Code != 500 {
			t.Fatalf("status = %d, want 500", w.Code)
		}
		if !strings.Contains(w.Body.String(), CodeInternal) {
			t.Fatalf("body = %q, want code %s", w.Body.String(), CodeInternal)
		}
	})
}

func TestEnvelopeDefaults(t *testing.T) {
	t.Run("Empty has no body", func(t *testing.T) {
		assert.Nil(t, Empty{}.envelopeBody())
	})

	t.Run("a zero-status Response is a 200 with its headers", func(t *testing.T) {
		w := httptest.NewRecorder()
		encodeResponse(w, Response[map[string]int]{
			Header: http.Header{"Etag": {`"v1"`}},
			Body:   map[string]int{"n": 1},
		})
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, `"v1"`, w.Header().Get("ETag"))
		assert.JSONEq(t, `{"n":1}`, w.Body.String())
	})
}

func TestNotModifiedResponseHasNoBody(t *testing.T) {
	t.Parallel()
	notModified := Response[map[string]string]{
		Status: http.StatusNotModified,
		Header: http.Header{"Etag": {`"v1"`}, "Cache-Control": {"max-age=60"}},
		Body:   map[string]string{"id": "x"},
	}

	t.Run("the encoder writes headers and no body", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		encodeResponse(w, notModified)
		assert.Equal(t, http.StatusNotModified, w.Code)
		assert.Equal(t, `"v1"`, w.Header().Get("ETag"))
		assert.Equal(t, "max-age=60", w.Header().Get("Cache-Control"))
		assert.Empty(t, w.Header().Values("Content-Type"))
		assert.Empty(t, w.Body.String())
	})

	t.Run("an action served through the registry", func(t *testing.T) {
		t.Parallel()
		reg := NewRegistry()
		Register(reg, Action[struct{}, Response[map[string]string]]{
			ID: "test.cached", Method: http.MethodGet, Path: "/cached",
			Statuses: []StatusDoc{{Code: http.StatusOK}, {Code: http.StatusNotModified}},
			Handle: func(context.Context, struct{}) (Response[map[string]string], error) {
				return notModified, nil
			},
		})
		reg.Freeze()
		w := httptest.NewRecorder()
		reg.Handler().ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/cached", nil))
		assert.Equal(t, http.StatusNotModified, w.Code)
		assert.Equal(t, `"v1"`, w.Header().Get("ETag"))
		assert.Empty(t, w.Header().Values("Content-Type"))
		assert.Empty(t, w.Body.String())
	})

	t.Run("the error path writes no body either", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		NewRegistry().writeAPIError(w, req, APIError{Status: http.StatusNotModified, Code: "NOT_MODIFIED", Message: "unchanged"})
		assert.Equal(t, http.StatusNotModified, w.Code)
		assert.Empty(t, w.Header().Values("Content-Type"))
		assert.Empty(t, w.Body.String())
	})
}

func TestNoContentResponseHasNoContentType(t *testing.T) {
	t.Parallel()

	t.Run("a Response drops the headers that describe a body", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		encodeResponse(w, Response[map[string]string]{
			Status: http.StatusNoContent,
			Header: http.Header{
				"Content-Type":   {"application/json"},
				"Content-Length": {"2"},
				"X-Request-Kind": {"delete"},
			},
			Body: map[string]string{"id": "x"},
		})
		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, w.Header().Values("Content-Type"))
		assert.Empty(t, w.Header().Values("Content-Length"))
		assert.Equal(t, "delete", w.Header().Get("X-Request-Kind"), "other headers are kept")
		assert.Empty(t, w.Body.String())
	})

	t.Run("Empty stays a bare 204", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		encodeResponse(w, Empty{})
		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, w.Header())
		assert.Empty(t, w.Body.String())
	})

	t.Run("headers set before the encoder runs are dropped too", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Transfer-Encoding", "chunked")
		encodeResponse(w, Empty{})
		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, w.Header().Values("Content-Type"))
		assert.Empty(t, w.Header().Values("Transfer-Encoding"))
		assert.Empty(t, w.Body.String())
	})
}
