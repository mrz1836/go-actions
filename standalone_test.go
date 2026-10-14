package actions

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wantDialect is the $schema every SchemaFor document declares.
const wantDialect = "https://json-schema.org/draft/2020-12/schema"

// Fixtures for SchemaFor and Validate.
type (
	// StandaloneAddress is a named struct a payload nests.
	StandaloneAddress struct {
		City string `json:"city" validate:"required" doc:"City name" example:"Lisbon"`
		Zip  string `json:"zip,omitempty" validate:"max=10"`
	}
	// standalonePayload is a payload checked outside a request: named struct
	// and enum fields, element rules, a pointer, and a map.
	standalonePayload struct {
		Name    string                       `json:"name" validate:"required,max=20" doc:"Display name" example:"Ada"`
		State   EnumState                    `json:"state" doc:"Where it stands"`
		Tags    []string                     `json:"tags" validate:"max=3,dive,oneof=a b c"`
		Address StandaloneAddress            `json:"address"`
		Backup  *StandaloneAddress           `json:"backup,omitempty"`
		Notes   map[string]StandaloneAddress `json:"notes,omitempty"`
		Prio    *EnumPriority                `json:"prio,omitempty" validate:"required"`
	}
	// SchemaNode refers to itself through a slice.
	SchemaNode struct {
		Name     string       `json:"name"`
		Children []SchemaNode `json:"children"`
	}
	// SchemaTree holds a type on a cycle without being on one.
	SchemaTree struct {
		Root SchemaNode `json:"root"`
	}
	// SchemaPing and SchemaPong refer to each other.
	SchemaPing struct {
		Pong *SchemaPong `json:"pong"`
	}
	SchemaPong struct {
		Ping *SchemaPing `json:"ping"`
	}
	// SchemaLoop reaches itself through an unexported type, whose schema is
	// inlined.
	SchemaLoop struct {
		Via *schemaVia `json:"via"`
	}
	schemaVia struct {
		Back *SchemaLoop `json:"back"`
	}
	// schemaNodeOuter names the package's SchemaNode in a test that declares
	// its own.
	schemaNodeOuter = SchemaNode
	// standaloneBadRule carries a rule no type takes.
	standaloneBadRule struct {
		Email string `json:"email" validate:"emial"`
	}
	// standaloneBadExample carries an example its rules refuse.
	standaloneBadExample struct {
		Limit int `json:"limit" example:"300" validate:"max=200"`
	}
	// StandaloneOrder has tag rules and a Validatable hook.
	StandaloneOrder struct {
		Qty     int                `json:"qty" validate:"min=1"`
		State   EnumState          `json:"state"`
		Tags    []string           `json:"tags" validate:"dive,oneof=a b"`
		Address *StandaloneAddress `json:"address"`
		Note    string             `json:"note"`
	}
)

// errNoteSaysNo is StandaloneOrder's hook failure.
var errNoteSaysNo = errors.New("the note says no")

// Validate refuses a note that says no.
func (o *StandaloneOrder) Validate() error {
	if o.Note == "no" {
		return errNoteSaysNo
	}
	return nil
}

// schemaOf returns SchemaFor[T]'s document, decoded, failing on an error.
func schemaOf[T any](t *testing.T) map[string]any {
	t.Helper()
	raw, err := SchemaFor[T]()
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc
}

// inlineRefs returns v with every component $ref replaced by the component's
// own schema, its sibling keywords kept beside it.
func inlineRefs(t *testing.T, components map[string]any, v any) any {
	t.Helper()
	switch node := v.(type) {
	case map[string]any:
		out := map[string]any{}
		if ref, ok := node["$ref"].(string); ok {
			target, found := components[strings.TrimPrefix(ref, componentRefPrefix)]
			require.True(t, found, "no component for %s", ref)
			inlined, _ := inlineRefs(t, components, target).(map[string]any)
			maps.Copy(out, inlined)
		}
		for key, child := range node {
			if key != "$ref" {
				out[key] = inlineRefs(t, components, child)
			}
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, child := range node {
			out[i] = inlineRefs(t, components, child)
		}
		return out
	default:
		return v
	}
}

func TestSchemaForAgreesWithTheRegistrysSchema(t *testing.T) {
	doc := contractFor[standalonePayload, Empty](t)
	registered := requestBodyOf(t, doc)
	require.Contains(t, jsonOf(t, registered), `"$ref"`, "the registry's schema references components")
	want := inlineRefs(t, componentsOf(doc), registered)

	got := schemaOf[standalonePayload](t)
	assert.Equal(t, wantDialect, got["$schema"])
	delete(got, "$schema")
	assert.JSONEq(t, jsonOf(t, want), jsonOf(t, got))
}

func TestSchemaForInlinesNamedTypesAndDefsOnlyCycles(t *testing.T) {
	node := `{"type":"object","properties":{"name":{"type":"string"},` +
		`"children":{"type":"array","items":{"$ref":"#/$defs/SchemaNode"}}}}`
	t.Run("named types are inlined", func(t *testing.T) {
		got := schemaOf[standalonePayload](t)
		rendered := jsonOf(t, got)
		assert.NotContains(t, rendered, "$ref")
		assert.NotContains(t, rendered, "$defs")
		assert.JSONEq(t, `{"type":"object","properties":{`+
			`"city":{"type":"string","description":"City name","examples":["Lisbon"]},`+
			`"zip":{"type":"string","maxLength":10}},"required":["city"]}`,
			jsonOf(t, propertyOf(t, got, "address")), "a named struct")
		assert.JSONEq(t, `{"type":"string","enum":["queued","running","failed"],"description":"Where it stands"}`,
			jsonOf(t, propertyOf(t, got, "state")), "an enum")
	})
	t.Run("a type that refers to itself is in $defs", func(t *testing.T) {
		assert.JSONEq(t, `{"$schema":"`+wantDialect+`","$ref":"#/$defs/SchemaNode",`+
			`"$defs":{"SchemaNode":`+node+`}}`, jsonOf(t, schemaOf[SchemaNode](t)))
	})
	t.Run("a type that holds a cycle is not in $defs", func(t *testing.T) {
		assert.JSONEq(t, `{"$schema":"`+wantDialect+`","type":"object",`+
			`"properties":{"root":{"$ref":"#/$defs/SchemaNode"}},"$defs":{"SchemaNode":`+node+`}}`,
			jsonOf(t, schemaOf[SchemaTree](t)))
	})
	t.Run("two types that refer to each other are both in $defs", func(t *testing.T) {
		assert.JSONEq(t, `{"$schema":"`+wantDialect+`","$ref":"#/$defs/SchemaPing","$defs":{`+
			`"SchemaPing":{"type":"object","properties":{"pong":{"$ref":"#/$defs/SchemaPong"}}},`+
			`"SchemaPong":{"type":"object","properties":{"ping":{"$ref":"#/$defs/SchemaPing"}}}}}`,
			jsonOf(t, schemaOf[SchemaPing](t)))
	})
	t.Run("an unexported type on a cycle is inlined", func(t *testing.T) {
		assert.JSONEq(t, `{"$schema":"`+wantDialect+`","$ref":"#/$defs/SchemaLoop","$defs":{`+
			`"SchemaLoop":{"type":"object","properties":{"via":{"type":"object","properties":{`+
			`"back":{"$ref":"#/$defs/SchemaLoop"}}}}}}}`,
			jsonOf(t, schemaOf[SchemaLoop](t)))
	})
}

func TestSchemaForReturnsADeclarationError(t *testing.T) {
	const pkg = "github.com/mrz1836/go-actions."
	t.Run("an unknown rule", func(t *testing.T) {
		var (
			raw json.RawMessage
			err error
		)
		assert.NotPanics(t, func() { raw, err = SchemaFor[standaloneBadRule]() })
		assert.Nil(t, raw)
		require.EqualError(t, err,
			`actions: type `+pkg+`standaloneBadRule: field "email": unknown validate rule "emial"`)
	})
	t.Run("an example its rules refuse", func(t *testing.T) {
		_, err := SchemaFor[*standaloneBadExample]()
		require.EqualError(t, err,
			`actions: type `+pkg+`standaloneBadExample: field "limit": example must be at most 200`)
	})
	t.Run("two types on cycles with one name", func(t *testing.T) {
		type SchemaNode struct {
			Kids []SchemaNode `json:"kids"`
		}
		type both struct {
			Outer schemaNodeOuter `json:"outer"`
			Inner SchemaNode      `json:"inner"`
		}
		_, err := SchemaFor[both]()
		require.EqualError(t, err, `actions: type `+pkg+`both: types `+pkg+`SchemaNode and `+pkg+
			`SchemaNode both map to the $defs name "SchemaNode"`)
	})
}

// validationMessage renders field failures as a 422's error message does.
func validationMessage(fields []FieldError) string {
	parts := make([]string, len(fields))
	for i, fe := range fields {
		parts[i] = fe.Message
		if fe.Field != "" {
			parts[i] = fe.Field + ": " + fe.Message
		}
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

func TestValidateReportsTheFieldsARequestWould(t *testing.T) {
	reg := NewRegistry()
	Register(reg, Action[StandaloneOrder, Empty]{
		ID: "orders.create", Method: http.MethodPost, Path: "/orders",
		Statuses: []StatusDoc{{Code: http.StatusNoContent}},
		Handle:   func(context.Context, StandaloneOrder) (Empty, error) { return Empty{}, nil },
	})
	reg.Freeze()
	handler := reg.Handler()

	for _, tc := range []struct {
		name string
		body string
	}{
		{
			"tag rules at every depth, then the hook",
			`{"qty":-1,"state":"paused","tags":["a","z"],"address":{"city":""},"note":"no"}`,
		},
		{"the hook alone", `{"qty":1,"state":"queued","note":"no"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/orders", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			var served errorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &served))

			var order StandaloneOrder
			require.NoError(t, json.Unmarshal([]byte(tc.body), &order))
			var apiErr *APIError
			require.ErrorAs(t, Validate(&order), &apiErr)
			assert.Equal(t, http.StatusUnprocessableEntity, apiErr.Status)
			assert.Equal(t, CodeValidation, apiErr.Code)
			assert.Equal(t, served.Error, validationMessage(apiErr.Fields))
		})
	}

	t.Run("a valid value", func(t *testing.T) {
		order := StandaloneOrder{Qty: 1, State: "queued", Tags: []string{"a"}, Address: &StandaloneAddress{City: "Lisbon"}}
		require.NoError(t, Validate(&order))
	})
}

func TestValidateReturnsADeclarationErrorThatIsNotAnAPIError(t *testing.T) {
	t.Run("tags that cannot compile", func(t *testing.T) {
		err := Validate(&standaloneBadRule{Email: "ada@example.com"})
		require.EqualError(t, err,
			`actions: type github.com/mrz1836/go-actions.standaloneBadRule: field "email": unknown validate rule "emial"`)
		var apiErr *APIError
		assert.NotErrorAs(t, err, &apiErr)
		assert.Equal(t, err.Error(), Validate(standaloneBadRule{}).Error(), "the same error, by value too")
	})
	t.Run("anything but a struct is valid", func(t *testing.T) {
		var nilOrder *StandaloneOrder
		for _, v := range []any{nil, "text", 42, nilOrder, []StandaloneOrder{{Qty: -1}}} {
			assert.NoError(t, Validate(v), "%T", v)
		}
	})
}
