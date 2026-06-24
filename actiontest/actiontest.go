package actiontest

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/mrz1836/go-actions"
)

// Invoke runs an action's Handle directly — the decode, validate, and encode
// stages are bypassed — for fast handler unit tests.
func Invoke[Req, Resp any](t *testing.T, a actions.Action[Req, Resp], req Req) (Resp, error) {
	t.Helper()
	return a.Handle(context.Background(), req)
}

// NewServer freezes reg and returns an *httptest.Server exercising the real
// decode/validate/encode pipeline. The registry's Handler is mounted at the
// server root, so request paths match the declared action paths (and the
// self-documentation endpoints are served at /openapi.json, /openapi.yaml, and
// /_actions). The server is closed on test cleanup.
func NewServer(t *testing.T, reg *actions.Registry) *httptest.Server {
	t.Helper()
	reg.Freeze()

	srv := httptest.NewServer(reg.Handler())
	t.Cleanup(srv.Close)
	return srv
}
