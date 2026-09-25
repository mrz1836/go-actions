package actions

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// schemaTypeKey is the JSON Schema "type" keyword.
const schemaTypeKey = "type"

// componentRefPrefix is the JSON pointer prefix of a named component schema.
const componentRefPrefix = "#/components/schemas/"

// inputSuffix names the request variant of a component whose request and
// response schemas differ.
const inputSuffix = "Input"

// schemaMode selects how struct fields become schema properties.
type schemaMode int

const (
	// modeRequest derives required from validate:"required" only; pointers are
	// not nullable. Request bodies, parameters, and the types they nest use it.
	modeRequest schemaMode = iota
	// modeResponse also requires every field the encoder always writes (a json
	// tag without omitempty/omitzero) and marks such pointer fields nullable,
	// because a nil pointer encodes as null. Response bodies use it.
	modeResponse
)

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
// and the names are human-meaningful. Each mode interns separately; components
// merges the two.
type schemaBuilder struct {
	// nullableKeyword marks nullability the OpenAPI 3.0 way ("nullable": true)
	// instead of with a "null" type, which only 3.1 allows.
	nullableKeyword bool

	request  map[string]map[string]any
	response map[string]map[string]any
	// requestRefs are the $ref nodes emitted in request mode, which components
	// retargets to <Name>Input when that name's schemas differ.
	requestRefs []refNode
}

// refNode is one emitted $ref object and the component name it points to.
type refNode struct {
	name string
	node map[string]any
}

// newSchemaBuilder returns an empty schemaBuilder.
func newSchemaBuilder() *schemaBuilder {
	return &schemaBuilder{
		request:  map[string]map[string]any{},
		response: map[string]map[string]any{},
	}
}

// schemaFor returns the JSON Schema for t in the given mode.
//
//nolint:gocyclo // one switch over the Go kind taxonomy
func (b *schemaBuilder) schemaFor(t reflect.Type, mode schemaMode) map[string]any {
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
		return map[string]any{schemaTypeKey: "array", "items": b.schemaFor(t.Elem(), mode)}
	case reflect.Map:
		return map[string]any{schemaTypeKey: "object", "additionalProperties": b.schemaFor(t.Elem(), mode)}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Struct:
		return b.structOrRef(t, mode)
	default:
		return map[string]any{}
	}
}

// structOrRef emits a struct schema inline, or interns a named struct type into
// the mode's components and returns a $ref to it.
func (b *schemaBuilder) structOrRef(t reflect.Type, mode schemaMode) map[string]any {
	if t == timeType {
		return map[string]any{schemaTypeKey: "string", "format": "date-time"}
	}
	name := t.Name()
	if name == "" || !isExported(name) || strings.ContainsAny(name, "[]") {
		return b.structSchema(t, mode)
	}
	interned := b.response
	if mode == modeRequest {
		interned = b.request
	}
	if _, seen := interned[name]; !seen {
		interned[name] = map[string]any{} // placeholder breaks recursion
		interned[name] = b.structSchema(t, mode)
	}
	ref := map[string]any{"$ref": componentRefPrefix + name}
	if mode == modeRequest {
		b.requestRefs = append(b.requestRefs, refNode{name: name, node: ref})
	}
	return ref
}

// structSchema builds the object schema for a struct's JSON-encoded fields.
func (b *schemaBuilder) structSchema(t reflect.Type, mode schemaMode) map[string]any {
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
		alwaysEncoded := mode == modeResponse && !omitsEmpty(f)
		properties[name] = b.fieldSchema(f, alwaysEncoded, mode)
		if alwaysEncoded || hasRule(f.Tag.Get("validate"), "required") {
			required = append(required, name)
		}
	}
	schema := map[string]any{schemaTypeKey: "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// fieldSchema returns one struct field's schema: the openapi tag's override, or
// the field type's schema with its validate constraints applied. A pointer the
// encoder always writes (alwaysEncoded) is made nullable, since nil encodes as
// null.
func (b *schemaBuilder) fieldSchema(f reflect.StructField, alwaysEncoded bool, mode schemaMode) map[string]any {
	schema, overridden := openapiOverride(f.Tag.Get("openapi"))
	if !overridden {
		schema = b.schemaFor(f.Type, mode)
		applyConstraints(schema, f.Tag.Get("validate"))
	}
	if alwaysEncoded && f.Type.Kind() == reflect.Pointer {
		schema = b.nullable(schema)
	}
	return schema
}

// nullable widens a schema to also admit null: OpenAPI 3.1 adds a "null" type
// (or, for a $ref, a oneOf with the null type); the 3.0 dialect sets "nullable":
// true (wrapping a $ref in allOf). An enum gains null too. A schema without a
// type — the empty schema — already admits null and is returned unchanged.
func (b *schemaBuilder) nullable(schema map[string]any) map[string]any {
	if _, isRef := schema["$ref"]; isRef {
		if b.nullableKeyword {
			return map[string]any{"allOf": []any{schema}, "nullable": true}
		}
		return map[string]any{"oneOf": []any{schema, map[string]any{schemaTypeKey: "null"}}}
	}
	typ, typed := schema[schemaTypeKey]
	if !typed {
		return schema
	}
	if enum, ok := schema["enum"].([]string); ok {
		widened := make([]any, 0, len(enum)+1)
		for _, v := range enum {
			widened = append(widened, v)
		}
		schema["enum"] = append(widened, nil)
	}
	if b.nullableKeyword {
		schema["nullable"] = true
	} else {
		schema[schemaTypeKey] = []any{typ, "null"}
	}
	return schema
}

// responseSchema returns the JSON Schema of an action's response body. An Empty
// response has no body; a Created/Accepted wrapper unwraps to its Body type.
func (b *schemaBuilder) responseSchema(respType reflect.Type) (map[string]any, bool) {
	if respType.Implements(envelopeType) || reflect.PointerTo(respType).Implements(envelopeType) {
		if field, ok := respType.FieldByName("Body"); ok {
			return b.schemaFor(field.Type, modeResponse), true
		}
		return nil, false // Empty
	}
	return b.schemaFor(respType, modeResponse), true
}

// components returns the document's component schemas, merging the two modes.
// A name interned in both modes whose schemas differ — directly, or through a
// component its request schema references — keeps the response schema as <Name>
// and emits the request schema as <Name>Input, and every request-mode $ref to it
// is retargeted. Otherwise one schema serves both modes. It panics when
// <Name>Input already names another component. Call it once, after every schema
// has been built.
func (b *schemaBuilder) components() map[string]any {
	differs := b.modesDiffer()
	out := make(map[string]any, len(b.request)+len(b.response))
	for name, schema := range b.response {
		out[name] = schema
	}
	for name, schema := range b.request {
		if !differs[name] {
			if _, both := b.response[name]; !both {
				out[name] = schema
			}
			continue
		}
		input := name + inputSuffix
		_, takenReq := b.request[input]
		_, takenResp := b.response[input]
		if takenReq || takenResp {
			panic(fmt.Sprintf("actions: %s differs between request and response, but %s already names another component", name, input))
		}
		out[input] = schema
	}
	for _, ref := range b.requestRefs {
		if differs[ref.name] {
			ref.node["$ref"] = componentRefPrefix + ref.name + inputSuffix
		}
	}
	return out
}

// modesDiffer reports, for each component interned in both modes, whether its
// request and response schemas differ. A schema differs when its own content
// differs or when its request schema references a component that differs. The
// reference rule is iterated to a fixpoint, so a cycle of otherwise-equal
// schemas stays equal.
func (b *schemaBuilder) modesDiffer() map[string]bool {
	differs := map[string]bool{}
	refs := map[string][]string{}
	for name, req := range b.request {
		resp, both := b.response[name]
		if !both {
			continue
		}
		differs[name] = !reflect.DeepEqual(req, resp)
		refs[name] = refNames(req)
	}
	for changed := true; changed; {
		changed = false
		for name, targets := range refs {
			if differs[name] {
				continue
			}
			for _, target := range targets {
				if differs[target] {
					differs[name] = true
					changed = true
					break
				}
			}
		}
	}
	return differs
}

// refNames returns the component names a schema fragment references by $ref.
func refNames(v any) []string {
	var names []string
	switch node := v.(type) {
	case map[string]any:
		if ref, ok := node["$ref"].(string); ok {
			names = append(names, strings.TrimPrefix(ref, componentRefPrefix))
		}
		for _, child := range node {
			names = append(names, refNames(child)...)
		}
	case []any:
		for _, child := range node {
			names = append(names, refNames(child)...)
		}
	}
	return names
}

// openapiOverride returns the schema an openapi struct tag substitutes for a
// field's generated one. The one override, type=object, documents an opaque JSON
// object — for example a json.RawMessage carrying a protocol payload — as
// {"type": "object", "additionalProperties": true}. Other rules are ignored.
func openapiOverride(tag string) (map[string]any, bool) {
	for _, rule := range strings.Split(tag, ",") {
		if strings.TrimSpace(rule) == "type=object" {
			return map[string]any{schemaTypeKey: "object", "additionalProperties": true}, true
		}
	}
	return nil, false
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

// jsonFirstSegment returns the name portion (the first comma-separated segment)
// of a json struct-tag value, e.g. "name" from "name,omitempty".
func jsonFirstSegment(tag string) string {
	name, _, _ := strings.Cut(tag, ",")
	return name
}

// jsonName returns a struct field's JSON name.
func jsonName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	return jsonFirstSegment(tag)
}

// omitsEmpty reports whether encoding/json may leave the field out: its json
// tag carries omitempty or omitzero.
func omitsEmpty(f reflect.StructField) bool {
	_, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
	for _, opt := range strings.Split(opts, ",") {
		if opt == "omitempty" || opt == "omitzero" {
			return true
		}
	}
	return false
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
