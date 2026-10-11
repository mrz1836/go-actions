package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

		got, err := decodeRequest[req](r, false)
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
		got, err := decodeRequest[int](r, false)
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
		got, err := decodeRequest[req](r, false)
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
		got, err := decodeRequest[req](r, false)
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

		_, err := decodeRequest[req](r, false)
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

		got, err := decodeRequest[req](r, false)
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
		got, err := decodeRequest[req](r, false)
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
		_, err := decodeRequest[req](r, false)
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
		_, err := decodeRequest[req](r, false)
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
		_, err := decodeRequest[req](r, false)
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
		got, err := decodeRequest[req](r, false)
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
		_, err := decodeRequest[req](r, false)
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
		got, err := decodeRequest[req](r, false)
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
		got, err := decodeRequest[req](r, false)
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
		_, err := decodeRequest[req](r, false)
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
		got, err := decodeRequest[req](r, false)
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
		got, err := decodeRequest[req](r, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Active != nil {
			t.Fatalf("Active = %v, want nil", got.Active)
		}
	})

	t.Run("a slice query field binds every value", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Tags []string `json:"-" query:"tags"`
		}
		for target, want := range map[string][]string{
			"/x?tags=a":        {"a"},
			"/x?tags=a&tags=b": {"a", "b"},
		} {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
			got, err := decodeRequest[req](r, false)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", target, err)
			}
			if !reflect.DeepEqual(got.Tags, want) {
				t.Fatalf("%s: Tags = %q, want %q", target, got.Tags, want)
			}
		}
	})
}

// repeatedQueryReq holds a slice query field of every element type a
// repeated parameter binds.
type repeatedQueryReq struct {
	States []string     `json:"-" query:"state"`
	Limits []int        `json:"-" query:"limit"`
	IDs    []uuid.UUID  `json:"-" query:"id"`
	Since  []time.Time  `json:"-" query:"since"`
	Kinds  []queryState `json:"-" query:"kind"`
}

// queryState is a named string element of a slice query field.
type queryState string

func TestDecodeRepeatedQueryParameterFillsASlice(t *testing.T) {
	const (
		id1 = "01900000-0000-7000-8000-000000000001"
		id2 = "01900000-0000-7000-8000-000000000002"
	)
	tests := []struct {
		name   string
		target string
		want   repeatedQueryReq
	}{
		{
			name:   "values bind in the order they arrive",
			target: "/x?state=failed&state=retryable&state=failed",
			want:   repeatedQueryReq{States: []string{"failed", "retryable", "failed"}},
		},
		{
			name:   "empty values are skipped",
			target: "/x?state=failed&state=&state=retryable",
			want:   repeatedQueryReq{States: []string{"failed", "retryable"}},
		},
		{name: "an absent parameter leaves the field nil", target: "/x"},
		{name: "only empty values leave the field nil", target: "/x?state=&state="},
		{
			name:   "a comma is part of a value",
			target: "/x?state=a,b&state=c",
			want:   repeatedQueryReq{States: []string{"a,b", "c"}},
		},
		{
			name:   "integers",
			target: "/x?limit=5&limit=-2",
			want:   repeatedQueryReq{Limits: []int{5, -2}},
		},
		{
			name:   "UUIDs",
			target: "/x?id=" + id1 + "&id=" + id2,
			want:   repeatedQueryReq{IDs: []uuid.UUID{uuid.MustParse(id1), uuid.MustParse(id2)}},
		},
		{
			name:   "times and calendar dates",
			target: "/x?since=2026-05-20T10:00:00Z&since=2026-05-21",
			want: repeatedQueryReq{Since: []time.Time{
				time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
				time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
			}},
		},
		{
			name:   "a named string element",
			target: "/x?kind=queued&kind=running",
			want:   repeatedQueryReq{Kinds: []queryState{"queued", "running"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.target, nil)
			got, err := decodeRequest[repeatedQueryReq](r, false)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("a slice parameter replaces the body field it shares", func(t *testing.T) {
		type req struct {
			Tags []string `json:"tags" query:"tag"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x?tag=q",
			strings.NewReader(`{"tags":["b1","b2"]}`))
		got, err := decodeRequest[req](r, false)
		require.NoError(t, err)
		assert.Equal(t, []string{"q"}, got.Tags)

		r = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", strings.NewReader(`{"tags":["b1"]}`))
		got, err = decodeRequest[req](r, false)
		require.NoError(t, err)
		assert.Equal(t, []string{"b1"}, got.Tags, "an absent parameter leaves the body's value")
	})

	t.Run("a slice type that unmarshals itself binds one value", func(t *testing.T) {
		type req struct {
			IP net.IP `json:"-" query:"ip"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?ip=10.0.0.1&ip=10.0.0.2", nil)
		got, err := decodeRequest[req](r, false)
		require.NoError(t, err)
		assert.Equal(t, "10.0.0.1", got.IP.String())
	})
}

func TestDecodeScalarQueryParameterKeepsTheFirstValue(t *testing.T) {
	type req struct {
		State string `json:"-" query:"state"`
		Limit *int   `json:"-" query:"limit"`
		Trace string `json:"-" header:"X-Trace-Id"`
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?state=a&state=b&limit=3&limit=x", nil)
	r.Header.Add("X-Trace-Id", "t-1")
	r.Header.Add("X-Trace-Id", "t-2")
	got, err := decodeRequest[req](r, false)
	require.NoError(t, err)
	assert.Equal(t, "a", got.State)
	require.NotNil(t, got.Limit)
	assert.Equal(t, 3, *got.Limit, "a later value is never read, so it cannot fail")
	assert.Equal(t, "t-1", got.Trace)
}

func TestDecodeRepeatedQueryParameterNamesTheBadElement(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		field   string
		message string
	}{
		{"the second value", "/x?limit=5&limit=x", "limit[1]", "must be an integer"},
		{"the position counts empty values", "/x?limit=&limit=x", "limit[1]", "must be an integer"},
		{"the first bad value wins", "/x?limit=x&limit=y", "limit[0]", "must be an integer"},
		{
			"a UUID element", "/x?id=01900000-0000-7000-8000-000000000001&id=nope",
			"id[1]", "must be a valid UUID",
		},
		{"a time element", "/x?since=&since=&since=yesterday", "since[2]", "must be an RFC3339 timestamp"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.target, nil)
			_, err := decodeRequest[repeatedQueryReq](r, false)
			requireParamError(t, err, tc.field, tc.message)
		})
	}
}

// FuzzDecodeRepeatedQuery proves binding a repeated query parameter never
// panics, that every value which converts is bound in order, and that a value
// which doesn't is a 422 naming its element.
func FuzzDecodeRepeatedQuery(f *testing.F) {
	for _, seed := range [][3]string{
		{"", "", ""},
		{"1", "", "a"},
		{"1", "2", "a"},
		{"", "7", ""},
		{"1,2", "3", "a,b"},
		{"%31", "4", "%zz"},
		{"1", "x", "b"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	type req struct {
		N []int    `json:"-" query:"n"`
		S []string `json:"-" query:"s"`
	}
	f.Fuzz(func(t *testing.T, a, b, c string) {
		target := "/x?n=" + url.QueryEscape(a) + "&n=" + url.QueryEscape(b) + "&s=" + url.QueryEscape(c)
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		got, err := decodeRequest[req](r, false) // contract: must not panic
		if err != nil {
			requireElementFailure(t, err, "n[0]", "n[1]")
			return
		}
		if want := boundInts(t, a, b); !reflect.DeepEqual(got.N, want) {
			t.Fatalf("N = %v, want %v", got.N, want)
		}
		if c != "" && !reflect.DeepEqual(got.S, []string{c}) {
			t.Fatalf("S = %q, want [%q]", got.S, c)
		}
	})
}

// requireElementFailure fails unless err is a 422 VALIDATION_ERROR whose every
// failure names one of the given elements.
func requireElementFailure(t *testing.T, err error, elements ...string) {
	t.Helper()
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity || apiErr.Code != CodeValidation {
		t.Fatalf("error = %v, want a 422 VALIDATION_ERROR", err)
	}
	for _, fe := range apiErr.Fields {
		if !slices.Contains(elements, fe.Field) {
			t.Fatalf("failure names %q, want one of %q", fe.Field, elements)
		}
	}
}

// boundInts returns the integers a repeated parameter with the given values
// binds: every non-empty value, in order. It fails when a value doesn't
// convert, since binding it should have been refused.
func boundInts(t *testing.T, vals ...string) []int {
	t.Helper()
	var out []int
	for _, v := range vals {
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("value %q bound without an error", v)
		}
		out = append(out, n)
	}
	return out
}

// TestDecodeRequestStrict pins the strict mode enabled by WithStrictDecoding:
// unknown fields and trailing data are rejected, and every malformed body
// yields the same generic message with no parser detail.
func TestDecodeRequestStrict(t *testing.T) {
	t.Parallel()
	type req struct {
		Email string `json:"email"`
	}

	rejected := []struct {
		name string
		body string
	}{
		{"unknown field", `{"email":"a@b.co","x":1}`},
		{"trailing value", `{"email":"a@b.co"}{"x":1}`},
		{"trailing empty object", `{} {}`},
		{"trailing garbage", `{} garbage`},
		{"syntax error", `{"email":`},
		{"wrong type", `{"email":1}`},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")

			_, err := decodeRequest[req](r, true)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want *APIError", err)
			}
			if apiErr.Status != http.StatusBadRequest || apiErr.Code != CodeBadRequest {
				t.Fatalf("got %d %s, want 400 %s", apiErr.Status, apiErr.Code, CodeBadRequest)
			}
			if apiErr.Message != "malformed JSON body" {
				t.Fatalf("message = %q, want exactly %q", apiErr.Message, "malformed JSON body")
			}
		})
	}

	accepted := []struct {
		name string
		body string
		want string
	}{
		{"single value", `{"email":"a@b.co"}`, "a@b.co"},
		{"trailing whitespace", "{\"email\":\"a@b.co\"}\n\t ", "a@b.co"},
		{"empty body", ``, ""},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")

			got, err := decodeRequest[req](r, true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Email != tc.want {
				t.Fatalf("decoded = %+v, want email %q", got, tc.want)
			}
		})
	}
}

// TestDecodeRequestLenient pins the default mode, which strict decoding must
// leave unchanged: unknown fields and trailing data are tolerated, and a
// malformed body keeps its parser detail.
func TestDecodeRequestLenient(t *testing.T) {
	t.Parallel()
	type req struct {
		Email string `json:"email"`
	}

	t.Run("unknown field and trailing data are accepted", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x",
			strings.NewReader(`{"email":"a@b.co","x":1} garbage`))
		r.Header.Set("Content-Type", "application/json")

		got, err := decodeRequest[req](r, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Email != "a@b.co" {
			t.Fatalf("decoded = %+v", got)
		}
	})

	t.Run("malformed body keeps the parser detail", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", strings.NewReader(`{"email":`))
		r.Header.Set("Content-Type", "application/json")

		_, err := decodeRequest[req](r, false)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
			t.Fatalf("error = %v, want 400 *APIError", err)
		}
		if !strings.HasPrefix(apiErr.Message, "malformed JSON body: ") {
			t.Fatalf("message = %q, want the parser detail", apiErr.Message)
		}
	})
}

// TestDecodeRequestContentTypeCase proves the JSON media type matches
// case-insensitively (RFC 9110) in both modes, while other types still skip
// the body.
func TestDecodeRequestContentTypeCase(t *testing.T) {
	t.Parallel()
	type req struct {
		Email string `json:"email"`
	}

	for _, strict := range []bool{false, true} {
		for _, tc := range []struct {
			contentType string
			want        string
		}{
			{"application/json", "a@b.co"},
			{"Application/JSON", "a@b.co"},
			{"APPLICATION/JSON; charset=utf-8", "a@b.co"},
			{"text/plain", ""},
		} {
			t.Run(fmt.Sprintf("strict=%t/%s", strict, tc.contentType), func(t *testing.T) {
				t.Parallel()
				r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x",
					strings.NewReader(`{"email":"a@b.co"}`))
				r.Header.Set("Content-Type", tc.contentType)

				got, err := decodeRequest[req](r, strict)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.Email != tc.want {
					t.Fatalf("decoded = %+v, want email %q", got, tc.want)
				}
			})
		}
	}
}

// FuzzDecodeRequest proves decodeRequest never panics on arbitrary body bytes
// for a representative request type, in either decoding mode.
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
		for _, strict := range []bool{false, true} {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/leads",
				bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			_, _ = decodeRequest[req](r, strict) // contract: must not panic
		}
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
		if _, err := decodeRequest[req](r, false); err != nil {
			b.Fatal(err)
		}
	}
}

// requireParamError asserts err is a 422 naming exactly one field.
func requireParamError(t *testing.T, err error, field, message string) {
	t.Helper()
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.Status)
	assert.Equal(t, CodeValidation, apiErr.Code)
	assert.Equal(t, []FieldError{{Field: field, Message: message}}, apiErr.Fields)
}

func TestDecodeTextUnmarshalerParams(t *testing.T) {
	type req struct {
		ID   uuid.UUID   `json:"-" path:"id"`
		Ref  *uuid.UUID  `json:"-" query:"ref"`
		Addr *netip.Addr `json:"-" header:"X-Client-IP"`
	}
	const id = "01900000-0000-7000-8000-000000000001"

	t.Run("valid values bind", func(t *testing.T) {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?ref="+id, nil)
		r.Header.Set("X-Client-IP", "10.0.0.1")
		got, err := decodeRequest[req](withURLParam(r, "id", id), false)
		require.NoError(t, err)
		assert.Equal(t, uuid.MustParse(id), got.ID)
		require.NotNil(t, got.Ref)
		assert.Equal(t, uuid.MustParse(id), *got.Ref)
		require.NotNil(t, got.Addr)
		assert.Equal(t, netip.MustParseAddr("10.0.0.1"), *got.Addr)
	})

	t.Run("an invalid uuid is a 422 under the parameter name", func(t *testing.T) {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		_, err := decodeRequest[req](withURLParam(r, "id", "nope"), false)
		requireParamError(t, err, "id", "must be a valid UUID")
	})

	t.Run("another TextUnmarshaler's failure is a 422", func(t *testing.T) {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		r.Header.Set("X-Client-IP", "not-an-ip")
		_, err := decodeRequest[req](withURLParam(r, "id", id), false)
		requireParamError(t, err, "X-Client-IP", "has an invalid format")
	})
}

func TestDecodeParamErrorsUseParamNames(t *testing.T) {
	type req struct {
		Limit  int     `json:"-" query:"limit"`
		Small  int8    `json:"-" query:"small"`
		Count  uint16  `json:"-" header:"X-Count"`
		Ratio  float32 `json:"-" header:"X-Ratio"`
		Named  string  `json:"named_json" query:"named_param"`
		Active bool    `json:"-" header:"X-Active"`
	}
	tests := []struct {
		name    string
		target  string
		headers map[string]string
		field   string
		message string
	}{
		{"json-dash query field", "/x?limit=abc", nil, "limit", "must be an integer"},
		{"integer overflow", "/x?small=300", nil, "small", "must be an integer"},
		{"header uint", "/x", map[string]string{"X-Count": "-1"}, "X-Count", "must be a non-negative integer"},
		{"header float", "/x", map[string]string{"X-Ratio": "1e40"}, "X-Ratio", "must be a number"},
		{"header bool", "/x", map[string]string{"X-Active": "maybe"}, "X-Active", "must be a boolean"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.target, nil)
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			_, err := decodeRequest[req](r, false)
			requireParamError(t, err, tc.field, tc.message)
		})
	}
}

func TestDecodeBindingRules(t *testing.T) {
	t.Run("GET and DELETE bodies are never decoded", func(t *testing.T) {
		type req struct {
			Name string `json:"name"`
		}
		for _, method := range []string{http.MethodGet, http.MethodDelete} {
			r := httptest.NewRequestWithContext(t.Context(), method, "/x", strings.NewReader(`{"name":"ignored"`))
			r.Header.Set("Content-Type", "application/json")
			got, err := decodeRequest[req](r, true)
			require.NoError(t, err, method)
			assert.Empty(t, got.Name, method)
		}
	})

	t.Run("a repeated query parameter binds its first value", func(t *testing.T) {
		type req struct {
			Tag string `json:"-" query:"tag"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?tag=a&tag=b", nil)
		got, err := decodeRequest[req](r, false)
		require.NoError(t, err)
		assert.Equal(t, "a", got.Tag)
	})

	t.Run("an empty parameter is treated as absent", func(t *testing.T) {
		type req struct {
			Limit *int `json:"-" query:"limit"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?limit=", nil)
		got, err := decodeRequest[req](r, false)
		require.NoError(t, err)
		assert.Nil(t, got.Limit)
	})

	t.Run("a parameter overrides the body field it shares", func(t *testing.T) {
		type req struct {
			ID string `json:"id" path:"pid"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/x/p", strings.NewReader(`{"id":"b"}`))
		got, err := decodeRequest[req](withURLParam(r, "pid", "p"), false)
		require.NoError(t, err)
		assert.Equal(t, "p", got.ID)
	})

	t.Run("unexported and embedded parameter fields are not bound", func(t *testing.T) {
		type embedded struct {
			Inner string `json:"-" query:"inner"`
		}
		type req struct {
			embedded

			hidden string `query:"hidden"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?inner=a&hidden=b", nil)
		got, err := decodeRequest[req](r, false)
		require.NoError(t, err)
		assert.Empty(t, got.Inner)
		assert.Empty(t, got.hidden)
	})

	t.Run("a pointer request type is allocated and bound", func(t *testing.T) {
		type req struct {
			Name  string `json:"name"`
			Limit int    `json:"-" query:"limit"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x?limit=3", strings.NewReader(`{"name":"a"}`))
		got, err := decodeRequest[*req](r, false)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, req{Name: "a", Limit: 3}, *got)
	})

	t.Run("a JSON null body leaves a pointer request allocated", func(t *testing.T) {
		type req struct {
			Name string `json:"name"`
		}
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", strings.NewReader(`null`))
		got, err := decodeRequest[*req](r, false)
		require.NoError(t, err)
		assert.NotNil(t, got)
	})

	t.Run("a request without a Body decodes as empty", func(t *testing.T) {
		type req struct {
			Name string `json:"name" validate:"required"`
		}
		r := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/x"}, Header: http.Header{}}
		got, err := decodeRequest[req](r, true)
		require.NoError(t, err)
		assert.Empty(t, got.Name)
	})

	t.Run("binders are cached per type", func(t *testing.T) {
		type req struct {
			A string `json:"-" query:"a"`
		}
		first, second := binderFor(reflect.TypeFor[req]()), binderFor(reflect.TypeFor[req]())
		assert.Same(t, first, second)
	})
}

// BenchmarkDecodeRequest_Params measures binding path, query, and header
// parameters — the query string is parsed once however many fields read it.
func BenchmarkDecodeRequest_Params(b *testing.B) {
	type req struct {
		ID     string `json:"-" path:"id"`
		Cursor string `json:"-" query:"cursor"`
		Limit  int    `json:"-" query:"limit"`
		Active *bool  `json:"-" query:"active"`
		Trace  string `json:"-" header:"X-Trace-Id"`
	}
	base := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/pets/p_1?cursor=c_9&limit=25&active=true", nil)
	base.Header.Set("X-Trace-Id", "t-1")
	r := withURLParam(base, "id", "p_1")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := decodeRequest[req](r, false); err != nil {
			b.Fatal(err)
		}
	}
}
