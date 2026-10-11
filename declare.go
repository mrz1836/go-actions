package actions

import (
	"fmt"
	"reflect"
)

// declError is one declaration problem, which Freeze panics with.
type declError struct {
	subject string // `action "<id>"`
	detail  string // `field "<path>": …` or `parameter "<name>": …`
}

// Error renders the problem as Freeze reports it.
func (e *declError) Error() string { return "actions: " + e.subject + ": " + e.detail }

// checkAction returns the first problem in an action's declaration, or nil:
// first its parameters, then its request body fields, then its response body
// fields, each depth first in field order. It reads every field the decoder
// binds, the validator checks, or the schema generator documents, and so
// refuses a parameter the decoder cannot bind, a validate rule that would
// otherwise be dropped (see parseRules), and a type that refers to itself but
// whose schema would be inlined into itself (see structFields).
func checkAction(id string, reqType, respType reflect.Type) error {
	c := &declChecker{
		subject:  fmt.Sprintf("action %q", id),
		seen:     map[reflect.Type]bool{},
		inlining: map[reflect.Type]bool{},
	}
	if st := structType(reqType); st != nil {
		// The request body's schema is always inlined, but a field of its own
		// type gets the component's $ref when the type is one.
		c.seen[st] = true
		if inlinesStruct(st) {
			c.inlining[st] = true
		}
		if err := c.params(st); err != nil {
			return err
		}
		if err := c.fields(st, ""); err != nil {
			return err
		}
	}
	if body, ok := responseBodyType(respType); ok {
		c.inlining = map[reflect.Type]bool{} // the response schema is built on its own
		return c.value(body, "")
	}
	return nil
}

// declChecker walks one action's types for checkAction.
type declChecker struct {
	subject string
	// seen holds the types already walked, which ends recursion: a field's
	// problem depends on its type, not on the path that reached it.
	seen map[reflect.Type]bool
	// inlining holds the inlined struct types whose schemas enclose the value
	// being walked (see structFields).
	inlining map[reflect.Type]bool
}

// fail returns the declaration error for detail.
func (c *declChecker) fail(detail string) error {
	return &declError{subject: c.subject, detail: detail}
}

// params checks the parameter fields of request struct t: that each type can
// bind from its location, then its validate rules, under the parameter's name.
func (c *declChecker) params(t reflect.Type) error {
	for _, p := range paramFields(t) {
		if !bindable(p.field.Type, p.in) {
			return c.fail(fmt.Sprintf("parameter %q: %s cannot be a %s parameter", p.name, p.field.Type, p.in))
		}
		if _, err := parseRules(p.field.Tag.Get("validate"), p.field.Type); err != nil {
			return c.fail(fmt.Sprintf("field %q: %v", p.name, err))
		}
	}
	return nil
}

// fields checks the validate rules of struct t's JSON fields, then descends
// into each field's value. prefix is the path of t's value ("" at the top).
func (c *declChecker) fields(t reflect.Type, prefix string) error {
	for _, jf := range jsonFields(t) {
		path := jf.name
		if prefix != "" {
			path = prefix + "." + jf.name
		}
		if _, err := parseRules(jf.field.Tag.Get("validate"), jf.field.Type); err != nil {
			return c.fail(fmt.Sprintf("field %q: %v", path, err))
		}
		if err := c.value(jf.field.Type, path); err != nil {
			return err
		}
	}
	return nil
}

// value descends into a value of type t (pointers followed) at path: a
// struct's fields, and the elements of a slice or array or the values of a
// map, named "<path>[]". Like the validator and the schema generator, it never
// descends into a type that encodes itself.
func (c *declChecker) value(t reflect.Type, path string) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if encodesItself(t) {
		return nil
	}
	if c.inlining[t] {
		return c.fail(fmt.Sprintf("field %q: %s refers to itself; a self-referencing type needs an exported, "+
			"non-generic name, so its schema can be a component", path, t))
	}
	if c.seen[t] {
		return nil
	}
	c.seen[t] = true
	switch t.Kind() { //nolint:exhaustive // other kinds hold no fields
	case reflect.Struct:
		return c.structFields(t, path)
	case reflect.Slice, reflect.Array, reflect.Map:
		return c.value(t.Elem(), path+"[]")
	default:
		return nil
	}
}

// structFields walks struct t's fields, tracking the inlined struct types
// whose schemas enclose them. The schema generator inlines an unnamed,
// unexported, or generic struct (see inlinesStruct), so meeting such a type
// again inside its own schema means that schema would never end: only a
// component can be referred back to. A component's schema is a $ref, which
// ends every cycle through it, so its fields start with none enclosing them.
func (c *declChecker) structFields(t reflect.Type, path string) error {
	if inlinesStruct(t) {
		c.inlining[t] = true
		defer delete(c.inlining, t)
		return c.fields(t, path)
	}
	enclosing := c.inlining
	c.inlining = map[reflect.Type]bool{}
	defer func() { c.inlining = enclosing }()
	return c.fields(t, path)
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
