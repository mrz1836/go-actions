package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"
)

// stringType is the JSON Schema "string" type keyword, named here so the test
// file does not repeat the literal (goconst).
const stringType = "string"

// InnerSchema and OuterSchema are exported so the generator interns them.
type InnerSchema struct {
	Name string `json:"name" validate:"required"`
}

type OuterSchema struct {
	Inner InnerSchema `json:"inner"`
	Count int         `json:"count" validate:"min=1,max=10"`
}

//nolint:gocognit,gocyclo // Test function with multiple sub-tests
func TestSchemaFor(t *testing.T) {
	t.Parallel()

	t.Run("scalar types", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		tests := []struct {
			name string
			typ  reflect.Type
			want string
		}{
			{stringType, reflect.TypeOf(""), stringType},
			{"int", reflect.TypeOf(0), "integer"},
			{"bool", reflect.TypeOf(true), "boolean"},
			{"float", reflect.TypeOf(1.5), "number"},
			{"slice", reflect.TypeOf([]int{}), "array"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := b.schemaFor(tt.typ, modeRequest)["type"]; got != tt.want {
					t.Errorf("schemaFor(%s) type = %v, want %v", tt.typ, got, tt.want)
				}
			})
		}
	})

	t.Run("pointer unwraps to underlying type", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		var p *string
		if got := b.schemaFor(reflect.TypeOf(p), modeRequest)["type"]; got != stringType {
			t.Fatalf("type = %v, want string", got)
		}
	})

	t.Run("map and interface schemas", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		if got := b.schemaFor(reflect.TypeOf(map[string]int{}), modeRequest)["type"]; got != "object" {
			t.Fatalf("map type = %v, want object", got)
		}
		var iface any
		if got := b.schemaFor(reflect.TypeOf(&iface).Elem(), modeRequest); len(got) != 0 {
			t.Fatalf("interface schema = %v, want empty", got)
		}
	})

	t.Run("byte slice is arbitrary JSON", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		if got := b.schemaFor(reflect.TypeOf([]byte{}), modeRequest); len(got) != 0 {
			t.Fatalf("[]byte schema = %v, want empty", got)
		}
	})

	t.Run("time.Time maps to a date-time string", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		got := b.schemaFor(reflect.TypeOf(time.Time{}), modeRequest)
		if got["type"] != stringType || got["format"] != "date-time" {
			t.Fatalf("time schema = %v, want date-time string", got)
		}
	})

	t.Run("named struct interns into components", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		schema := b.schemaFor(reflect.TypeFor[OuterSchema](), modeRequest)

		ref, ok := schema["$ref"].(string)
		if !ok || ref != "#/components/schemas/OuterSchema" {
			t.Fatalf("expected a $ref to OuterSchema, got %v", schema)
		}
		components := b.components()
		if _, ok := components["OuterSchema"]; !ok {
			t.Fatal("OuterSchema was not interned into components")
		}
		if _, ok := components["InnerSchema"]; !ok {
			t.Fatal("nested InnerSchema was not interned into components")
		}
	})

	t.Run("validate tags translate to constraints", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		b.schemaFor(reflect.TypeFor[OuterSchema](), modeRequest)

		components := b.components()
		outer, _ := components["OuterSchema"].(map[string]any)
		props, _ := outer["properties"].(map[string]any)
		count, _ := props["count"].(map[string]any)
		if count["minimum"] != 1.0 || count["maximum"] != 10.0 {
			t.Fatalf("count constraints = %v, want minimum 1 / maximum 10", count)
		}

		inner, _ := components["InnerSchema"].(map[string]any)
		required, _ := inner["required"].([]string)
		if len(required) != 1 || required[0] != "name" {
			t.Fatalf("InnerSchema required = %v, want [name]", required)
		}
	})

	t.Run("string and format constraints", func(t *testing.T) {
		t.Parallel()
		type formatReq struct {
			Email string `json:"email" validate:"email,min=3,max=120"`
			ID    string `json:"id" validate:"uuid"`
			Phone string `json:"phone" validate:"e164"`
			Kind  string `json:"kind" validate:"oneof=a b"`
			When  string `json:"when" validate:"rfc3339"`
		}
		b := newSchemaBuilder()
		schema := b.structSchema(reflect.TypeFor[formatReq](), modeRequest)
		props, _ := schema["properties"].(map[string]any)

		email, _ := props["email"].(map[string]any)
		if email["format"] != "email" || email["minLength"] != 3.0 || email["maxLength"] != 120.0 {
			t.Fatalf("email schema = %v", email)
		}
		if id, _ := props["id"].(map[string]any); id["format"] != "uuid" {
			t.Fatalf("id schema = %v, want uuid format", id)
		}
		if phone, _ := props["phone"].(map[string]any); phone["pattern"] == nil {
			t.Fatalf("phone schema = %v, want e164 pattern", phone)
		}
		kind, _ := props["kind"].(map[string]any)
		enum, _ := kind["enum"].([]string)
		if len(enum) != 2 {
			t.Fatalf("kind enum = %v, want [a b]", enum)
		}
		if when, _ := props["when"].(map[string]any); when["format"] != "date-time" {
			t.Fatalf("when schema = %v, want date-time format", when)
		}
	})

	t.Run("unexported and json-skipped fields are omitted", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Visible string `json:"visible"`
			Skipped string `json:"-"`
			hidden  string //nolint:unused // exercises the unexported-field skip
		}
		b := newSchemaBuilder()
		schema := b.structSchema(reflect.TypeFor[req](), modeRequest)
		props, _ := schema["properties"].(map[string]any)
		if _, ok := props["visible"]; !ok {
			t.Fatal("visible field missing")
		}
		if len(props) != 1 {
			t.Fatalf("properties = %v, want only visible", props)
		}
	})

	t.Run("field without a json tag is keyed by its Go name", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Name string // no json tag → jsonName falls back to the field name
		}
		b := newSchemaBuilder()
		schema := b.structSchema(reflect.TypeFor[req](), modeRequest)
		props, _ := schema["properties"].(map[string]any)
		if _, ok := props["Name"]; !ok {
			t.Fatalf("properties = %v, want key Name", props)
		}
	})
}

// TestResponseSchema_EmptyHasNoBody covers the Empty branch of responseSchema:
// an envelope whose Body field is absent yields no response schema.
func TestResponseSchema_EmptyHasNoBody(t *testing.T) {
	t.Parallel()
	b := newSchemaBuilder()
	schema, hasBody := b.responseSchema(reflect.TypeFor[Empty]())
	if hasBody || schema != nil {
		t.Fatalf("responseSchema(Empty) = (%v, %v), want (nil, false)", schema, hasBody)
	}
}

// TestBuildRequestBody covers the no-body branches: a non-struct request type
// and a struct whose fields are all bound from path/query (no JSON body).
func TestBuildRequestBody(t *testing.T) {
	t.Parallel()

	t.Run("non-struct request type has no body", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		if got := buildRequestBody(b, reflect.TypeOf("")); got != nil {
			t.Fatalf("buildRequestBody(string) = %v, want nil", got)
		}
	})

	t.Run("request with only path/query fields has no body", func(t *testing.T) {
		t.Parallel()
		type req struct {
			ID    string `json:"-" path:"id"`
			Limit int    `json:"-" query:"limit"`
		}
		b := newSchemaBuilder()
		if got := buildRequestBody(b, reflect.TypeFor[req]()); got != nil {
			t.Fatalf("buildRequestBody(no-body req) = %v, want nil", got)
		}
	})

	t.Run("request with JSON fields produces a required body", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Name string `json:"name"`
		}
		b := newSchemaBuilder()
		got := buildRequestBody(b, reflect.TypeFor[req]())
		if got == nil || got["required"] != true {
			t.Fatalf("buildRequestBody(body req) = %v, want a required body", got)
		}
	})
}

// componentRef is the $ref prefix for a named component schema.
const componentRef = "#/components/schemas/"

// RespLeaf is a named struct referenced through a pointer from RespView.
type RespLeaf struct {
	Name string `json:"name"`
}

// RespView exercises every response-mode rule: required from a missing
// omitempty, optional from omitempty/omitzero, nullable pointers, and the
// openapi:"type=object" override.
type RespView struct {
	ID    string           `json:"id"`
	Note  string           `json:"note,omitempty"`
	Seen  time.Time        `json:"seen,omitzero"`
	Nick  *string          `json:"nick"`
	Age   *int             `json:"age,omitempty"`
	When  *time.Time       `json:"when"`
	Leaf  *RespLeaf        `json:"leaf"`
	Kind  *string          `json:"kind" validate:"oneof=a b"`
	Raw   json.RawMessage  `json:"raw" openapi:"type=object"`
	Blob  any              `json:"blob"`
	Items []string         `json:"items"`
	Opaq  *json.RawMessage `json:"opaq"`
}

// reqBody is a request body: required stays driven by validate:"required" and
// pointers stay non-nullable.
type reqBody struct {
	Name string          `json:"name" validate:"required"`
	Nick *string         `json:"nick"`
	Note string          `json:"note"`
	Raw  json.RawMessage `json:"raw" openapi:"type=object"`
}

// SharedSame has the same schema in request and response mode.
type SharedSame struct {
	ID string `json:"id" validate:"required"`
}

// SharedDiff differs by mode: "note" is required only in a response.
type SharedDiff struct {
	ID   string `json:"id" validate:"required"`
	Note string `json:"note"`
}

// SharedWrap is identical by mode on its own but nests SharedDiff, so it
// differs transitively.
type SharedWrap struct {
	Inner SharedDiff `json:"inner" validate:"required"`
}

// SharedNode is self-referential and identical by mode; the cycle must not make
// it differ.
type SharedNode struct {
	Name string      `json:"name" validate:"required"`
	Next *SharedNode `json:"next,omitempty"`
}

// sharedReq and SharedResp use the shared types in both modes.
type sharedReq struct {
	Same SharedSame `json:"same"`
	Wrap SharedWrap `json:"wrap"`
	Node SharedNode `json:"node"`
}

type SharedResp struct {
	Same SharedSame `json:"same"`
	Wrap SharedWrap `json:"wrap"`
	Node SharedNode `json:"node"`
}

// Clash differs by mode, and ClashInput already names another component, so the
// request variant of Clash has no free name.
type Clash struct {
	Note string `json:"note"`
}

type ClashInput struct {
	X string `json:"x" validate:"required"`
}

type clashReq struct {
	Clash Clash      `json:"clash"`
	Other ClashInput `json:"other"`
}

// contractFor registers one POST action with request type Req and response type
// Resp, freezes the registry, and returns the decoded OpenAPI document.
func contractFor[Req, Resp any](t *testing.T, opts ...Option) map[string]any {
	t.Helper()
	reg := NewRegistry(opts...)
	Register(reg, Action[Req, Resp]{
		ID:       "test.schema",
		Method:   http.MethodPost,
		Path:     "/schema",
		Summary:  "Schema fixture",
		Statuses: []StatusDoc{{Code: http.StatusOK, Description: "ok"}},
		Handle: func(context.Context, Req) (Resp, error) {
			var zero Resp
			return zero, nil
		},
	})
	reg.Freeze()
	var doc map[string]any
	if err := json.Unmarshal(reg.OpenAPIJSON(), &doc); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}
	return doc
}

// componentsOf returns components.schemas from a decoded document.
func componentsOf(doc map[string]any) map[string]any {
	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	return schemas
}

// component returns one named component schema, failing when it is absent.
func component(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	schema, ok := componentsOf(doc)[name].(map[string]any)
	if !ok {
		t.Fatalf("component %q missing; have %v", name, keysOf(componentsOf(doc)))
	}
	return schema
}

// requestBodyOf returns the fixture operation's inline request-body schema.
func requestBodyOf(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	paths, _ := doc["paths"].(map[string]any)
	item, _ := paths["/schema"].(map[string]any)
	op, _ := item["post"].(map[string]any)
	body, _ := op["requestBody"].(map[string]any)
	content, _ := body["content"].(map[string]any)
	appJSON, _ := content["application/json"].(map[string]any)
	schema, ok := appJSON["schema"].(map[string]any)
	if !ok {
		t.Fatal("request body schema missing")
	}
	return schema
}

// propertyOf returns one property schema of an object schema.
func propertyOf(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	props, _ := schema["properties"].(map[string]any)
	prop, ok := props[name].(map[string]any)
	if !ok {
		t.Fatalf("property %q missing from %v", name, schema)
	}
	return prop
}

// requiredOf returns an object schema's required list as strings.
func requiredOf(schema map[string]any) []string {
	raw, _ := schema["required"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

// keysOf returns the sorted keys of a map.
func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// jsonOf renders a decoded schema fragment for comparison and failure output.
func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

//nolint:gocognit,gocyclo // Test function with multiple sub-tests
func TestSchemaResponseMode(t *testing.T) {
	t.Parallel()
	doc := contractFor[struct{}, RespView](t)
	view := component(t, doc, "RespView")

	t.Run("fields without omitempty are required", func(t *testing.T) {
		t.Parallel()
		got := requiredOf(view)
		want := []string{"id", "nick", "when", "leaf", "kind", "raw", "blob", "items", "opaq"}
		if !slices.Equal(got, want) {
			t.Fatalf("required = %v, want %v", got, want)
		}
	})

	t.Run("omitempty and omitzero fields are optional", func(t *testing.T) {
		t.Parallel()
		got := requiredOf(view)
		for _, name := range []string{"note", "seen", "age"} {
			if slices.Contains(got, name) {
				t.Fatalf("%q is required, want optional (required = %v)", name, got)
			}
		}
		if age := propertyOf(t, view, "age"); age["type"] != "integer" {
			t.Fatalf("age = %v, want a non-nullable integer", age)
		}
	})

	t.Run("pointers without omitempty are nullable", func(t *testing.T) {
		t.Parallel()
		if got := jsonOf(t, propertyOf(t, view, "nick")); got != `{"type":["string","null"]}` {
			t.Fatalf("nick = %s", got)
		}
		if got := jsonOf(t, propertyOf(t, view, "when")); got != `{"format":"date-time","type":["string","null"]}` {
			t.Fatalf("when = %s", got)
		}
	})

	t.Run("pointer to a named struct is a oneOf ref or null", func(t *testing.T) {
		t.Parallel()
		want := `{"oneOf":[{"$ref":"` + componentRef + `RespLeaf"},{"type":"null"}]}`
		if got := jsonOf(t, propertyOf(t, view, "leaf")); got != want {
			t.Fatalf("leaf = %s, want %s", got, want)
		}
	})

	t.Run("a nullable enum admits null", func(t *testing.T) {
		t.Parallel()
		if got := jsonOf(t, propertyOf(t, view, "kind")); got != `{"enum":["a","b",null],"type":["string","null"]}` {
			t.Fatalf("kind = %s", got)
		}
	})

	t.Run("type=object override", func(t *testing.T) {
		t.Parallel()
		if got := jsonOf(t, propertyOf(t, view, "raw")); got != `{"additionalProperties":true,"type":"object"}` {
			t.Fatalf("raw = %s", got)
		}
	})

	t.Run("an empty schema and slices stay as they are", func(t *testing.T) {
		t.Parallel()
		if got := jsonOf(t, propertyOf(t, view, "blob")); got != `{}` {
			t.Fatalf("blob = %s", got)
		}
		if got := jsonOf(t, propertyOf(t, view, "items")); got != `{"items":{"type":"string"},"type":"array"}` {
			t.Fatalf("items = %s", got)
		}
		if got := jsonOf(t, propertyOf(t, view, "opaq")); got != `{}` {
			t.Fatalf("opaq = %s, want {} (it already admits null)", got)
		}
	})
}

func TestSchemaRequestModeUnchanged(t *testing.T) {
	t.Parallel()
	body := requestBodyOf(t, contractFor[reqBody, Empty](t))

	if got := requiredOf(body); !slices.Equal(got, []string{"name"}) {
		t.Fatalf("request required = %v, want [name]", got)
	}
	if got := jsonOf(t, propertyOf(t, body, "nick")); got != `{"type":"string"}` {
		t.Fatalf("request nick = %s, want a plain string", got)
	}
	if got := jsonOf(t, propertyOf(t, body, "raw")); got != `{"additionalProperties":true,"type":"object"}` {
		t.Fatalf("request raw = %s, want the type=object override", got)
	}
}

//nolint:gocognit // Test function with multiple sub-tests
func TestSchemaSharedTypeInputSplit(t *testing.T) {
	t.Parallel()
	doc := contractFor[sharedReq, SharedResp](t)
	schemas := componentsOf(doc)
	body := requestBodyOf(t, doc)

	t.Run("identical schemas keep one component", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"SharedSameInput", "SharedNodeInput"} {
			if _, ok := schemas[name]; ok {
				t.Fatalf("%s emitted, want one shared component", name)
			}
		}
		if ref := propertyOf(t, body, "same")["$ref"]; ref != componentRef+"SharedSame" {
			t.Fatalf("request same $ref = %v", ref)
		}
		if ref := propertyOf(t, body, "node")["$ref"]; ref != componentRef+"SharedNode" {
			t.Fatalf("request node $ref = %v (a cycle must default to equal)", ref)
		}
	})

	t.Run("differing schemas split into Name and NameInput", func(t *testing.T) {
		t.Parallel()
		resp := component(t, doc, "SharedDiff")
		req := component(t, doc, "SharedDiffInput")
		if got := requiredOf(resp); !slices.Equal(got, []string{"id", "note"}) {
			t.Fatalf("SharedDiff required = %v, want [id note]", got)
		}
		if got := requiredOf(req); !slices.Equal(got, []string{"id"}) {
			t.Fatalf("SharedDiffInput required = %v, want [id]", got)
		}
	})

	t.Run("a type nesting a differing type splits too", func(t *testing.T) {
		t.Parallel()
		wrap := component(t, doc, "SharedWrap")
		wrapInput := component(t, doc, "SharedWrapInput")
		if ref := propertyOf(t, wrap, "inner")["$ref"]; ref != componentRef+"SharedDiff" {
			t.Fatalf("SharedWrap.inner $ref = %v", ref)
		}
		if ref := propertyOf(t, wrapInput, "inner")["$ref"]; ref != componentRef+"SharedDiffInput" {
			t.Fatalf("SharedWrapInput.inner $ref = %v", ref)
		}
		if ref := propertyOf(t, body, "wrap")["$ref"]; ref != componentRef+"SharedWrapInput" {
			t.Fatalf("request wrap $ref = %v", ref)
		}
		if ref := propertyOf(t, component(t, doc, "SharedResp"), "wrap")["$ref"]; ref != componentRef+"SharedWrap" {
			t.Fatalf("response wrap $ref = %v", ref)
		}
	})

	t.Run("a request-only type keeps its name", func(t *testing.T) {
		t.Parallel()
		doc := contractFor[sharedReq, Empty](t)
		for _, name := range []string{"SharedDiff", "SharedWrap"} {
			if _, ok := componentsOf(doc)[name]; !ok {
				t.Fatalf("%s missing; have %v", name, keysOf(componentsOf(doc)))
			}
			if _, ok := componentsOf(doc)[name+"Input"]; ok {
				t.Fatalf("%sInput emitted for a request-only type", name)
			}
		}
	})
}

func TestSchemaInputNameCollisionPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic when <Name>Input is already a component")
		}
	}()
	contractFor[clashReq, Clash](t)
}

func TestSchemaNullableOpenAPI30(t *testing.T) {
	t.Parallel()
	view := component(t, contractFor[struct{}, RespView](t, WithOpenAPIVersion("3.0.3")), "RespView")

	if got := jsonOf(t, propertyOf(t, view, "nick")); got != `{"nullable":true,"type":"string"}` {
		t.Fatalf("nick = %s", got)
	}
	want := `{"allOf":[{"$ref":"` + componentRef + `RespLeaf"}],"nullable":true}`
	if got := jsonOf(t, propertyOf(t, view, "leaf")); got != want {
		t.Fatalf("leaf = %s, want %s", got, want)
	}
	if got := jsonOf(t, propertyOf(t, view, "kind")); got != `{"enum":["a","b",null],"nullable":true,"type":"string"}` {
		t.Fatalf("kind = %s", got)
	}
}

func TestSchemaRefNames(t *testing.T) {
	t.Parallel()
	fragment := map[string]any{
		"properties": map[string]any{
			"a": map[string]any{"$ref": componentRef + "Alpha"},
			"b": map[string]any{"oneOf": []any{map[string]any{"$ref": componentRef + "Beta"}}},
		},
	}
	got := refNames(fragment)
	slices.Sort(got)
	if !slices.Equal(got, []string{"Alpha", "Beta"}) {
		t.Fatalf("refNames = %v, want [Alpha Beta]", got)
	}
}
