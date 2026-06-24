package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerServesActionsAndContract(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(handler())
	t.Cleanup(srv.Close)

	for _, path := range []string{"/pets", "/pets/1", "/openapi.json", "/_actions"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatalf("build request for %s: %v", path, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
		}
	}
}
