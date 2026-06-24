package actions

import (
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// schemaTypeKey is the JSON Schema "type" keyword.
const schemaTypeKey = "type"

// timeType is the reflect.Type of time.Time, schema-mapped to a date-time string.
//
//nolint:gochecknoglobals // a cached reflect.Type is an intentional package global
var timeType = reflect.TypeOf(time.Time{})

// envelopeType is the reflect.Type of the responseEnvelope interface.
//
//nolint:gochecknoglobals // a cached reflect.Type is an intentional package global
var envelopeType = reflect.TypeOf((*responseEnvelope)(nil)).Elem()

// schemaBuilder generates JSON Schema 2020-12 fragments. Named struct types are
// interned into components and referenced by $ref so the document stays small
// and the names are human-meaningful.
type schemaBuilder struct {
	components map[string]any
}

// newSchemaBuilder returns an empty schemaBuilder.
func newSchemaBuilder() *schemaBuilder {
	return &schemaBuilder{components: map[string]any{}}
}

// schemaFor returns the JSON Schema for t.
//
//nolint:gocyclo // one switch over the Go kind taxonomy
func (b *schemaBuilder) schemaFor(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() { //nolint:exhaustive // unsupported kinds fall through to the default
	case reflect.String:
		return map[string]any{schemaTypeKey: "string"}
	case reflect.Bool:
		return map[string]any{schemaTypeKey: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{schemaTypeKey: "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{schemaTypeKey: "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			// []byte — arbitrary embedded JSON.
			return map[string]any{}
		}
		return map[string]any{schemaTypeKey: "array", "items": b.schemaFor(t.Elem())}
	case reflect.Map:
		return map[string]any{schemaTypeKey: "object", "additionalProperties": b.schemaFor(t.Elem())}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Struct:
		return b.structOrRef(t)
	default:
		return map[string]any{}
	}
}

// structOrRef emits a struct schema inline, or interns a named struct type into
// components and returns a $ref to it.
func (b *schemaBuilder) structOrRef(t reflect.Type) map[string]any {
	if t == timeType {
		return map[string]any{schemaTypeKey: "string", "format": "date-time"}
	}
	name := t.Name()
	if name == "" || !isExported(name) || strings.ContainsAny(name, "[]") {
		return b.structSchema(t)
	}
	if _, seen := b.components[name]; !seen {
		b.components[name] = map[string]any{} // placeholder breaks recursion
		b.components[name] = b.structSchema(t)
	}
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

// structSchema builds the object schema for a struct's JSON-encoded fields.
func (b *schemaBuilder) structSchema(t reflect.Type) map[string]any {
	properties := map[string]any{}
	var required []string
	for i := range t.NumField() {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported
		}
		name := jsonName(f)
		if name == "" || name == "-" {
			continue
		}
		fieldSchema := b.schemaFor(f.Type)
		applyConstraints(fieldSchema, f.Tag.Get("validate"))
		properties[name] = fieldSchema
		if hasRule(f.Tag.Get("validate"), "required") {
			required = append(required, name)
		}
	}
	schema := map[string]any{schemaTypeKey: "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// responseSchema returns the JSON Schema of an action's response body. An Empty
// response has no body; a Created/Accepted wrapper unwraps to its Body type.
func (b *schemaBuilder) responseSchema(respType reflect.Type) (map[string]any, bool) {
	if respType.Implements(envelopeType) || reflect.PointerTo(respType).Implements(envelopeType) {
		if field, ok := respType.FieldByName("Body"); ok {
			return b.schemaFor(field.Type), true
		}
		return nil, false // Empty
	}
	return b.schemaFor(respType), true
}

// applyConstraints folds validate-tag rules into a field schema.
//
//nolint:gocyclo // one switch over the small fixed rule vocabulary
func applyConstraints(schema map[string]any, validateTag string) {
	if validateTag == "" {
		return
	}
	isString := schema[schemaTypeKey] == "string"
	isNumber := schema[schemaTypeKey] == "integer" || schema[schemaTypeKey] == "number"
	for _, rule := range strings.Split(validateTag, ",") {
		key, arg, _ := strings.Cut(strings.TrimSpace(rule), "=")
		switch key {
		case "min":
			if n, err := strconv.ParseFloat(arg, 64); err == nil {
				switch {
				case isString:
					schema["minLength"] = n
				case isNumber:
					schema["minimum"] = n
				}
			}
		case "max":
			if n, err := strconv.ParseFloat(arg, 64); err == nil {
				switch {
				case isString:
					schema["maxLength"] = n
				case isNumber:
					schema["maximum"] = n
				}
			}
		case "oneof":
			schema["enum"] = strings.Fields(arg)
		case "uuid":
			schema["format"] = "uuid"
		case "email":
			schema["format"] = "email"
		case "rfc3339":
			schema["format"] = "date-time"
		case "e164":
			schema["pattern"] = `^\+[1-9]\d{1,14}$`
		}
	}
}

// jsonName returns a struct field's JSON name.
func jsonName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	return strings.Split(tag, ",")[0]
}

// hasRule reports whether a validate tag contains the named rule.
func hasRule(tag, rule string) bool {
	for _, r := range strings.Split(tag, ",") {
		key, _, _ := strings.Cut(strings.TrimSpace(r), "=")
		if key == rule {
			return true
		}
	}
	return false
}

// isExported reports whether name begins with an uppercase letter.
func isExported(name string) bool {
	return name != "" && unicode.IsUpper(rune(name[0]))
}
