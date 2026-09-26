package actions_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/go-actions"
	"github.com/mrz1836/go-actions/actiontest"
)

type pingReq struct {
	Name string `json:"name" validate:"required"`
}

type pingResp struct {
	Greeting string `json:"greeting"`
}

func pingAction() actions.Action[pingReq, actions.Accepted[pingResp]] {
	return actions.Action[pingReq, actions.Accepted[pingResp]]{
		ID:       "test.ping",
		Method:   http.MethodPost,
		Path:     "/ping",
		Summary:  "Ping",
		Tags:     []string{"test"},
		Statuses: []actions.StatusDoc{{Code: 202, Description: "accepted"}},
		Handle: func(_ context.Context, req pingReq) (actions.Accepted[pingResp], error) {
			return actions.Accepted[pingResp]{Body: pingResp{Greeting: "hi " + req.Name}}, nil
		},
	}
}

// postJSON posts a JSON body to url and returns the response.
func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

// assertPanics asserts that fn panics. The panic value is returned for further
// inspection.
func assertPanics(t *testing.T, fn func()) (got any) {
	t.Helper()
	defer func() { got = recover() }()
	fn()
	t.Fatal("expected a panic but fn returned normally")
	return nil
}

//nolint:gocognit // Test function with multiple sub-tests
func TestRegistry(t *testing.T) {
	t.Parallel()

	t.Run("register freeze serve", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry()
		actions.Register(reg, pingAction())
		srv := actiontest.NewServer(t, reg)

		resp := postJSON(t, srv.URL+"/ping", `{"name":"jane"}`)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", resp.StatusCode)
		}
		var body pingResp
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Greeting != "hi jane" {
			t.Fatalf("greeting = %q", body.Greeting)
		}
	})

	t.Run("validation rejected with 422", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry()
		actions.Register(reg, pingAction())
		srv := actiontest.NewServer(t, reg)

		resp := postJSON(t, srv.URL+"/ping", `{}`)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422", resp.StatusCode)
		}
		b, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(b), actions.CodeValidation) {
			t.Fatalf("body = %s, want code %s", b, actions.CodeValidation)
		}
	})

	t.Run("setup-time invariants panic", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			run  func()
		}{
			{
				name: "empty ID",
				run: func() {
					reg := actions.NewRegistry()
					a := pingAction()
					a.ID = ""
					actions.Register(reg, a)
					reg.Freeze()
				},
			},
			{
				name: "empty method",
				run: func() {
					reg := actions.NewRegistry()
					a := pingAction()
					a.Method = ""
					actions.Register(reg, a)
					reg.Freeze()
				},
			},
			{
				name: "invalid path",
				run: func() {
					reg := actions.NewRegistry()
					a := pingAction()
					a.Path = "ping"
					actions.Register(reg, a)
					reg.Freeze()
				},
			},
			{
				name: "duplicate action ID",
				run: func() {
					reg := actions.NewRegistry()
					actions.Register(reg, pingAction())
					dup := pingAction()
					dup.Path = "/ping2"
					actions.Register(reg, dup)
					reg.Freeze()
				},
			},
			{
				name: "duplicate method+path",
				run: func() {
					reg := actions.NewRegistry()
					actions.Register(reg, pingAction())
					dup := pingAction()
					dup.ID = "test.ping2"
					actions.Register(reg, dup)
					reg.Freeze()
				},
			},
			{
				name: "action declares no statuses",
				run: func() {
					reg := actions.NewRegistry()
					a := pingAction()
					a.Statuses = nil
					actions.Register(reg, a)
					reg.Freeze()
				},
			},
			{
				name: "response header with an empty name",
				run: func() {
					reg := actions.NewRegistry()
					a := pingAction()
					a.Statuses = []actions.StatusDoc{
						{Code: http.StatusTooManyRequests, Error: true, Headers: []actions.HeaderDoc{{Description: "no name"}}},
					}
					actions.Register(reg, a)
					reg.Freeze()
				},
			},
			{
				name: "Handler called before Freeze",
				run: func() {
					reg := actions.NewRegistry()
					_ = reg.Handler()
				},
			},
			{
				name: "Register called after Freeze",
				run: func() {
					reg := actions.NewRegistry()
					reg.Freeze()
					actions.Register(reg, pingAction())
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := assertPanics(t, tt.run); got == nil {
					t.Fatalf("expected a panic for %q", tt.name)
				}
			})
		}
	})

	t.Run("Freeze is idempotent", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry()
		actions.Register(reg, pingAction())
		reg.Freeze()
		reg.Freeze() // second call is a no-op, must not panic
		if len(reg.OpenAPIJSON()) == 0 {
			t.Fatal("expected OpenAPI JSON after freeze")
		}
	})

	t.Run("Invoke bypasses the pipeline", func(t *testing.T) {
		t.Parallel()
		resp, err := actiontest.Invoke(t, pingAction(), pingReq{Name: "amy"})
		if err != nil {
			t.Fatalf("invoke: %v", err)
		}
		if resp.Body.Greeting != "hi amy" {
			t.Fatalf("greeting = %q", resp.Body.Greeting)
		}
	})
}

// errBoom is a static, opaque handler error used to exercise the redacted-500
// path; its message must never reach the wire.
var errBoom = errors.New("internal boom detail")

// boomAction returns a plain (non-APIError) error, exercising the redacted 500
// path through the registry-built handler.
func boomAction() actions.Action[pingReq, pingResp] {
	return actions.Action[pingReq, pingResp]{
		ID:      "test.boom",
		Method:  http.MethodPost,
		Path:    "/boom",
		Summary: "Boom",
		Statuses: []actions.StatusDoc{
			{Code: http.StatusOK, Description: "ok"},
			{Code: http.StatusInternalServerError, Description: "boom", Error: true},
		},
		Handle: func(_ context.Context, _ pingReq) (pingResp, error) {
			return pingResp{}, errBoom
		},
	}
}

// missingAction returns a typed *APIError, exercising the mapped-status path.
func missingAction() actions.Action[pingReq, pingResp] {
	return actions.Action[pingReq, pingResp]{
		ID:      "test.missing",
		Method:  http.MethodPost,
		Path:    "/missing",
		Summary: "Missing",
		Statuses: []actions.StatusDoc{
			{Code: http.StatusOK, Description: "ok"},
			{Code: http.StatusNotFound, Description: "missing", Error: true},
		},
		Handle: func(_ context.Context, _ pingReq) (pingResp, error) {
			return pingResp{}, &actions.APIError{
				Status:  http.StatusNotFound,
				Code:    actions.CodeNotFound,
				Message: "nope",
			}
		},
	}
}

// TestRegistry_HandlerErrors covers the error branches of the registry-built
// handler: a malformed body (400), a plain handler error (redacted 500), and a
// typed *APIError returned by a handler (mapped through to its status).
func TestRegistry_HandlerErrors(t *testing.T) {
	t.Parallel()

	t.Run("plain handler error maps to a redacted 500", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry()
		actions.Register(reg, boomAction())
		srv := actiontest.NewServer(t, reg)

		resp := postJSON(t, srv.URL+"/boom", `{"name":"jane"}`)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", resp.StatusCode)
		}
		b, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(b), "internal boom detail") {
			t.Fatalf("body leaked the internal error detail: %s", b)
		}
		if !strings.Contains(string(b), actions.CodeInternal) {
			t.Fatalf("body = %s, want code %s", b, actions.CodeInternal)
		}
	})

	t.Run("typed APIError passes through with its status", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry()
		actions.Register(reg, missingAction())
		srv := actiontest.NewServer(t, reg)

		resp := postJSON(t, srv.URL+"/missing", `{"name":"jane"}`)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		b, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(b), actions.CodeNotFound) {
			t.Fatalf("body = %s, want code %s", b, actions.CodeNotFound)
		}
	})

	t.Run("malformed body maps to 400", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry()
		actions.Register(reg, pingAction())
		srv := actiontest.NewServer(t, reg)

		resp := postJSON(t, srv.URL+"/ping", `{"name":`)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		b, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(b), actions.CodeBadRequest) {
			t.Fatalf("body = %s, want code %s", b, actions.CodeBadRequest)
		}
	})
}

// okHandle is a Handle returning Resp's zero value.
func okHandle[Req, Resp any](context.Context, Req) (Resp, error) {
	var zero Resp
	return zero, nil
}

// freezeOne registers a single action and freezes the registry, returning the
// panic value, or nil when Freeze succeeded.
func freezeOne[Req, Resp any](a actions.Action[Req, Resp], opts ...actions.Option) (panicked any) {
	defer func() { panicked = recover() }()
	reg := actions.NewRegistry(opts...)
	actions.Register(reg, a)
	reg.Freeze()
	return nil
}

func TestFreezeRejectsNilHandle(t *testing.T) {
	got := freezeOne(actions.Action[pingReq, pingResp]{
		ID: "test.nil", Method: http.MethodGet, Path: "/nil",
		Statuses: []actions.StatusDoc{{Code: http.StatusOK}},
	})
	assert.Equal(t, `actions: action "test.nil" has a nil Handle`, got)
}

func TestRegisterUppercasesMethod(t *testing.T) {
	t.Run("a lowercase method routes and documents as uppercase", func(t *testing.T) {
		reg := actions.NewRegistry()
		actions.Register(reg, actions.Action[struct{}, pingResp]{
			ID: "test.lower", Method: "get", Path: "/lower", Summary: "Lower",
			Statuses: []actions.StatusDoc{{Code: http.StatusOK}},
			Handle:   okHandle[struct{}, pingResp],
		})
		srv := actiontest.NewServer(t, reg)

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/lower", nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, string(reg.OpenAPIJSON()), `"get": {`)
	})

	t.Run("case variants of one method collide", func(t *testing.T) {
		reg := actions.NewRegistry()
		for _, m := range []string{"get", "GET"} {
			actions.Register(reg, actions.Action[struct{}, pingResp]{
				ID: "test." + m, Method: m, Path: "/dup",
				Statuses: []actions.StatusDoc{{Code: http.StatusOK}},
				Handle:   okHandle[struct{}, pingResp],
			})
		}
		assert.PanicsWithValue(t, `actions: duplicate route "GET /dup" (actions "test.get" and "test.GET")`, reg.Freeze)
	})
}

func TestFreezeRejectsRoutesCollidingAfterStripPrefix(t *testing.T) {
	reg := actions.NewRegistry(actions.WithStripPrefix("/v1"))
	for _, p := range []string{"/v1/pets", "/pets"} {
		actions.Register(reg, actions.Action[struct{}, pingResp]{
			ID: "test" + strings.ReplaceAll(p, "/", "."), Method: http.MethodGet, Path: p,
			Statuses: []actions.StatusDoc{{Code: http.StatusOK}},
			Handle:   okHandle[struct{}, pingResp],
		})
	}
	assert.PanicsWithValue(t, `actions: duplicate route "GET /pets" (actions "test.v1.pets" and "test.pets")`, reg.Freeze)
}

func TestFreezeRequiresDocumentedSuccessStatus(t *testing.T) {
	ok := func(code int) []actions.StatusDoc { return []actions.StatusDoc{{Code: code}} }
	errOnly := func(code int) []actions.StatusDoc { return []actions.StatusDoc{{Code: code, Error: true}} }

	tests := []struct {
		name   string
		freeze func() any
		want   any
	}{
		{"Empty documents 204", func() any { return freezeEnvelope[actions.Empty](ok(204)) }, nil},
		{
			"Empty without 204", func() any { return freezeEnvelope[actions.Empty](ok(200)) },
			`actions: action "test.status" responds 204 on success (Resp actions.Empty) but documents no non-error 204 StatusDoc`,
		},
		{
			"Created without 201", func() any { return freezeEnvelope[actions.Created[pingResp]](ok(200)) },
			`actions: action "test.status" responds 201 on success (Resp actions.Created[github.com/mrz1836/go-actions_test.pingResp]) but documents no non-error 201 StatusDoc`,
		},
		{
			"Accepted without 202", func() any { return freezeEnvelope[actions.Accepted[pingResp]](ok(200)) },
			`actions: action "test.status" responds 202 on success (Resp actions.Accepted[github.com/mrz1836/go-actions_test.pingResp]) but documents no non-error 202 StatusDoc`,
		},
		{
			"a plain body without 200", func() any { return freezeEnvelope[pingResp](ok(201)) },
			`actions: action "test.status" responds 200 on success (Resp actions_test.pingResp) but documents no non-error 200 StatusDoc`,
		},
		{
			"success documented only as an error", func() any { return freezeEnvelope[pingResp](errOnly(200)) },
			`actions: action "test.status" responds 200 on success (Resp actions_test.pingResp) but documents no non-error 200 StatusDoc`,
		},
		{"a pointer envelope is checked", func() any { return freezeEnvelope[*actions.Created[pingResp]](ok(201)) }, nil},
		{"Response[T] is exempt", func() any { return freezeEnvelope[actions.Response[pingResp]](ok(299)) }, nil},
		{"an interface Resp is exempt", func() any { return freezeEnvelope[any](ok(201)) }, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.freeze())
		})
	}
}

// freezeEnvelope freezes one action whose Resp is R with the given statuses.
func freezeEnvelope[R any](statuses []actions.StatusDoc) any {
	return freezeOne(actions.Action[struct{}, R]{
		ID: "test.status", Method: http.MethodPost, Path: "/status",
		Statuses: statuses, Handle: okHandle[struct{}, R],
	})
}

func TestRegisterNonValueRequestTypes(t *testing.T) {
	var gotPtr *pingReq
	var gotInt int
	reg := actions.NewRegistry()
	actions.Register(reg, actions.Action[*pingReq, pingResp]{
		ID: "test.ptr", Method: http.MethodPost, Path: "/ptr",
		Statuses: []actions.StatusDoc{{Code: http.StatusOK}},
		Handle: func(_ context.Context, req *pingReq) (pingResp, error) {
			gotPtr = req
			return pingResp{Greeting: "hi " + req.Name}, nil
		},
	})
	actions.Register(reg, actions.Action[int, pingResp]{
		ID: "test.int", Method: http.MethodPost, Path: "/int",
		Statuses: []actions.StatusDoc{{Code: http.StatusOK}},
		Handle: func(_ context.Context, req int) (pingResp, error) {
			gotInt = req + 1
			return pingResp{}, nil
		},
	})
	srv := actiontest.NewServer(t, reg)

	t.Run("a pointer request is decoded and validated", func(t *testing.T) {
		resp := postJSON(t, srv.URL+"/ptr", `{"name":"ada"}`)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		require.NotNil(t, gotPtr)
		assert.Equal(t, "ada", gotPtr.Name)

		bad := postJSON(t, srv.URL+"/ptr", `{}`)
		defer func() { _ = bad.Body.Close() }()
		assert.Equal(t, http.StatusUnprocessableEntity, bad.StatusCode)
	})

	t.Run("a non-struct request binds nothing", func(t *testing.T) {
		resp := postJSON(t, srv.URL+"/int", `42`)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, 1, gotInt, "the handler receives the zero value")
	})
}
