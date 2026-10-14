package actions

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// declError is one declaration problem, which Freeze panics with and SchemaFor
// and Validate return.
type declError struct {
	subject string // `action "<id>"`, or `type <pkg.Type>`
	detail  string // `field "<path>": …`, `parameter "<name>": …`, `EnumValues …`, or `types …`
}

// Error renders the problem as Freeze reports it.
func (e *declError) Error() string { return "actions: " + e.subject + ": " + e.detail }

// checkAction returns the first problem in an action's declaration, or nil:
// first its parameters, then its request body fields, then its response body
// fields, each depth first in field order. It reads every field the decoder
// binds, the validator checks, or the schema generator documents, and so
// refuses a parameter the decoder cannot bind, a validate rule that would
// otherwise be dropped (see parseRules), an example its field's rules refuse
// (see checkExample), an EnumValues method that breaks the rules (see enumOf),
// and a type that refers to itself but whose schema would be inlined into
// itself (see structFields).
func checkAction(id string, reqType, respType reflect.Type) error {
	c := newDeclChecker(fmt.Sprintf("action %q", id))
	if st := structType(reqType); st != nil {
		if err := c.request(st); err != nil {
			return err
		}
	}
	if body, ok := responseBodyType(respType); ok {
		c.inlining = map[reflect.Type]bool{} // the response schema is built on its own
		return c.value(body, "")
	}
	return nil
}

// checkRequest is checkAction's request half, for a type read as a request
// outside a registry (SchemaFor and Validate): a struct, or a pointer to one,
// is checked as a request type, its parameters and then its body fields; any
// other type as a value. subject names the type in the error.
func checkRequest(subject string, t reflect.Type) error {
	c := newDeclChecker(subject)
	if st := structType(t); st != nil {
		return c.request(st)
	}
	return c.value(t, "")
}

// declChecker walks one action's types for checkAction, or one type's for
// checkRequest.
type declChecker struct {
	subject string
	// seen holds the types already walked, which ends recursion: a field's
	// problem depends on its type, not on the path that reached it.
	seen map[reflect.Type]bool
	// inlining holds the inlined struct types whose schemas enclose the value
	// being walked (see structFields).
	inlining map[reflect.Type]bool
}

// newDeclChecker returns a checker whose errors name subject.
func newDeclChecker(subject string) *declChecker {
	return &declChecker{
		subject:  subject,
		seen:     map[reflect.Type]bool{},
		inlining: map[reflect.Type]bool{},
	}
}

// fail returns the declaration error for detail.
func (c *declChecker) fail(detail string) error {
	return &declError{subject: c.subject, detail: detail}
}

// request checks request struct type t: its parameter fields, then its body
// fields.
func (c *declChecker) request(t reflect.Type) error {
	// The request body's schema is always inlined, but a field of its own
	// type gets the component's $ref when the type is one.
	c.seen[t] = true
	if inlinesStruct(t) {
		c.inlining[t] = true
	}
	if err := c.params(t); err != nil {
		return err
	}
	return c.fields(t, "")
}

// params checks the parameter fields of request struct t: that each type can
// bind from its location, then its validate rules, under the parameter's name,
// then the EnumValues method of its type and of a slice's element type, then
// its example.
func (c *declChecker) params(t reflect.Type) error {
	for _, p := range paramFields(t) {
		if !bindable(p.field.Type, p.in) {
			return c.fail(fmt.Sprintf("parameter %q: %s cannot be a %s parameter", p.name, p.field.Type, p.in))
		}
		if _, err := parseRules(p.field.Tag.Get("validate"), p.field.Type); err != nil {
			return c.fail(fmt.Sprintf("field %q: %v", p.name, err))
		}
		if _, err := enumOf(p.field.Type); err != nil {
			return err
		}
		if p.field.Type.Kind() == reflect.Slice {
			if _, err := enumOf(p.field.Type.Elem()); err != nil {
				return err
			}
		}
		if reason := checkExample(p.field); reason != "" {
			return c.fail(fmt.Sprintf("field %q: example %s", p.name, reason))
		}
	}
	return nil
}

// fields checks the validate rules of struct t's JSON fields, then descends
// into each field's value, then checks the field's example against rules the
// walk has accepted. prefix is the path of t's value ("" at the top).
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
		if reason := checkExample(jf.field); reason != "" {
			return c.fail(fmt.Sprintf("field %q: example %s", path, reason))
		}
	}
	return nil
}

// checkExample returns why field f's example tag fails the field, or "" when
// it passes or is empty: the example must decode into the field's type, read
// as the schema reads it (see exampleValue), and then pass every check a
// request value there gets, its validate rules, the rules after dive, its
// enum, and those of the types it holds. The reason is "does not decode into
// <Go type>", or the first failure's message, after its path within the value
// when it has one ("zip: must be at most 5 characters").
func checkExample(f reflect.StructField) string {
	tag := f.Tag.Get("example")
	if tag == "" {
		return ""
	}
	value, ok := decodeExample(f.Type, tag)
	if !ok {
		return "does not decode into " + f.Type.String()
	}
	c := &validatorCompiler{nodes: map[reflect.Type]*vnode{}}
	vf := c.field("", nil, f)
	c.resolveLiveness()
	var w walker
	checkRules(&w, value, true, vf.rules, vf.required, noParent, "")
	if v, present := deref(value); present && vf.sub != nil {
		vf.sub.check(&w, v, noParent, &vf)
	}
	if len(w.errs) == 0 {
		return ""
	}
	first := w.errs[0]
	if first.Field == "" {
		return first.Message
	}
	return first.Field + ": " + first.Message
}

// decodeExample decodes an example tag into a new value of type t, reporting
// false when it does not decode.
func decodeExample(t reflect.Type, tag string) (reflect.Value, bool) {
	example, ok := exampleValue(t, tag)
	if !ok {
		return reflect.Value{}, false
	}
	data, err := json.Marshal(example)
	if err != nil {
		return reflect.Value{}, false
	}
	ptr := reflect.New(t)
	if json.Unmarshal(data, ptr.Interface()) != nil {
		return reflect.Value{}, false
	}
	return ptr.Elem(), true
}

// value descends into a value of type t (pointers followed) at path: a
// struct's fields, and the elements of a slice or array or the values of a
// map, named "<path>[]". It checks every type's EnumValues method first, then,
// like the validator and the schema generator, never descends into a type
// that encodes itself.
func (c *declChecker) value(t reflect.Type, path string) error {
	t = derefType(t)
	if _, err := enumOf(t); err != nil {
		return err
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
