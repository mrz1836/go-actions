package actions

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"sync"
)

// jsonSchemaDialect is the $schema of the documents SchemaFor returns.
const jsonSchemaDialect = "https://json-schema.org/draft/2020-12/schema"

// SchemaFor returns a self-contained JSON Schema 2020-12 document for T read
// as a request body: "$schema" is https://json-schema.org/draft/2020-12/schema,
// and the rest is the schema a registry publishes for a request body of type
// T, built from the same struct tags, with each component written in place.
// It carries "required" from validate:"required" (pointers are not nullable),
// the validate constraints, enums, the rules after dive as items keywords,
// and the doc and example tags, in the OpenAPI 3.1 forms. A Validatable hook
// is not described.
//
// Named types are inlined, so a reader needs no reference resolution, except
// an exported, non-generic struct type whose schema would hold itself: its
// schema is written once under "$defs", and every use of it, T included, is
// {"$ref": "#/$defs/<Name>"}. Unexported and generic types on such a cycle
// are inlined inside it.
//
// T is checked as Freeze checks a request type, and a declaration problem is
// returned as the error, its text naming "type <pkg.Type>" where Freeze names
// the action: a validate rule that is unknown, misapplied, or malformed, an
// example its field refuses, a bad EnumValues method, a parameter that cannot
// bind, an unnamed, unexported, or generic type that refers to itself, or two
// types in $defs with one name. SchemaFor never panics on T's declaration.
func SchemaFor[T any]() (json.RawMessage, error) {
	t := derefType(reflect.TypeFor[T]())
	subject := typeSubject(t)
	if err := checkRequest(subject, t); err != nil {
		return nil, err
	}
	cyclic, err := cyclicTypes(subject, t)
	if err != nil {
		return nil, err
	}
	b := newSchemaBuilder()
	b.standalone = true
	b.cyclic = cyclic
	b.defs = map[string]map[string]any{}
	doc := map[string]any{"$schema": jsonSchemaDialect}
	maps.Copy(doc, b.schemaFor(t, modeRequest))
	if len(b.defs) > 0 {
		doc["$defs"] = b.defs
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("actions: marshal the schema of %s: %w", subject, err)
	}
	return raw, nil
}

// Validate checks v as a registered action checks a decoded request: its
// validate tags at every depth, its enums, and then its Validatable hook. v is
// a struct or a pointer to one; anything else, a nil pointer included, is
// valid. Pass a pointer when the hook has a pointer receiver.
//
// It returns nil when v is valid, or an *APIError (Status 422, Code
// VALIDATION_ERROR) whose Fields are named exactly as a request with the same
// body names them. When v's type declares something Freeze refuses (see
// SchemaFor), it returns that declaration error instead, which is not an
// *APIError; a type is checked once, on first use.
func Validate(v any) error {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	if err := declarationOf(rv.Type()); err != nil {
		return err
	}
	if apiErr := validateRequest(v); apiErr != nil {
		return apiErr
	}
	return nil
}

// declResult is one type's cached declaration check.
type declResult struct{ err error }

// declCache holds Validate's declaration check per type, so a type is checked
// once.
//
//nolint:gochecknoglobals // an immutable per-type check cache
var declCache sync.Map

// declarationOf returns the declaration problem of struct type t read as a
// request (see checkRequest), or nil, checking each type once.
func declarationOf(t reflect.Type) error {
	if cached, ok := declCache.Load(t); ok {
		return cached.(declResult).err
	}
	r := declResult{err: checkRequest(typeSubject(t), t)}
	declCache.Store(t, r)
	return r.err
}

// typeSubject names type t in a declaration error: "type <pkg.Type>", or the
// type as Go writes it when it has no name.
func typeSubject(t reflect.Type) string {
	if t.Name() == "" {
		return "type " + t.String()
	}
	return "type " + qualifiedName(t)
}

// cyclicTypes returns the struct types reachable from root whose schemas would
// hold themselves and can be named (see inlinesStruct): the types SchemaFor
// writes once under $defs. Two of them with one name are a declaration error,
// named by subject.
func cyclicTypes(subject string, root reflect.Type) (map[reflect.Type]bool, error) {
	cyclic := map[reflect.Type]bool{}
	byName := map[string]reflect.Type{}
	for _, t := range reachableTypes(root) {
		if t.Kind() != reflect.Struct || inlinesStruct(t) || !holdsItself(t) {
			continue
		}
		if prev, taken := byName[t.Name()]; taken {
			return nil, &declError{subject: subject, detail: fmt.Sprintf(
				"types %s and %s both map to the $defs name %q", qualifiedName(prev), qualifiedName(t), t.Name(),
			)}
		}
		byName[t.Name()] = t
		cyclic[t] = true
	}
	return cyclic, nil
}

// reachableTypes returns root and every type whose schema root's schema holds,
// at any depth (see schemaChildren), pointers followed, each once, in the
// order a depth-first walk meets them.
func reachableTypes(root reflect.Type) []reflect.Type {
	var order []reflect.Type
	seen := map[reflect.Type]bool{}
	stack := []reflect.Type{root}
	for len(stack) > 0 {
		t := derefType(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		if seen[t] {
			continue
		}
		seen[t] = true
		order = append(order, t)
		children := schemaChildren(t)
		for i := len(children) - 1; i >= 0; i-- {
			stack = append(stack, children[i])
		}
	}
	return order
}

// holdsItself reports whether struct type t's schema holds t's own schema, at
// any depth.
func holdsItself(t reflect.Type) bool {
	seen := map[reflect.Type]bool{}
	stack := schemaChildren(t)
	for len(stack) > 0 {
		next := derefType(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		if next == t {
			return true
		}
		if !seen[next] {
			seen[next] = true
			stack = append(stack, schemaChildren(next)...)
		}
	}
	return false
}

// schemaChildren returns the types whose schemas the schema of t (pointers
// followed) holds directly, as schemaFor builds it: a slice's or an array's
// element type, a map's value type, and a struct's JSON field types, except
// a field whose openapi tag overrides its schema. A type schemaFor describes
// on its own (a time.Time, a type that encodes itself, a []byte, or an enum)
// holds none.
func schemaChildren(t reflect.Type) []reflect.Type {
	t = derefType(t)
	if encodesItself(t) || isByteSlice(t) || isEnumType(t) {
		return nil
	}
	switch t.Kind() { //nolint:exhaustive // other kinds hold no schemas
	case reflect.Slice, reflect.Array, reflect.Map:
		return []reflect.Type{t.Elem()}
	case reflect.Struct:
		var children []reflect.Type
		for _, jf := range jsonFields(t) {
			if _, overridden := openapiOverride(jf.field.Tag.Get("openapi")); !overridden {
				children = append(children, jf.field.Type)
			}
		}
		return children
	default:
		return nil
	}
}
