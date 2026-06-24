package actions_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/go-actions"
	"github.com/mrz1836/go-actions/actiontest"
)

// callAPI issues a request and returns the status, response headers, and body.
func callAPI(t *testing.T, method, url, body string) (int, http.Header, string) {
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
	return resp.StatusCode, resp.Header, string(raw)
}

// panicAction panics inside its handler, exercising recovery.
func panicAction() actions.Action[pingReq, pingResp] {
	return actions.Action[pingReq, pingResp]{
		ID:       "test.panic",
		Method:   http.MethodPost,
		Path:     "/panic",
		Summary:  "Panic",
		Statuses: []actions.StatusDoc{{Code: 500, Description: "boom", Error: true}},
		Handle: func(_ context.Context, _ pingReq) (pingResp, error) {
			panic("handler exploded")
		},
	}
}

// ridReq/ridAction echo the framework-assigned request id back in the body.
type ridResp struct {
	RequestID string `json:"request_id"`
}

func ridAction() actions.Action[struct{}, ridResp] {
	return actions.Action[struct{}, ridResp]{
		ID:       "test.rid",
		Method:   http.MethodGet,
		Path:     "/rid",
		Summary:  "Echo request id",
		Statuses: []actions.StatusDoc{{Code: 200, Description: "ok"}},
		Handle: func(ctx context.Context, _ struct{}) (ridResp, error) {
			return ridResp{RequestID: actions.RequestIDFromContext(ctx)}, nil
		},
	}
}

func TestMaxBodyBytes(t *testing.T) {
	t.Parallel()

	t.Run("a body over the cap is rejected with 413", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry(actions.WithMaxBodyBytes(8))
		actions.Register(reg, pingAction())
		srv := actiontest.NewServer(t, reg)

		status, _, body := callAPI(t, http.MethodPost, srv.URL+"/ping", `{"name":"a-name-well-over-eight-bytes"}`)
		assert.Equal(t, http.StatusRequestEntityTooLarge, status)
		assert.Contains(t, body, actions.CodePayloadTooLarge)
	})

	t.Run("a body under the cap is accepted", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry(actions.WithMaxBodyBytes(1024))
		actions.Register(reg, pingAction())
		srv := actiontest.NewServer(t, reg)

		status, _, _ := callAPI(t, http.MethodPost, srv.URL+"/ping", `{"name":"jane"}`)
		assert.Equal(t, http.StatusAccepted, status)
	})

	t.Run("zero disables the cap", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry(actions.WithMaxBodyBytes(0))
		actions.Register(reg, pingAction())
		srv := actiontest.NewServer(t, reg)

		big := `{"name":"` + strings.Repeat("x", 4096) + `"}`
		status, _, _ := callAPI(t, http.MethodPost, srv.URL+"/ping", big)
		assert.Equal(t, http.StatusAccepted, status)
	})
}

func TestPanicRecovery(t *testing.T) {
	t.Parallel()
	reg := actions.NewRegistry()
	actions.Register(reg, panicAction())
	actions.Register(reg, pingAction())
	srv := actiontest.NewServer(t, reg)

	t.Run("a panicking handler yields a redacted 500 with a request id", func(t *testing.T) {
		t.Parallel()
		status, header, body := callAPI(t, http.MethodPost, srv.URL+"/panic", `{"name":"x"}`)
		assert.Equal(t, http.StatusInternalServerError, status)
		assert.Contains(t, body, actions.CodeInternal)
		assert.NotContains(t, body, "handler exploded", "panic detail must not leak")
		assert.NotEmpty(t, header.Get("X-Request-ID"))
	})

	t.Run("the server survives the panic and serves the next request", func(t *testing.T) {
		t.Parallel()
		status, _, _ := callAPI(t, http.MethodPost, srv.URL+"/ping", `{"name":"jane"}`)
		assert.Equal(t, http.StatusAccepted, status)
	})
}

func TestRequestID(t *testing.T) {
	t.Parallel()

	t.Run("absent id is generated, echoed, and visible to the handler", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry()
		actions.Register(reg, ridAction())
		srv := actiontest.NewServer(t, reg)

		status, header, body := callAPI(t, http.MethodGet, srv.URL+"/rid", "")
		require.Equal(t, http.StatusOK, status)
		echoed := header.Get("X-Request-ID")
		assert.NotEmpty(t, echoed)
		assert.Contains(t, body, echoed, "the handler must observe the same id echoed on the response")
	})

	t.Run("an inbound X-Request-ID is reused", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry()
		actions.Register(reg, ridAction())
		srv := actiontest.NewServer(t, reg)

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/rid", nil)
		require.NoError(t, err)
		req.Header.Set("X-Request-ID", "caller-supplied-id")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, "caller-supplied-id", resp.Header.Get("X-Request-ID"))
	})

	t.Run("a custom generator is used", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry(actions.WithRequestIDGenerator(func() string { return "fixed-gen-id" }))
		actions.Register(reg, ridAction())
		srv := actiontest.NewServer(t, reg)

		_, header, _ := callAPI(t, http.MethodGet, srv.URL+"/rid", "")
		assert.Equal(t, "fixed-gen-id", header.Get("X-Request-ID"))
	})
}

func TestMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("registry middleware wraps every route including self-docs", func(t *testing.T) {
		t.Parallel()
		mark := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("X-Mark", "1")
				next.ServeHTTP(w, req)
			})
		}
		reg := actions.NewRegistry(actions.WithMiddleware(mark))
		actions.Register(reg, pingAction())
		srv := actiontest.NewServer(t, reg)

		_, actionHdr, _ := callAPI(t, http.MethodPost, srv.URL+"/ping", `{"name":"x"}`)
		assert.Equal(t, "1", actionHdr.Get("X-Mark"))
		_, docHdr, _ := callAPI(t, http.MethodGet, srv.URL+"/openapi.json", "")
		assert.Equal(t, "1", docHdr.Get("X-Mark"), "registry middleware must also cover self-docs")
	})

	t.Run("per-action middleware wraps only its action", func(t *testing.T) {
		t.Parallel()
		tag := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("X-Action-MW", "yes")
				next.ServeHTTP(w, req)
			})
		}
		a := pingAction()
		a.Middleware = []actions.Middleware{tag}
		reg := actions.NewRegistry()
		actions.Register(reg, a)
		actions.Register(reg, ridAction()) // no per-action middleware
		srv := actiontest.NewServer(t, reg)

		_, tagged, _ := callAPI(t, http.MethodPost, srv.URL+"/ping", `{"name":"x"}`)
		assert.Equal(t, "yes", tagged.Get("X-Action-MW"))
		_, untagged, _ := callAPI(t, http.MethodGet, srv.URL+"/rid", "")
		assert.Empty(t, untagged.Get("X-Action-MW"))
	})
}

func TestObserver(t *testing.T) {
	t.Parallel()

	obs := &recordingObserver{}
	reg := actions.NewRegistry(actions.WithObserver(obs.observe))
	actions.Register(reg, pingAction())
	actions.Register(reg, panicAction())
	srv := actiontest.NewServer(t, reg)

	t.Run("a successful request is observed with id, status, and latency", func(t *testing.T) {
		status, _, _ := callAPI(t, http.MethodPost, srv.URL+"/ping", `{"name":"jane"}`)
		require.Equal(t, http.StatusAccepted, status)

		ob, ok := obs.find("test.ping")
		require.True(t, ok, "expected an observation for test.ping")
		assert.Equal(t, http.StatusAccepted, ob.Status)
		assert.Equal(t, http.MethodPost, ob.Method)
		assert.Positive(t, ob.Duration)
		assert.NoError(t, ob.Err)
	})

	t.Run("a panic is observed as a 500 with a non-nil error", func(t *testing.T) {
		status, _, _ := callAPI(t, http.MethodPost, srv.URL+"/panic", `{"name":"x"}`)
		require.Equal(t, http.StatusInternalServerError, status)

		ob, ok := obs.find("test.panic")
		require.True(t, ok, "expected an observation for test.panic")
		assert.Equal(t, http.StatusInternalServerError, ob.Status)
		assert.Error(t, ob.Err)
	})
}

// recordingObserver is a thread-safe ObserveFunc sink for assertions.
type recordingObserver struct {
	mu  sync.Mutex
	obs []actions.Observation
}

func (o *recordingObserver) observe(ob actions.Observation) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.obs = append(o.obs, ob)
}

func (o *recordingObserver) find(actionID string) (actions.Observation, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, ob := range o.obs {
		if ob.ActionID == actionID {
			return ob, true
		}
	}
	return actions.Observation{}, false
}

func TestTimeout(t *testing.T) {
	t.Parallel()
	slow := actions.Action[pingReq, pingResp]{
		ID:       "test.slow",
		Method:   http.MethodPost,
		Path:     "/slow",
		Summary:  "Slow",
		Timeout:  10 * time.Millisecond,
		Statuses: []actions.StatusDoc{{Code: 200, Description: "ok"}, {Code: 504, Description: "timeout", Error: true}},
		Handle: func(ctx context.Context, _ pingReq) (pingResp, error) {
			<-ctx.Done() // a cooperative handler that honors the deadline
			return pingResp{}, ctx.Err()
		},
	}
	reg := actions.NewRegistry()
	actions.Register(reg, slow)
	srv := actiontest.NewServer(t, reg)

	status, _, body := callAPI(t, http.MethodPost, srv.URL+"/slow", `{"name":"x"}`)
	assert.Equal(t, http.StatusGatewayTimeout, status)
	assert.Contains(t, body, actions.CodeTimeout)
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	t.Parallel()

	t.Run("unknown route returns a JSON 404 with a request id", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry()
		actions.Register(reg, pingAction())
		srv := actiontest.NewServer(t, reg)

		status, header, body := callAPI(t, http.MethodGet, srv.URL+"/nope", "")
		assert.Equal(t, http.StatusNotFound, status)
		assert.Contains(t, body, actions.CodeNotFound)
		assert.NotEmpty(t, header.Get("X-Request-ID"))
	})

	t.Run("known path wrong method returns a JSON 405", func(t *testing.T) {
		t.Parallel()
		reg := actions.NewRegistry()
		actions.Register(reg, pingAction()) // POST /ping
		srv := actiontest.NewServer(t, reg)

		status, _, body := callAPI(t, http.MethodPut, srv.URL+"/ping", "")
		assert.Equal(t, http.StatusMethodNotAllowed, status)
		assert.Contains(t, body, actions.CodeMethodNotAllowed)
	})

	t.Run("a custom 404 handler overrides the default", func(t *testing.T) {
		t.Parallel()
		custom := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("custom-not-found"))
		})
		reg := actions.NewRegistry(actions.WithNotFoundHandler(custom))
		actions.Register(reg, pingAction())
		srv := actiontest.NewServer(t, reg)

		status, _, body := callAPI(t, http.MethodGet, srv.URL+"/nope", "")
		assert.Equal(t, http.StatusNotFound, status)
		assert.Equal(t, "custom-not-found", body)
	})

	t.Run("a custom 405 handler overrides the default", func(t *testing.T) {
		t.Parallel()
		custom := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte("custom-405"))
		})
		reg := actions.NewRegistry(actions.WithMethodNotAllowedHandler(custom))
		actions.Register(reg, pingAction()) // POST /ping
		srv := actiontest.NewServer(t, reg)

		status, _, body := callAPI(t, http.MethodPut, srv.URL+"/ping", "")
		assert.Equal(t, http.StatusMethodNotAllowed, status)
		assert.Equal(t, "custom-405", body)
	})
}

// TestPanicRecovery_AbortHandlerRepanics verifies the http.ErrAbortHandler
// sentinel is re-panicked (the connection is aborted) rather than converted to a
// 500 JSON body.
func TestPanicRecovery_AbortHandlerRepanics(t *testing.T) {
	t.Parallel()
	abort := actions.Action[pingReq, pingResp]{
		ID:       "test.abort",
		Method:   http.MethodPost,
		Path:     "/abort",
		Summary:  "Abort",
		Statuses: []actions.StatusDoc{{Code: 500, Description: "x", Error: true}},
		Handle: func(_ context.Context, _ pingReq) (pingResp, error) {
			panic(http.ErrAbortHandler)
		},
	}
	reg := actions.NewRegistry()
	actions.Register(reg, abort)
	srv := actiontest.NewServer(t, reg)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/abort", strings.NewReader(`{"name":"x"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("expected an aborted connection, got status %d", resp.StatusCode)
	}
}

func TestResponseEnvelopeWithHeaders(t *testing.T) {
	t.Parallel()
	cached := actions.Action[struct{}, actions.Response[pingResp]]{
		ID:       "test.cached",
		Method:   http.MethodGet,
		Path:     "/cached",
		Summary:  "Cached",
		Statuses: []actions.StatusDoc{{Code: 200, Description: "ok"}},
		Handle: func(_ context.Context, _ struct{}) (actions.Response[pingResp], error) {
			return actions.Response[pingResp]{
				Status: http.StatusOK,
				Header: http.Header{"Cache-Control": []string{"public, max-age=600"}},
				Body:   pingResp{Greeting: "hi"},
			}, nil
		},
	}
	reg := actions.NewRegistry()
	actions.Register(reg, cached)
	srv := actiontest.NewServer(t, reg)

	status, header, body := callAPI(t, http.MethodGet, srv.URL+"/cached", "")
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "public, max-age=600", header.Get("Cache-Control"))
	assert.JSONEq(t, `{"greeting":"hi"}`, body)
}

// couponReq implements Validatable for the end-to-end custom-validation test.
type couponReq struct {
	Code string `json:"code" validate:"required"`
}

func (c couponReq) Validate() error {
	if !strings.HasPrefix(c.Code, "SAVE") {
		return &actions.APIError{Fields: []actions.FieldError{{Field: "code", Message: "must start with SAVE"}}}
	}
	return nil
}

func TestValidatableEndToEnd(t *testing.T) {
	t.Parallel()
	coupon := actions.Action[couponReq, pingResp]{
		ID:       "test.coupon",
		Method:   http.MethodPost,
		Path:     "/coupons",
		Summary:  "Apply a coupon",
		Statuses: []actions.StatusDoc{{Code: 200, Description: "ok"}, {Code: 422, Description: "invalid", Error: true}},
		Handle: func(_ context.Context, _ couponReq) (pingResp, error) {
			return pingResp{Greeting: "applied"}, nil
		},
	}
	reg := actions.NewRegistry()
	actions.Register(reg, coupon)
	srv := actiontest.NewServer(t, reg)

	t.Run("custom rule rejects with 422", func(t *testing.T) {
		t.Parallel()
		status, _, body := callAPI(t, http.MethodPost, srv.URL+"/coupons", `{"code":"NOPE"}`)
		assert.Equal(t, http.StatusUnprocessableEntity, status)
		assert.Contains(t, body, "must start with SAVE")
	})

	t.Run("custom rule accepts a valid value", func(t *testing.T) {
		t.Parallel()
		status, _, _ := callAPI(t, http.MethodPost, srv.URL+"/coupons", `{"code":"SAVE10"}`)
		assert.Equal(t, http.StatusOK, status)
	})
}

func TestPageResponse(t *testing.T) {
	t.Parallel()
	list := actions.Action[struct{}, actions.Page[pingResp]]{
		ID:       "test.page",
		Method:   http.MethodGet,
		Path:     "/page",
		Summary:  "Page",
		Statuses: []actions.StatusDoc{{Code: 200, Description: "ok"}},
		Handle: func(_ context.Context, _ struct{}) (actions.Page[pingResp], error) {
			return actions.Page[pingResp]{
				Items:      []pingResp{{Greeting: "a"}, {Greeting: "b"}},
				NextCursor: "c2",
				HasMore:    true,
			}, nil
		},
	}
	reg := actions.NewRegistry()
	actions.Register(reg, list)
	srv := actiontest.NewServer(t, reg)

	status, _, body := callAPI(t, http.MethodGet, srv.URL+"/page", "")
	assert.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{"items":[{"greeting":"a"},{"greeting":"b"}],"next_cursor":"c2","has_more":true}`, body)
}
