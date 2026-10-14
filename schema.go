package actions

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// schemaTypeKey is the JSON Schema "type" keyword.
const schemaTypeKey = "type"

// typeString is the JSON Schema "string" type.
const typeString = "string"

// componentRefPrefix is the JSON pointer prefix of a named component schema.
const componentRefPrefix = "#/components/schemas/"

// defsRefPrefix is the JSON pointer prefix of a schema in a standalone
// document's $defs.
const defsRefPrefix = "#/$defs/"

// inputSuffix names the request variant of a component whose request and
// response schemas differ.
const inputSuffix = "Input"

// errorComponent names the framework's error-envelope component schema.
const errorComponent = "Error"

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
var timeType = reflect.TypeFor[time.Time]()

// uuidType is the reflect.Type of uuid.UUID, schema-mapped to a uuid string.
//
//nolint:gochecknoglobals // a cached reflect.Type is an intentional package global
var uuidType = reflect.TypeFor[uuid.UUID]()

// jsonMarshalerType is the reflect.Type of json.Marshaler.
//
//nolint:gochecknoglobals // a cached reflect.Type is an intentional package global
var jsonMarshalerType = reflect.TypeFor[json.Marshaler]()

// textMarshalerType is the reflect.Type of encoding.TextMarshaler.
//
//nolint:gochecknoglobals // a cached reflect.Type is an intentional package global
var textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()

// envelopeType is the reflect.Type of the responseEnvelope interface.
//
//nolint:gochecknoglobals // a cached reflect.Type is an intentional package global
var envelopeType = reflect.TypeOf((*responseEnvelope)(nil)).Elem()

// schemaBuilder generates JSON Schema 2020-12 fragments. Named struct types are
// interned into components and referenced by $ref so the document stays small
// and the names are human-meaningful. Each mode interns separately; components
// merges the two.
type schemaBuilder struct {
	// openAPI30 selects the OpenAPI 3.0 dialect: nullability is marked with
	// "nullable": true instead of a "null" type, and a []byte is a string with
	// "format": "byte" instead of "contentEncoding": "base64".
	openAPI30 bool

	request  map[string]map[string]any
	response map[string]map[string]any
	// names maps each interned component name to the Go type that claimed it,
	// so two distinct types sharing a name are caught instead of merged.
	names map[string]reflect.Type
	// requestRefs are the $ref nodes emitted in request mode, which components
	// retargets to <Name>Input when that name's schemas differ.
	requestRefs []refNode

	// standalone selects SchemaFor's self-contained document: every named
	// type is inlined, except the types in cyclic, whose schemas go once in
	// defs and are referenced as #/$defs/<Name>. No component is interned.
	standalone bool
	cyclic     map[reflect.Type]bool
	defs       map[string]map[string]any
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
		names:    map[string]reflect.Type{},
	}
}

// schemaFor returns the JSON Schema for t in the given mode, following
// encoding/json's precedence: a time.Time is a date-time string; a type with its
// own MarshalJSON (json.RawMessage included) is the empty schema, since its
// encoding is unknown; a type with its own MarshalText is a string (uuid.UUID a
// uuid string); a []byte is a base64 string; an enum type is its values (see
// enumSchema); everything else maps by kind.
//
//nolint:gocyclo // one switch over the Go kind taxonomy
func (b *schemaBuilder) schemaFor(t reflect.Type, mode schemaMode) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == timeType:
		return map[string]any{schemaTypeKey: typeString, "format": "date-time"}
	case implements(t, jsonMarshalerType):
		return map[string]any{}
	case implements(t, textMarshalerType):
		if t == uuidType {
			return map[string]any{schemaTypeKey: typeString, "format": "uuid"}
		}
		return map[string]any{schemaTypeKey: typeString}
	case isByteSlice(t):
		if b.openAPI30 {
			return map[string]any{schemaTypeKey: typeString, "format": "byte"}
		}
		return map[string]any{schemaTypeKey: typeString, "contentEncoding": "base64"}
	}
	if spec, _ := enumOf(t); spec != nil {
		return b.enumSchema(t, spec, mode)
	}
	switch t.Kind() { //nolint:exhaustive // unsupported kinds fall through to the default
	case reflect.String:
		return map[string]any{schemaTypeKey: typeString}
	case reflect.Bool:
		return map[string]any{schemaTypeKey: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return map[string]any{schemaTypeKey: "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{schemaTypeKey: "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{schemaTypeKey: "array", "items": b.schemaFor(t.Elem(), mode)}
	case reflect.Map:
		return map[string]any{schemaTypeKey: "object", "additionalProperties": b.schemaFor(t.Elem(), mode)}
	case reflect.Struct:
		return b.structOrRef(t, mode)
	default:
		return map[string]any{}
	}
}

// implements reports whether t, or a pointer to t, implements iface.
func implements(t, iface reflect.Type) bool {
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}

// encodesItself reports whether encoding/json encodes t through its own method
// (MarshalJSON or MarshalText, time.Time included) rather than field by field.
// The schema generator and the validator both treat such a type as opaque.
func encodesItself(t reflect.Type) bool {
	return implements(t, jsonMarshalerType) || implements(t, textMarshalerType)
}

// isByteSlice reports whether encoding/json encodes t as a base64 string: a
// slice of a byte kind whose element type has no marshaling method of its own.
func isByteSlice(t reflect.Type) bool {
	return t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 && !encodesItself(t.Elem())
}

// structOrRef emits a struct schema inline, or interns a named struct type into
// the mode's components and returns a $ref to it. An unnamed, unexported, or
// generic struct type is inlined (see inlinesStruct); Freeze refuses one that
// refers to itself, whose inlined schema would never end. A standalone builder
// inlines every struct type but those on a cycle (see defsRef).
func (b *schemaBuilder) structOrRef(t reflect.Type, mode schemaMode) map[string]any {
	if b.standalone && b.cyclic[t] {
		return b.defsRef(t, mode)
	}
	if b.standalone || inlinesStruct(t) {
		return b.structSchema(t, mode)
	}
	name := t.Name()
	b.claimName(name, t)
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

// defsRef puts struct type t's schema in the standalone document's $defs, once,
// and returns a $ref to it. SchemaFor has checked that no two such types share
// a name.
func (b *schemaBuilder) defsRef(t reflect.Type, mode schemaMode) map[string]any {
	name := t.Name()
	if _, seen := b.defs[name]; !seen {
		b.defs[name] = map[string]any{} // placeholder breaks recursion
		b.defs[name] = b.structSchema(t, mode)
	}
	return map[string]any{"$ref": defsRefPrefix + name}
}

// enumSchema returns enum type t's schema, {"type": "string" | "integer",
// "enum": [...]} with the values in EnumValues' order. Like a struct, a named,
// exported, non-generic enum type is interned into the mode's components and
// referenced by $ref, and any other is inlined (see inlinesStruct); a
// standalone builder inlines every one. Its schema is the same in both modes,
// so a request and a response share one component.
func (b *schemaBuilder) enumSchema(t reflect.Type, spec *enumSpec, mode schemaMode) map[string]any {
	schema := map[string]any{schemaTypeKey: shapeType(shapeOf(t)), "enum": slices.Clone(spec.values)}
	if b.standalone || inlinesStruct(t) {
		return schema
	}
	name := t.Name()
	b.claimName(name, t)
	interned := b.response
	if mode == modeRequest {
		interned = b.request
	}
	if _, seen := interned[name]; !seen {
		interned[name] = schema
	}
	return map[string]any{"$ref": componentRefPrefix + name}
}

// inlinesStruct reports whether the schema generator writes struct type t's
// schema inline instead of as a component: t is unnamed, unexported, or
// generic. Only a component can be referred back to, by $ref.
func inlinesStruct(t reflect.Type) bool {
	name := t.Name()
	return name == "" || !isExported(name) || strings.ContainsAny(name, "[]")
}

// claimName records that type t owns the component name, panicking when a
// different type already owns it or when it is the framework's Error name.
func (b *schemaBuilder) claimName(name string, t reflect.Type) {
	if name == errorComponent {
		panic(fmt.Sprintf("actions: type %s would replace the framework's %s component schema; rename it",
			qualifiedName(t), errorComponent))
	}
	if prev, ok := b.names[name]; ok && prev != t {
		panic(fmt.Sprintf("actions: types %s and %s both map to the component schema %q; rename one",
			qualifiedName(prev), qualifiedName(t), name))
	}
	b.names[name] = t
}

// qualifiedName returns a type's import-path-qualified name.
func qualifiedName(t reflect.Type) string {
	return t.PkgPath() + "." + t.Name()
}

// structSchema builds the object schema for a struct's JSON-encoded fields,
// including those promoted from embedded structs (see jsonFields).
func (b *schemaBuilder) structSchema(t reflect.Type, mode schemaMode) map[string]any {
	properties := map[string]any{}
	var required []string
	for _, jf := range jsonFields(t) {
		f := jf.field
		// Freeze has already refused a tag with a rule parseRules refuses.
		rules, _ := parseRules(f.Tag.Get("validate"), f.Type)
		// In a response, a field without omitempty/omitzero is always written
		// — unless it is promoted through an embedded pointer, which omits it
		// while nil.
		written := mode == modeResponse && !omitsEmpty(f)
		properties[jf.name] = b.fieldSchema(f, rules, written, mode)
		if (written && !jf.viaPointer) || hasRequiredRule(rules.field) {
			required = append(required, jf.name)
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
// encoder writes whenever the field is present (written) is made nullable,
// since nil encodes as null. The field's doc and example tags come last (see
// annotate).
func (b *schemaBuilder) fieldSchema(f reflect.StructField, rules ruleSet, written bool, mode schemaMode) map[string]any {
	schema, overridden := openapiOverride(f.Tag.Get("openapi"))
	if !overridden {
		schema = b.schemaFor(f.Type, mode)
		applyRuleSet(schema, rules)
	}
	if written && f.Type.Kind() == reflect.Pointer {
		schema = b.nullable(schema)
	}
	return b.annotate(schema, f)
}

// annotate adds field f's doc tag to its schema as the description, and its
// example tag (see exampleValue) as "examples": [v], or, in the OpenAPI 3.0
// dialect, "example": v. An empty tag writes nothing. A bare $ref takes the
// keywords beside it, except in 3.0, which ignores a $ref's siblings, so there
// it is wrapped once in allOf; a nullable wrapper takes them on itself.
func (b *schemaBuilder) annotate(schema map[string]any, f reflect.StructField) map[string]any {
	doc := f.Tag.Get("doc")
	example, hasExample := exampleValue(f.Type, f.Tag.Get("example"))
	if doc == "" && !hasExample {
		return schema
	}
	if _, isRef := schema["$ref"]; isRef && b.openAPI30 {
		schema = map[string]any{"allOf": []any{schema}}
	}
	if doc != "" {
		schema["description"] = doc
	}
	if hasExample && b.openAPI30 {
		schema["example"] = example
	} else if hasExample {
		schema["examples"] = []any{example}
	}
	return schema
}

// exampleValue reads an example tag for a value of type t, the same way for
// the schema and for the check Freeze runs on it (see checkExample). When t
// (pointers followed) encodes as a JSON string — a string kind, enums
// included, a time.Time, a type that marshals itself as text, or a []byte —
// the tag is that string, as written; otherwise it is JSON, kept compacted as
// written, so a large number keeps its precision. It reports false for an
// empty tag, and for one that should be JSON but isn't.
func exampleValue(t reflect.Type, tag string) (any, bool) {
	if tag == "" {
		return nil, false
	}
	if encodesAsString(derefType(t)) {
		return tag, true
	}
	var compact bytes.Buffer
	if json.Compact(&compact, []byte(tag)) != nil {
		return nil, false
	}
	return json.RawMessage(compact.Bytes()), true
}

// encodesAsString reports whether encoding/json writes a value of type t as a
// JSON string, in schemaFor's precedence: a time.Time does; a type with its own
// MarshalJSON writes what it likes, so it is read as JSON; a type with its own
// MarshalText, a []byte, and a string kind do.
func encodesAsString(t reflect.Type) bool {
	switch {
	case t == timeType:
		return true
	case implements(t, jsonMarshalerType):
		return false
	case implements(t, textMarshalerType), isByteSlice(t):
		return true
	default:
		return t.Kind() == reflect.String
	}
}

// nullable widens a schema to also admit null: OpenAPI 3.1 adds a "null" type
// (or, for a $ref, a oneOf with the null type); the 3.0 dialect sets "nullable":
// true (wrapping a $ref in allOf). An enum gains null too. A schema without a
// type — the empty schema — already admits null and is returned unchanged.
func (b *schemaBuilder) nullable(schema map[string]any) map[string]any {
	if _, isRef := schema["$ref"]; isRef {
		if b.openAPI30 {
			return map[string]any{"allOf": []any{schema}, "nullable": true}
		}
		return map[string]any{"oneOf": []any{schema, map[string]any{schemaTypeKey: "null"}}}
	}
	typ, typed := schema[schemaTypeKey]
	if !typed {
		return schema
	}
	if enum, ok := schema["enum"].([]any); ok {
		schema["enum"] = append(enum, nil)
	}
	if b.openAPI30 {
		schema["nullable"] = true
	} else {
		schema[schemaTypeKey] = []any{typ, "null"}
	}
	return schema
}

// responseSchema returns the JSON Schema of an action's response body. An Empty
// response has no body; a Created/Accepted wrapper unwraps to its Body type.
func (b *schemaBuilder) responseSchema(respType reflect.Type) (map[string]any, bool) {
	body, ok := responseBodyType(respType)
	if !ok {
		return nil, false
	}
	return b.schemaFor(body, modeResponse), true
}

// responseBodyType returns the type of an action's response body: Created,
// Accepted, and Response unwrap to their Body type, and Empty has no body.
func responseBodyType(respType reflect.Type) (reflect.Type, bool) {
	for respType.Kind() == reflect.Pointer {
		respType = respType.Elem()
	}
	if implements(respType, envelopeType) {
		field, ok := respType.FieldByName("Body")
		return field.Type, ok
	}
	return respType, true
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

// applyRuleSet folds a field's parsed validate rules into its schema: the
// field's own rules into the schema itself, and the element rules after dive
// into its items. required after dive has no keyword: it is only enforced.
func applyRuleSet(schema map[string]any, rules ruleSet) {
	applyConstraints(schema, rules.field)
	if items, ok := schema["items"].(map[string]any); ok {
		applyConstraints(items, rules.elem)
	}
}

// applyConstraints folds a field's parsed validate rules into its schema. A
// rule becomes a keyword only when the schema's type matches the Go shape the
// rule was parsed for — e.g. min on a []byte (a base64 string in the schema)
// has no keyword — so every documented constraint is one the runtime enforces.
func applyConstraints(schema map[string]any, rules []rule) {
	typ, _ := schema[schemaTypeKey].(string)
	for _, r := range rules {
		switch r.kind {
		case ruleMin, ruleMax:
			if key := boundKeyword(r, typ); key != "" {
				schema[key] = r.limit
			}
		case ruleOneOf:
			if shapeType(r.shape) == typ {
				schema["enum"] = slices.Clone(r.enum)
			}
		case ruleUUID, ruleEmail, ruleRFC3339, ruleE164, ruleDate:
			if typ == typeString {
				applyFormat(schema, r.kind)
			}
		case ruleRequired:
			// Documented through the object's required list.
		case ruleEnum:
			// Documented by the enum type's own schema.
		}
	}
}

// boundKeyword returns the JSON Schema keyword a min/max rule becomes on a
// schema of type typ, or "" when the rule has none there.
func boundKeyword(r rule, typ string) string {
	if shapeType(r.shape) != typ {
		return ""
	}
	isMin := r.kind == ruleMin
	switch r.shape { //nolint:exhaustive // parseBound admits only these shapes
	case shapeString:
		return pick(isMin, "minLength", "maxLength")
	case shapeItems:
		return pick(isMin, "minItems", "maxItems")
	case shapeProps:
		return pick(isMin, "minProperties", "maxProperties")
	default:
		return pick(isMin, "minimum", "maximum")
	}
}

// pick returns a when cond holds, otherwise b.
func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// shapeTypes maps each value shape to its JSON Schema type; shapeOther has
// none.
//
//nolint:gochecknoglobals // a fixed lookup table is an intentional package global
var shapeTypes = [...]string{
	shapeOther:  "",
	shapeString: typeString,
	shapeInt:    "integer",
	shapeUint:   "integer",
	shapeFloat:  "number",
	shapeItems:  "array",
	shapeProps:  "object",
}

// shapeType returns the JSON Schema type of a value shape, or "" for
// shapeOther.
func shapeType(shape valueShape) string {
	return shapeTypes[shape]
}

// applyFormat adds a string-format rule's keyword.
func applyFormat(schema map[string]any, kind ruleKind) {
	switch kind { //nolint:exhaustive // only the string-format rules reach here
	case ruleUUID:
		schema["format"] = "uuid"
	case ruleEmail:
		schema["format"] = "email"
	case ruleRFC3339:
		schema["format"] = "date-time"
	case ruleDate:
		schema["format"] = "date"
	default:
		schema["pattern"] = e164Pattern
	}
}

// jsonFirstSegment returns the name portion (the first comma-separated segment)
// of a json struct-tag value, e.g. "name" from "name,omitempty".
func jsonFirstSegment(tag string) string {
	name, _, _ := strings.Cut(tag, ",")
	return name
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

// hasRequiredRule reports whether parsed rules include required.
func hasRequiredRule(rules []rule) bool {
	return slices.ContainsFunc(rules, func(r rule) bool { return r.kind == ruleRequired })
}

// isExported reports whether name begins with an uppercase letter.
func isExported(name string) bool {
	return name != "" && unicode.IsUpper(rune(name[0]))
}
