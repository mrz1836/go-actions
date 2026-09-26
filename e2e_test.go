package actions_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/go-actions"
	"github.com/mrz1836/go-actions/actiontest"
)

// echoReq carries an optional query message for the plain-200 echo action.
type echoReq struct {
	Msg string `json:"-" query:"msg"`
}

// echoAction returns a plain struct, exercising the default 200 OK envelope.
func echoAction() actions.Action[echoReq, pingResp] {
	return actions.Action[echoReq, pingResp]{
		ID:       "test.echo",
		Method:   http.MethodGet,
		Path:     "/echo",
		Summary:  "Echo a message",
		Statuses: []actions.StatusDoc{{Code: http.StatusOK, Description: "ok"}},
		Handle: func(_ context.Context, req echoReq) (pingResp, error) {
			return pingResp{Greeting: "echo " + req.Msg}, nil
		},
	}
}

// fullRegistry wires every status-bearing action into one frozen server so the
// end-to-end tests prove the actions coexist and route correctly.
func fullRegistry(t *testing.T) string {
	t.Helper()
	reg := actions.NewRegistry(actions.WithInfo("E2E", "end-to-end contract", "9.9.9"))
	actions.Register(reg, echoAction())       // 200
	actions.Register(reg, getItemAction())    // 201 (Created)
	actions.Register(reg, pingAction())       // 202 (Accepted)
	actions.Register(reg, deleteItemAction()) // 204 (Empty)
	actions.Register(reg, missingAction())    // 404 (typed APIError)
	actions.Register(reg, boomAction())       // 500 (redacted)
	return actiontest.NewServer(t, reg).URL
}

// doReq issues an HTTP request and returns the status, Content-Type, and body.
func doReq(t *testing.T, method, url, body string) (status int, contentType, respBody string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, url, rdr)
	require.NoError(t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(raw)
}

// TestE2E_StatusMatrix drives every response envelope and error path through a
// real server and asserts the exact status, Content-Type, and body — proving the
// decode→validate→handle→encode pipeline returns accurate, usable responses.
func TestE2E_StatusMatrix(t *testing.T) {
	t.Parallel()
	base := fullRegistry(t)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantJSON   string // exact JSON body, when deterministic ("" = skip)
		wantNoBody bool
		wantSubstr string // substring the body must contain ("" = skip)
		wantNoLeak string // substring the body must NOT contain ("" = skip)
	}{
		{
			name: "plain 200", method: http.MethodGet, path: "/echo?msg=hi",
			wantStatus: http.StatusOK, wantJSON: `{"greeting":"echo hi"}`,
		},
		{
			name: "created 201", method: http.MethodGet, path: "/items/abc",
			wantStatus: http.StatusCreated, wantJSON: `{"name":"x"}`,
		},
		{
			name: "accepted 202", method: http.MethodPost, path: "/ping", body: `{"name":"jane"}`,
			wantStatus: http.StatusAccepted, wantJSON: `{"greeting":"hi jane"}`,
		},
		{
			name: "empty 204", method: http.MethodDelete, path: "/items/abc",
			wantStatus: http.StatusNoContent, wantNoBody: true,
		},
		{
			name: "typed 404", method: http.MethodPost, path: "/missing", body: `{"name":"x"}`,
			wantStatus: http.StatusNotFound, wantSubstr: actions.CodeNotFound,
		},
		{
			name: "redacted 500", method: http.MethodPost, path: "/boom", body: `{"name":"x"}`,
			wantStatus: http.StatusInternalServerError, wantSubstr: actions.CodeInternal,
			wantNoLeak: "internal boom detail",
		},
		{
			name: "validation 422", method: http.MethodPost, path: "/ping", body: `{}`,
			wantStatus: http.StatusUnprocessableEntity, wantSubstr: actions.CodeValidation,
		},
		{
			name: "malformed 400", method: http.MethodPost, path: "/ping", body: `{"name":`,
			wantStatus: http.StatusBadRequest, wantSubstr: actions.CodeBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status, contentType, body := doReq(t, tt.method, base+tt.path, tt.body)
			assert.Equal(t, tt.wantStatus, status)

			if tt.wantNoBody {
				assert.Empty(t, body, "envelope must carry no body")
				return
			}
			assert.Contains(t, contentType, "application/json")
			if tt.wantJSON != "" {
				assert.JSONEq(t, tt.wantJSON, body)
			}
			if tt.wantSubstr != "" {
				assert.Contains(t, body, tt.wantSubstr)
			}
			if tt.wantNoLeak != "" {
				assert.NotContains(t, body, tt.wantNoLeak, "internal detail must not leak")
			}
		})
	}
}

// TestE2E_ServedArtifacts proves the self-describing endpoints are present and
// well-formed on a live server.
func TestE2E_ServedArtifacts(t *testing.T) {
	t.Parallel()
	base := fullRegistry(t)

	t.Run("openapi.json is valid and complete", func(t *testing.T) {
		t.Parallel()
		status, contentType, body := doReq(t, http.MethodGet, base+"/openapi.json", "")
		assert.Equal(t, http.StatusOK, status)
		assert.Contains(t, contentType, "application/json")

		var doc map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &doc))
		assert.Equal(t, "3.1.0", doc["openapi"])
		for _, opID := range []string{"test.echo", "test.get_item", "test.ping", "test.delete_item"} {
			assert.Contains(t, body, opID, "operationId %s must appear in the contract", opID)
		}
	})

	t.Run("openapi.yaml is served", func(t *testing.T) {
		t.Parallel()
		status, contentType, _ := doReq(t, http.MethodGet, base+"/openapi.yaml", "")
		assert.Equal(t, http.StatusOK, status)
		assert.Contains(t, contentType, "application/yaml")
	})

	t.Run("_actions index is served as HTML", func(t *testing.T) {
		t.Parallel()
		status, contentType, body := doReq(t, http.MethodGet, base+"/_actions", "")
		assert.Equal(t, http.StatusOK, status)
		assert.Contains(t, contentType, "text/html")
		assert.Contains(t, body, "E2E Actions")
	})
}

// TestE2E_Concurrent hammers the frozen handler from many goroutines to prove the
// served registry is safe for concurrent use. Each worker records its result into
// a distinct slot and the assertions run on the test goroutine after the join, so
// the test itself is race-free; run with -race to surface any race in the pipeline
// or the cached artifacts.
func TestE2E_Concurrent(t *testing.T) {
	t.Parallel()
	base := fullRegistry(t)

	type call struct {
		method, url, body string
		want              int
	}
	// Spread requests across reads, writes, and the served artifacts.
	calls := []call{
		{http.MethodGet, base + "/echo?msg=x", "", http.StatusOK},
		{http.MethodPost, base + "/ping", `{"name":"a"}`, http.StatusAccepted},
		{http.MethodGet, base + "/items/z", "", http.StatusCreated},
		{http.MethodGet, base + "/openapi.json", "", http.StatusOK},
	}

	const workers = 32
	statuses := make([]int, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func(i int) {
			defer wg.Done()
			c := calls[i%len(calls)]
			var rdr io.Reader
			if c.body != "" {
				rdr = strings.NewReader(c.body)
			}
			req, err := http.NewRequestWithContext(t.Context(), c.method, c.url, rdr)
			if err != nil {
				errs[i] = err
				return
			}
			if c.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs[i] = err
				return
			}
			_ = resp.Body.Close()
			statuses[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	for i := range workers {
		require.NoErrorf(t, errs[i], "worker %d", i)
		assert.Equalf(t, calls[i%len(calls)].want, statuses[i], "worker %d", i)
	}
}

// postWithType posts body with the given Content-Type and returns the status and
// the decoded JSON response object.
func postWithType(t *testing.T, url, contentType, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var got map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	return resp.StatusCode, got
}

// TestE2E_StrictDecoding drives WithStrictDecoding through a served registry:
// every malformed body is a 400 carrying only the generic message, a valid body
// is decoded, a mixed-case JSON content type is honored, and the 413 body cap
// still applies. Without the option, an unknown field is still accepted.
func TestE2E_StrictDecoding(t *testing.T) {
	t.Parallel()
	reg := actions.NewRegistry(actions.WithStrictDecoding())
	actions.Register(reg, pingAction())
	strictURL := actiontest.NewServer(t, reg).URL + "/ping"

	t.Run("malformed bodies are a generic 400", func(t *testing.T) {
		t.Parallel()
		for _, body := range []string{
			`{"name":"jane","x":1}`,
			`{"name":"jane"}{}`,
			`{"name":"jane"} garbage`,
			`{"name":`,
		} {
			status, got := postWithType(t, strictURL, "application/json", body)
			assert.Equal(t, http.StatusBadRequest, status, body)
			assert.Equal(t, actions.CodeBadRequest, got["code"], body)
			assert.Equal(t, "malformed JSON body", got["error"], body)
		}
	})

	t.Run("a valid body is decoded", func(t *testing.T) {
		t.Parallel()
		status, got := postWithType(t, strictURL, "application/json", `{"name":"jane"}`)
		assert.Equal(t, http.StatusAccepted, status)
		assert.Equal(t, "hi jane", got["greeting"])
	})

	t.Run("a mixed-case JSON content type is decoded", func(t *testing.T) {
		t.Parallel()
		status, got := postWithType(t, strictURL, "Application/JSON", `{"name":"jane"}`)
		assert.Equal(t, http.StatusAccepted, status)
		assert.Equal(t, "hi jane", got["greeting"])
	})

	t.Run("the body cap still yields 413", func(t *testing.T) {
		t.Parallel()
		capped := actions.NewRegistry(actions.WithStrictDecoding(), actions.WithMaxBodyBytes(8))
		actions.Register(capped, pingAction())
		url := actiontest.NewServer(t, capped).URL + "/ping"
		status, got := postWithType(t, url, "application/json", `{"name":"a-name-well-over-eight-bytes"}`)
		assert.Equal(t, http.StatusRequestEntityTooLarge, status)
		assert.Equal(t, actions.CodePayloadTooLarge, got["code"])
	})

	t.Run("without the option an unknown field is accepted", func(t *testing.T) {
		t.Parallel()
		lenient := actions.NewRegistry()
		actions.Register(lenient, pingAction())
		url := actiontest.NewServer(t, lenient).URL + "/ping"
		status, got := postWithType(t, url, "application/json", `{"name":"jane","x":1}`)
		assert.Equal(t, http.StatusAccepted, status)
		assert.Equal(t, "hi jane", got["greeting"])
	})
}

// Request and response types for BenchmarkHandler.
type (
	benchCreateReq struct {
		Name  string `json:"name" validate:"required,max=64"`
		Email string `json:"email" validate:"required,email"`
	}
	benchGetReq struct {
		ID     string `json:"-" path:"id" validate:"required"`
		Expand bool   `json:"-" query:"expand"`
		Limit  int    `json:"-" query:"limit" validate:"max=100"`
		Trace  string `json:"-" header:"X-Trace-Id"`
	}
	benchUser struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email,omitempty"`
	}
)

// benchHandler returns the frozen handler of a two-action registry: a
// validated JSON create and a parameter-bound get.
func benchHandler(opts ...actions.Option) http.Handler {
	reg := actions.NewRegistry(opts...)
	actions.Register(reg, actions.Action[benchCreateReq, actions.Created[benchUser]]{
		ID: "users.create", Method: http.MethodPost, Path: "/users", Summary: "Create a user",
		Statuses: []actions.StatusDoc{
			{Code: http.StatusCreated, Description: "created"},
			{Code: http.StatusUnprocessableEntity, Description: "invalid", Error: true},
		},
		Handle: func(_ context.Context, req benchCreateReq) (actions.Created[benchUser], error) {
			return actions.Created[benchUser]{Body: benchUser{ID: "u_1", Name: req.Name, Email: req.Email}}, nil
		},
	})
	actions.Register(reg, actions.Action[benchGetReq, benchUser]{
		ID: "users.get", Method: http.MethodGet, Path: "/users/{id}", Summary: "Get a user",
		Statuses: []actions.StatusDoc{{Code: http.StatusOK, Description: "the user"}},
		Handle: func(_ context.Context, req benchGetReq) (benchUser, error) {
			return benchUser{ID: req.ID, Name: "Ada"}, nil
		},
	})
	reg.Freeze()
	return reg.Handler()
}

// BenchmarkHandler measures whole requests through Registry.Handler — routing,
// the framework middleware, decode, validate, handle, and encode — for the
// common outcomes.
func BenchmarkHandler(b *testing.B) {
	plain := benchHandler()
	observed := benchHandler(actions.WithObserver(func(actions.Observation) {}))
	const created = `{"name":"Ada","email":"ada@example.com"}`
	cases := []struct {
		name    string
		handler http.Handler
		method  string
		target  string
		body    string
		want    int
	}{
		{"create_201", plain, http.MethodPost, "/users", created, http.StatusCreated},
		{"validation_422", plain, http.MethodPost, "/users", `{"name":"","email":"nope"}`, http.StatusUnprocessableEntity},
		{"get_params_200", plain, http.MethodGet, "/users/u_1?expand=true&limit=10", "", http.StatusOK},
		{"not_found_404", plain, http.MethodGet, "/nope", "", http.StatusNotFound},
		{"with_observer", observed, http.MethodPost, "/users", created, http.StatusCreated},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var body io.Reader
				if tc.body != "" {
					body = strings.NewReader(tc.body)
				}
				r := httptest.NewRequestWithContext(context.Background(), tc.method, tc.target, body)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-Trace-Id", "t-1")
				w := httptest.NewRecorder()
				tc.handler.ServeHTTP(w, r)
				if w.Code != tc.want {
					b.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
				}
			}
		})
	}
}
