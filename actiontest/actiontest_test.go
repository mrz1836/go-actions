package actiontest_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mrz1836/go-actions"
	"github.com/mrz1836/go-actions/actiontest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type echoReq struct {
	Name string `json:"name" validate:"required"`
}

type echoResp struct {
	Echo string `json:"echo"`
}

func echoAction() actions.Action[echoReq, actions.Created[echoResp]] {
	return actions.Action[echoReq, actions.Created[echoResp]]{
		ID:       "test.echo",
		Method:   http.MethodPost,
		Path:     "/echo",
		Summary:  "Echo",
		Statuses: []actions.StatusDoc{{Code: 201, Description: "created"}},
		Handle: func(_ context.Context, req echoReq) (actions.Created[echoResp], error) {
			return actions.Created[echoResp]{Body: echoResp{Echo: req.Name}}, nil
		},
	}
}

func TestInvoke(t *testing.T) {
	t.Parallel()
	resp, err := actiontest.Invoke(t, echoAction(), echoReq{Name: "zoe"})
	require.NoError(t, err)
	assert.Equal(t, "zoe", resp.Body.Echo)
}

func TestNewServer(t *testing.T) {
	t.Parallel()
	reg := actions.NewRegistry()
	actions.Register(reg, echoAction())
	srv := actiontest.NewServer(t, reg)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/echo",
		strings.NewReader(`{"name":"zoe"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	var got echoResp
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "zoe", got.Echo)
}
