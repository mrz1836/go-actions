package petstore

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mrz1836/go-actions"
	"github.com/mrz1836/go-actions/actiontest"
)

// get issues a GET against url and returns the response.
func get(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return resp
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

func TestRegistryListPets(t *testing.T) {
	t.Parallel()
	srv := actiontest.NewServer(t, Registry())

	resp := get(t, srv.URL+"/pets")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var pets []Pet
	if err := json.NewDecoder(resp.Body).Decode(&pets); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pets) != 2 {
		t.Fatalf("len(pets) = %d, want 2", len(pets))
	}
}

func TestRegistryGetPetFound(t *testing.T) {
	t.Parallel()
	srv := actiontest.NewServer(t, Registry())

	resp := get(t, srv.URL+"/pets/1")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRegistryGetPetMissing(t *testing.T) {
	t.Parallel()
	srv := actiontest.NewServer(t, Registry())

	resp := get(t, srv.URL+"/pets/999")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), actions.CodeNotFound) {
		t.Fatalf("body = %s, want code %s", b, actions.CodeNotFound)
	}
}

func TestRegistryCreatePet(t *testing.T) {
	t.Parallel()
	srv := actiontest.NewServer(t, Registry())

	resp := postJSON(t, srv.URL+"/pets", `{"name":"Fido","tag":"dog"}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var pet Pet
	if err := json.NewDecoder(resp.Body).Decode(&pet); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pet.Name != "Fido" || pet.ID == "" {
		t.Fatalf("pet = %+v", pet)
	}
}

func TestRegistryCreatePetInvalid(t *testing.T) {
	t.Parallel()
	srv := actiontest.NewServer(t, Registry())

	resp := postJSON(t, srv.URL+"/pets", `{"name":""}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestRegistryServesContractEndpoints(t *testing.T) {
	t.Parallel()
	srv := actiontest.NewServer(t, Registry())

	for _, path := range []string{"/openapi.json", "/openapi.yaml", "/_actions"} {
		resp := get(t, srv.URL+path)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
		}
	}
}

func TestRegistryListPetsByTag(t *testing.T) {
	t.Parallel()
	srv := actiontest.NewServer(t, Registry())

	for _, tc := range []struct {
		query string
		names []string
	}{
		{"?tag=dog&tag=cat", []string{"Rex", "Mittens"}},
		{"?tag=dog", []string{"Rex"}},
		{"?tag=cat&limit=1", []string{"Mittens"}},
	} {
		resp := get(t, srv.URL+"/pets"+tc.query)
		var pets []Pet
		err := json.NewDecoder(resp.Body).Decode(&pets)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || err != nil {
			t.Fatalf("GET /pets%s = %d (%v), want 200", tc.query, resp.StatusCode, err)
		}
		names := make([]string, len(pets))
		for i, p := range pets {
			names[i] = p.Name
		}
		if strings.Join(names, ",") != strings.Join(tc.names, ",") {
			t.Fatalf("GET /pets%s = %v, want %v", tc.query, names, tc.names)
		}
	}

	for _, tc := range []struct{ query, message string }{
		{"?tag=fish", "validation failed: tag[0]: must be one of: dog, cat"},
		{"?tag=dog&tag=cat&tag=dog", "validation failed: tag: must be at most 2 items"},
	} {
		resp := get(t, srv.URL+"/pets"+tc.query)
		var body struct {
			Error string `json:"error"`
		}
		err := json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity || err != nil || body.Error != tc.message {
			t.Fatalf("GET /pets%s = %d %q (%v), want 422 %q", tc.query, resp.StatusCode, body.Error, err, tc.message)
		}
	}

	resp := get(t, srv.URL+"/openapi.json")
	defer func() { _ = resp.Body.Close() }()
	var doc struct {
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode the contract: %v", err)
	}
	got, err := json.Marshal(doc.Components.Schemas["PetTag"])
	if err != nil || string(got) != `{"enum":["dog","cat"],"type":"string"}` {
		t.Fatalf("PetTag component = %s (%v)", got, err)
	}
}
