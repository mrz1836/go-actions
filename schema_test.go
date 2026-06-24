package actions

import (
	"reflect"
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
				if got := b.schemaFor(tt.typ)["type"]; got != tt.want {
					t.Errorf("schemaFor(%s) type = %v, want %v", tt.typ, got, tt.want)
				}
			})
		}
	})

	t.Run("pointer unwraps to underlying type", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		var p *string
		if got := b.schemaFor(reflect.TypeOf(p))["type"]; got != stringType {
			t.Fatalf("type = %v, want string", got)
		}
	})

	t.Run("map and interface schemas", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		if got := b.schemaFor(reflect.TypeOf(map[string]int{}))["type"]; got != "object" {
			t.Fatalf("map type = %v, want object", got)
		}
		var iface any
		if got := b.schemaFor(reflect.TypeOf(&iface).Elem()); len(got) != 0 {
			t.Fatalf("interface schema = %v, want empty", got)
		}
	})

	t.Run("byte slice is arbitrary JSON", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		if got := b.schemaFor(reflect.TypeOf([]byte{})); len(got) != 0 {
			t.Fatalf("[]byte schema = %v, want empty", got)
		}
	})

	t.Run("time.Time maps to a date-time string", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		got := b.schemaFor(reflect.TypeOf(time.Time{}))
		if got["type"] != stringType || got["format"] != "date-time" {
			t.Fatalf("time schema = %v, want date-time string", got)
		}
	})

	t.Run("named struct interns into components", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		schema := b.schemaFor(reflect.TypeFor[OuterSchema]())

		ref, ok := schema["$ref"].(string)
		if !ok || ref != "#/components/schemas/OuterSchema" {
			t.Fatalf("expected a $ref to OuterSchema, got %v", schema)
		}
		if _, ok := b.components["OuterSchema"]; !ok {
			t.Fatal("OuterSchema was not interned into components")
		}
		if _, ok := b.components["InnerSchema"]; !ok {
			t.Fatal("nested InnerSchema was not interned into components")
		}
	})

	t.Run("validate tags translate to constraints", func(t *testing.T) {
		t.Parallel()
		b := newSchemaBuilder()
		b.schemaFor(reflect.TypeFor[OuterSchema]())

		outer, _ := b.components["OuterSchema"].(map[string]any)
		props, _ := outer["properties"].(map[string]any)
		count, _ := props["count"].(map[string]any)
		if count["minimum"] != 1.0 || count["maximum"] != 10.0 {
			t.Fatalf("count constraints = %v, want minimum 1 / maximum 10", count)
		}

		inner, _ := b.components["InnerSchema"].(map[string]any)
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
		schema := b.structSchema(reflect.TypeFor[formatReq]())
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
		schema := b.structSchema(reflect.TypeFor[req]())
		props, _ := schema["properties"].(map[string]any)
		if _, ok := props["visible"]; !ok {
			t.Fatal("visible field missing")
		}
		if len(props) != 1 {
			t.Fatalf("properties = %v, want only visible", props)
		}
	})
}
