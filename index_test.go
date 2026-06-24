package actions_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/go-actions"
	"github.com/mrz1836/go-actions/actiontest"
)

func getBody(t *testing.T, url, accept string) (body, contentType string, status int) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(raw), resp.Header.Get("Content-Type"), resp.StatusCode
}

func TestIndex(t *testing.T) {
	t.Parallel()

	newServer := func(t *testing.T, opts ...actions.Option) string {
		t.Helper()
		reg := actions.NewRegistry(opts...)
		actions.Register(reg, pingAction())
		return actiontest.NewServer(t, reg).URL
	}

	t.Run("HTML by default uses the neutral title", func(t *testing.T) {
		t.Parallel()
		body, contentType, status := getBody(t, newServer(t)+"/_actions", "")
		assert.Equal(t, http.StatusOK, status)
		assert.Contains(t, contentType, "text/html")
		assert.Contains(t, body, "<h1>API Actions</h1>")
		assert.Contains(t, body, "test.ping")
	})

	t.Run("HTML title follows WithInfo", func(t *testing.T) {
		t.Parallel()
		body, _, status := getBody(t, newServer(t, actions.WithInfo("Widgets", "", ""))+"/_actions", "")
		assert.Equal(t, http.StatusOK, status)
		assert.Contains(t, body, "<h1>Widgets Actions</h1>")
	})

	t.Run("Markdown via content negotiation", func(t *testing.T) {
		t.Parallel()
		body, contentType, status := getBody(t, newServer(t)+"/_actions", "text/markdown")
		assert.Equal(t, http.StatusOK, status)
		assert.Contains(t, contentType, "text/markdown")
		assert.True(t, strings.HasPrefix(body, "# API Actions"))
		assert.Contains(t, body, "test.ping")
	})

	t.Run("unrecognized Accept falls back to HTML", func(t *testing.T) {
		t.Parallel()
		_, contentType, status := getBody(t, newServer(t)+"/_actions", "application/xml")
		assert.Equal(t, http.StatusOK, status)
		assert.Contains(t, contentType, "text/html")
	})

	t.Run("Markdown with quality factors still negotiates Markdown", func(t *testing.T) {
		t.Parallel()
		// Exercises the q-factor (;) stripping across a multi-entry Accept header.
		body, contentType, status := getBody(t, newServer(t)+"/_actions", "text/markdown;q=0.9, text/html;q=0.8")
		assert.Equal(t, http.StatusOK, status)
		assert.Contains(t, contentType, "text/markdown")
		assert.True(t, strings.HasPrefix(body, "# API Actions"))
	})

	t.Run("openapi.json and openapi.yaml served", func(t *testing.T) {
		t.Parallel()
		base := newServer(t)
		jsonBody, jsonType, jsonStatus := getBody(t, base+"/openapi.json", "")
		assert.Equal(t, http.StatusOK, jsonStatus)
		assert.Contains(t, jsonType, "application/json")
		assert.Contains(t, jsonBody, `"openapi"`)

		_, yamlType, yamlStatus := getBody(t, base+"/openapi.yaml", "")
		assert.Equal(t, http.StatusOK, yamlStatus)
		assert.Contains(t, yamlType, "application/yaml")
	})
}
