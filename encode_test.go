package actions

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
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
