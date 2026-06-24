package actions

import (
	"net/http"
	"testing"
)

func TestDefaultInfoIsNeutral(t *testing.T) {
	t.Parallel()
	info := defaultInfo()
	if info.title != "API" {
		t.Fatalf("default title = %q, want neutral 'API'", info.title)
	}
	if info.version == "" || info.description == "" {
		t.Fatalf("default info incomplete: %+v", info)
	}
}

func TestWithInfo(t *testing.T) {
	t.Parallel()

	t.Run("all fields override", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(WithInfo("Widgets", "desc", "9.9.9"))
		if r.info.title != "Widgets" || r.info.description != "desc" || r.info.version != "9.9.9" {
			t.Fatalf("info = %+v", r.info)
		}
	})

	t.Run("empty arguments leave defaults in place", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(WithInfo("", "", ""))
		def := defaultInfo()
		if r.info.title != def.title || r.info.description != def.description || r.info.version != def.version {
			t.Fatalf("empty WithInfo clobbered defaults: %+v", r.info)
		}
	})

	t.Run("partial override keeps the rest default", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(WithInfo("OnlyTitle", "", ""))
		if r.info.title != "OnlyTitle" || r.info.version != defaultInfo().version {
			t.Fatalf("info = %+v", r.info)
		}
	})
}

func TestWithErrorMapper(t *testing.T) {
	t.Parallel()

	t.Run("installs a custom mapper", func(t *testing.T) {
		t.Parallel()
		mapper := func(error) APIError { return APIError{Status: http.StatusTeapot} }
		r := NewRegistry(WithErrorMapper(mapper))
		if r.errorMapper(nil).Status != http.StatusTeapot {
			t.Fatal("custom mapper was not installed")
		}
	})

	t.Run("nil mapper is ignored, default retained", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(WithErrorMapper(nil))
		if r.errorMapper == nil {
			t.Fatal("nil mapper must not clear the default")
		}
		got := r.errorMapper(&APIError{Status: http.StatusNotFound, Code: CodeNotFound})
		if got.Status != http.StatusNotFound {
			t.Fatalf("default mapper not retained: %+v", got)
		}
	})
}

func TestWithStripPrefixMountPath(t *testing.T) {
	t.Parallel()

	t.Run("no prefix is the identity", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry()
		if got := r.mountPath("/v1/persons/{id}"); got != "/v1/persons/{id}" {
			t.Fatalf("mountPath = %q", got)
		}
	})

	t.Run("configured prefix is stripped", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(WithStripPrefix("/v1"))
		if got := r.mountPath("/v1/persons/{id}"); got != "/persons/{id}" {
			t.Fatalf("mountPath = %q, want /persons/{id}", got)
		}
	})

	t.Run("stripping down to nothing yields root", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(WithStripPrefix("/v1"))
		if got := r.mountPath("/v1"); got != "/" {
			t.Fatalf("mountPath = %q, want /", got)
		}
	})
}
