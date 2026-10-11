package actions

import (
	"fmt"
	"reflect"
)

// declError is one declaration problem, which Freeze panics with.
type declError struct {
	subject string // `action "<id>"`
	detail  string // `parameter "<name>": …`
}

// Error renders the problem as Freeze reports it.
func (e *declError) Error() string { return "actions: " + e.subject + ": " + e.detail }

// checkAction returns the first problem in an action's declaration, or nil: a
// parameter the decoder cannot bind.
func checkAction(id string, reqType reflect.Type) error {
	c := &declChecker{subject: fmt.Sprintf("action %q", id)}
	if st := structType(reqType); st != nil {
		return c.params(st)
	}
	return nil
}

// declChecker walks one action's types for checkAction.
type declChecker struct {
	subject string
}

// fail returns the declaration error for detail.
func (c *declChecker) fail(detail string) error {
	return &declError{subject: c.subject, detail: detail}
}

// params checks that each parameter field of request struct t can bind from
// its location.
func (c *declChecker) params(t reflect.Type) error {
	for _, p := range paramFields(t) {
		if !bindable(p.field.Type, p.in) {
			return c.fail(fmt.Sprintf("parameter %q: %s cannot be a %s parameter", p.name, p.field.Type, p.in))
		}
	}
	return nil
}

// bindable reports whether a parameter of type t binds from location in: with
// one pointer followed, t is a scalar setScalar converts (see bindsScalar);
// for a query parameter only, and with no pointer, t may also be a slice of
// such a scalar, except one whose element is a uint8 kind, which encodes as a
// base64 string.
func bindable(t reflect.Type, in string) bool {
	if t.Kind() == reflect.Pointer {
		return bindsScalar(t.Elem())
	}
	if bindsScalar(t) {
		return true
	}
	return in == "query" && t.Kind() == reflect.Slice &&
		t.Elem().Kind() != reflect.Uint8 && bindsScalar(t.Elem())
}

// bindsScalar reports whether setScalar converts one value into type t:
// time.Time, a type whose pointer implements encoding.TextUnmarshaler, or a
// string, bool, integer, or float kind.
func bindsScalar(t reflect.Type) bool {
	if t == timeType || reflect.PointerTo(t).Implements(textUnmarshalerType) {
		return true
	}
	switch t.Kind() { //nolint:exhaustive // every other kind cannot bind
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}
