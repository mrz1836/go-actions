package actions_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/go-actions"
	"github.com/mrz1836/go-actions/actiontest"
)

// exportReq asks the export to carry a Content-Type of its own.
type exportReq struct {
	Own bool `json:"-" query:"own"`
}

// exportAction returns a CSV export as Raw, with a formula cell, under the
// media type its 200 StatusDoc declares.
func exportAction() actions.Action[exportReq, actions.Raw] {
	return actions.Action[exportReq, actions.Raw]{
		ID: "export.csv", Method: http.MethodGet, Path: "/export",
		Statuses: []actions.StatusDoc{
			{Code: http.StatusOK, MediaType: "text/csv; charset=utf-8"},
			{Code: http.StatusNotFound, Error: true},
		},
		Handle: func(_ context.Context, req exportReq) (actions.Raw, error) {
			header := http.Header{"Content-Disposition": {"attachment; filename=jobs.csv"}}
			if req.Own {
				header.Set("Content-Type", "application/octet-stream")
			}
			return actions.Raw{Header: header, Body: []byte("a,b\n=1,2\n")}, nil
		},
	}
}

// fetchRaw sends a GET to url and returns the status, headers, and body.
func fetchRaw(t *testing.T, url string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, string(body)
}

func TestRawResponseWritesItsBytesAsGiven(t *testing.T) {
	reg := actions.NewRegistry()
	actions.Register(reg, exportAction())
	url := actiontest.NewServer(t, reg).URL

	t.Run("the bytes, the header, and the declared media type", func(t *testing.T) {
		status, header, body := fetchRaw(t, url+"/export")
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, "text/csv; charset=utf-8", header.Get("Content-Type"))
		assert.Equal(t, "attachment; filename=jobs.csv", header.Get("Content-Disposition"))
		assert.Equal(t, "a,b\n=1,2\n", body, "the body is written byte for byte, the formula cell untouched")
	})

	t.Run("the declared media type replaces the Raw's own", func(t *testing.T) {
		_, header, _ := fetchRaw(t, url+"/export?own=true")
		assert.Equal(t, []string{"text/csv; charset=utf-8"}, header.Values("Content-Type"))
	})
}

func TestOpenAPIRawResponseContent(t *testing.T) {
	for _, version := range []string{"3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			reg := actions.NewRegistry(actions.WithOpenAPIVersion(version))
			actions.Register(reg, exportAction())
			reg.Freeze()
			var doc map[string]any
			require.NoError(t, json.Unmarshal(reg.OpenAPIJSON(), &doc))

			responses, _ := operation(t, doc, "get", "/export")["responses"].(map[string]any)
			ok, _ := responses["200"].(map[string]any)
			assert.Equal(t, map[string]any{
				"text/csv; charset=utf-8": map[string]any{"schema": map[string]any{"type": "string"}},
			}, ok["content"])
			notFound, _ := responses["404"].(map[string]any)
			assert.Equal(t, errorRefContent(), notFound["content"], "an error keeps the JSON Error schema")

			components, _ := doc["components"].(map[string]any)
			schemas, _ := components["schemas"].(map[string]any)
			assert.NotContains(t, schemas, "Raw", "Raw gets no component")
		})
	}
}
