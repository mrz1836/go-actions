package actions_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

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
