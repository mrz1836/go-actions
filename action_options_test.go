package actions_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/go-actions"
)

// withMemoryStore builds a registry with an in-memory idempotency store whose
// keys all share one scope.
func withMemoryStore() actions.Option {
	return actions.WithIdempotency(actions.NewMemoryIdempotencyStore(), actions.IdempotencyOptions{
		Scope: func(*http.Request) string { return "" },
	})
}

// keyedAt returns a JSON action at method that takes a required key.
func keyedAt(method string) actions.Action[struct{}, pingResp] {
	return actions.Action[struct{}, pingResp]{
		ID: "t.key", Method: method, Path: "/key",
		Statuses:       []actions.StatusDoc{{Code: http.StatusOK}},
		IdempotencyKey: actions.IdempotencyKeyRequired,
		Handle:         okHandle[struct{}, pingResp],
	}
}

// rawAction returns an action that returns Raw with the given statuses.
func rawAction(statuses ...actions.StatusDoc) actions.Action[struct{}, actions.Raw] {
	return actions.Action[struct{}, actions.Raw]{
		ID: "t.raw", Method: http.MethodGet, Path: "/raw",
		Statuses: statuses,
		Handle:   okHandle[struct{}, actions.Raw],
	}
}

func TestFreezeRefusesBadKeyAnnotationAndRawDeclarations(t *testing.T) {
	t.Run("annotations that are both read-only and destructive", func(t *testing.T) {
		got := freezeOne(actions.Action[struct{}, pingResp]{
			ID: "t.ann", Method: http.MethodGet, Path: "/ann",
			Statuses:    []actions.StatusDoc{{Code: http.StatusOK}},
			Annotations: &actions.Annotations{ReadOnly: true, Destructive: true},
			Handle:      okHandle[struct{}, pingResp],
		})
		assert.Equal(t, `actions: action "t.ann": annotations cannot be both read-only and destructive`, got)
	})

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run("an idempotency key on "+method, func(t *testing.T) {
			got := freezeOne(keyedAt(method), withMemoryStore())
			assert.Equal(t, `actions: action "t.key": idempotency keys apply to POST and PATCH`, got)
		})
	}

	t.Run("an idempotency key on a registry without a store", func(t *testing.T) {
		got := freezeOne(keyedAt(http.MethodPost))
		assert.Equal(t, `actions: action "t.key" uses idempotency keys but the registry has no store`, got)
	})

	t.Run("an idempotency key on a registry given a nil store", func(t *testing.T) {
		got := freezeOne(keyedAt(http.MethodPost), actions.WithIdempotency(nil, actions.IdempotencyOptions{
			Scope: func(*http.Request) string { return "" },
		}))
		assert.Equal(t, `actions: action "t.key" uses idempotency keys but the registry has no store`, got)
	})

	t.Run("Raw without a MediaType on its 200", func(t *testing.T) {
		got := freezeOne(rawAction(actions.StatusDoc{Code: http.StatusOK}))
		assert.Equal(t, `actions: action "t.raw" returns Raw but its 200 StatusDoc declares no MediaType`, got)
	})

	t.Run("a MediaType on a JSON action", func(t *testing.T) {
		got := freezeOne(actions.Action[struct{}, pingResp]{
			ID: "t.json", Method: http.MethodGet, Path: "/json",
			Statuses: []actions.StatusDoc{{Code: http.StatusOK, MediaType: "text/plain"}},
			Handle:   okHandle[struct{}, pingResp],
		})
		assert.Equal(t, `actions: action "t.json": MediaType applies only to the 200 StatusDoc of an action that returns Raw`, got)
	})

	t.Run("a MediaType on an error StatusDoc of a Raw action", func(t *testing.T) {
		got := freezeOne(rawAction(
			actions.StatusDoc{Code: http.StatusOK, MediaType: "text/csv"},
			actions.StatusDoc{Code: http.StatusNotFound, Error: true, MediaType: "text/plain"},
		))
		assert.Equal(t, `actions: action "t.raw": MediaType applies only to the 200 StatusDoc of an action that returns Raw`, got)
	})

	t.Run("declarations that can work freeze", func(t *testing.T) {
		assert.Nil(t, freezeOne(keyedAt(http.MethodPost), withMemoryStore()))
		assert.Nil(t, freezeOne(keyedAt(http.MethodPatch), withMemoryStore()))
		assert.Nil(t, freezeOne(rawAction(
			actions.StatusDoc{Code: http.StatusOK, MediaType: "text/csv"},
			actions.StatusDoc{Code: http.StatusNotFound, Error: true},
		)))
		assert.Nil(t, freezeOne(actions.Action[struct{}, pingResp]{
			ID: "t.ann", Method: http.MethodDelete, Path: "/ann",
			Statuses:    []actions.StatusDoc{{Code: http.StatusOK}},
			Annotations: &actions.Annotations{Destructive: true, Idempotent: true},
			Handle:      okHandle[struct{}, pingResp],
		}))
	})
}

func TestWithIdempotencyNeedsAScope(t *testing.T) {
	assert.PanicsWithValue(t, "actions: WithIdempotency needs a Scope", func() {
		actions.WithIdempotency(actions.NewMemoryIdempotencyStore(), actions.IdempotencyOptions{})
	})
}
