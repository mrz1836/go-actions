package actions

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// enumMethod is the method that makes a named type an enum:
// func (T) EnumValues() []T.
const enumMethod = "EnumValues"

// enumSpec is an enum type's checked values, read once per type.
type enumSpec struct {
	// values are the raw values, in EnumValues' order: a string, an int64, or
	// a uint64, as the schema's enum keyword lists them.
	values []any
	// check is the rule every value of the type carries: the value must be one
	// of values, compared as the type's kind.
	check rule
}

// enumResult is one type's cached reading: its spec, or the declaration
// error of its EnumValues method, or neither when it is no enum.
type enumResult struct {
	spec *enumSpec
	err  error
}

// enumCache holds one enumResult per type, so EnumValues is called once.
//
//nolint:gochecknoglobals // an immutable per-type reading cache
var enumCache sync.Map

// enumOf returns the spec of enum type t (pointers followed), nil when t is not
// an enum type, or the declaration error of an EnumValues method that breaks
// the rules: a value receiver and the signature func() []T, a string or
// integer kind, no MarshalJSON or MarshalText of its own, at least one value,
// and no value twice. Only a struct type can have an EnumValues it didn't
// declare, promoted from an embedded field, so a struct type whose method
// isn't func() []T on the value is read as no enum rather than refused.
func enumOf(t reflect.Type) (*enumSpec, error) {
	t = derefType(t)
	if cached, ok := enumCache.Load(t); ok {
		r := cached.(enumResult)
		return r.spec, r.err
	}
	spec, err := readEnum(t)
	enumCache.Store(t, enumResult{spec: spec, err: err})
	return spec, err
}

// isEnumType reports whether t (pointers followed) is an enum type whose
// EnumValues method keeps the rules.
func isEnumType(t reflect.Type) bool {
	spec, err := enumOf(t)
	return spec != nil && err == nil
}

// enumRules returns the rules a value of type t carries by its type: an enum
// type's value check, or nothing.
func enumRules(t reflect.Type) []rule {
	if spec, err := enumOf(t); spec != nil && err == nil {
		return []rule{spec.check}
	}
	return nil
}

// readEnum reads t's EnumValues method; see enumOf.
func readEnum(t reflect.Type) (*enumSpec, error) {
	if t.Kind() == reflect.Interface {
		return nil, nil //nolint:nilnil // an interface type is never an enum
	}
	method, onValue := t.MethodByName(enumMethod)
	if !onValue {
		if _, onPointer := reflect.PointerTo(t).MethodByName(enumMethod); !onPointer {
			return nil, nil //nolint:nilnil // no EnumValues method: no enum
		}
	}
	wellFormed := onValue && method.Type.NumIn() == 1 && !method.Type.IsVariadic() &&
		method.Type.NumOut() == 1 && method.Type.Out(0) == reflect.SliceOf(t)
	switch {
	case t.Kind() == reflect.Struct && !wellFormed:
		return nil, nil //nolint:nilnil // a method the struct type didn't declare
	case !wellFormed:
		return nil, enumError(t, fmt.Sprintf("must be func() []%s on the value", t.Name()))
	case !enumKind(t):
		return nil, enumError(t, "is allowed only on a string or integer type")
	case encodesItself(t):
		return nil, enumError(t, "is not allowed on a type that marshals itself")
	}
	return enumValues(t, reflect.Zero(t).Method(method.Index).Call(nil)[0])
}

// enumKind reports whether an enum may have type t's kind: a string, or an
// integer other than uintptr.
func enumKind(t reflect.Type) bool {
	switch t.Kind() { //nolint:exhaustive // every other kind is refused
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

// enumValues checks the values EnumValues returned for type t and builds the
// type's spec. Values and labels are the raw values, never a String method's
// text, so a message lists what a client sends.
func enumValues(t reflect.Type, list reflect.Value) (*enumSpec, error) {
	if list.Len() == 0 {
		return nil, enumError(t, "returns no values")
	}
	shape := shapeOf(t)
	spec := &enumSpec{values: make([]any, list.Len())}
	labels := make([]string, list.Len())
	seen := make(map[any]bool, list.Len())
	for i := range list.Len() {
		v := list.Index(i)
		switch shape { //nolint:exhaustive // enumKind admits only these shapes
		case shapeString:
			spec.values[i], labels[i] = v.String(), v.String()
		case shapeInt:
			spec.values[i], labels[i] = v.Int(), strconv.FormatInt(v.Int(), 10)
		default:
			spec.values[i], labels[i] = v.Uint(), strconv.FormatUint(v.Uint(), 10)
		}
		if seen[spec.values[i]] {
			return nil, enumError(t, "returns "+labels[i]+" twice")
		}
		seen[spec.values[i]] = true
	}
	spec.check = rule{
		kind: ruleEnum, shape: shape, allowed: labels, enum: spec.values,
		msg: "must be one of: " + strings.Join(labels, ", "),
	}
	return spec, nil
}

// enumError is the declaration error of type t's EnumValues method.
func enumError(t reflect.Type, reason string) error {
	return &declError{subject: "type " + qualifiedName(t), detail: enumMethod + " " + reason}
}
