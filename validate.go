package actions

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/mail"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Validatable is the optional escape hatch for validation a struct tag cannot
// express. When the request type implements it (on the value or pointer
// receiver), Validate is always called — whether or not the tag rules passed —
// and its failures are appended to theirs in the one 422 response: a returned
// *APIError contributes its field details (or its message when it has none),
// and any other error becomes a single message-level failure. Only the
// top-level request type is consulted; nested types' Validate methods are not.
type Validatable interface {
	Validate() error
}

// e164Pattern is the JSON Schema pattern of a canonical E.164 phone string; the
// runtime check (isE164) matches exactly the same strings.
const e164Pattern = `^\+[1-9]\d{1,14}$`

// ruleKind identifies one validate rule.
type ruleKind uint8

const (
	ruleRequired ruleKind = iota + 1
	ruleMin
	ruleMax
	ruleOneOf
	ruleUUID
	ruleEmail
	ruleE164
	ruleRFC3339
)

// valueShape classifies a field's type (pointers followed) for the rules that
// apply to it and the schema keywords they become.
type valueShape uint8

const (
	shapeOther  valueShape = iota
	shapeString            // min/max bound the length in characters (runes)
	shapeInt               // min/max bound the value; oneof lists integers
	shapeUint              // as shapeInt, unsigned
	shapeFloat             // as shapeInt, floating point
	shapeItems             // slice or array: min/max bound the length
	shapeProps             // map: min/max bound the number of entries
)

// shapeOf returns the valueShape of t, following pointers.
func shapeOf(t reflect.Type) valueShape {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() { //nolint:exhaustive // every other kind is shapeOther
	case reflect.String:
		return shapeString
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return shapeInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return shapeUint
	case reflect.Float32, reflect.Float64:
		return shapeFloat
	case reflect.Slice, reflect.Array:
		return shapeItems
	case reflect.Map:
		return shapeProps
	default:
		return shapeOther
	}
}

// rule is one parsed validate rule. The same parsed rules drive the runtime
// check (rule.fails) and the schema keywords (applyConstraints), so the two
// cannot disagree.
type rule struct {
	kind  ruleKind
	shape valueShape
	// limit is a min/max bound.
	limit float64
	// allowed are a oneof rule's values as written; enum holds them typed for
	// the shape (string, int64, uint64, or float64).
	allowed []string
	enum    []any
	// msg is the failure message.
	msg string
}

// formatMessages are the failure messages of the string-format rules.
//
//nolint:gochecknoglobals // a fixed lookup table is an intentional package global
var formatMessages = map[ruleKind]string{
	ruleUUID:    "must be a valid UUID",
	ruleEmail:   "must be a valid email address",
	ruleE164:    "must be a valid E.164 phone number",
	ruleRFC3339: "must be an RFC 3339 timestamp",
}

// parseRules parses a validate tag for a field of type t. A rule that does not
// apply to the field's shape, that is malformed, or that is unknown is dropped,
// so it is neither enforced nor documented.
func parseRules(tag string, t reflect.Type) []rule {
	if tag == "" {
		return nil
	}
	shape := shapeOf(t)
	var rules []rule
	for _, raw := range strings.Split(tag, ",") {
		key, arg, _ := strings.Cut(strings.TrimSpace(raw), "=")
		if r, ok := parseRule(key, arg, shape); ok {
			rules = append(rules, r)
		}
	}
	return rules
}

// parseRule parses one rule for a field of the given shape, reporting false
// when the rule is dropped.
func parseRule(key, arg string, shape valueShape) (rule, bool) {
	switch key {
	case "required":
		return rule{kind: ruleRequired, shape: shape, msg: "is required"}, true
	case "min":
		return parseBound(ruleMin, arg, shape)
	case "max":
		return parseBound(ruleMax, arg, shape)
	case "oneof":
		return parseOneOf(arg, shape)
	}
	var kind ruleKind
	switch key {
	case "uuid":
		kind = ruleUUID
	case "email":
		kind = ruleEmail
	case "e164":
		kind = ruleE164
	case "rfc3339":
		kind = ruleRFC3339
	default:
		return rule{}, false
	}
	if shape != shapeString {
		return rule{}, false
	}
	return rule{kind: kind, shape: shape, msg: formatMessages[kind]}, true
}

// parseBound parses a min/max rule. A number bound must be finite; a length
// bound (string, slice, array, map) must be a non-negative integer.
func parseBound(kind ruleKind, arg string, shape valueShape) (rule, bool) {
	var unit string
	switch shape { //nolint:exhaustive // other shapes take no bound
	case shapeString:
		unit = " characters"
	case shapeItems, shapeProps:
		unit = " items"
	case shapeInt, shapeUint, shapeFloat:
	default:
		return rule{}, false
	}
	limit, err := strconv.ParseFloat(arg, 64)
	if err != nil || math.IsInf(limit, 0) || math.IsNaN(limit) {
		return rule{}, false
	}
	if unit != "" && (limit < 0 || limit != math.Trunc(limit)) {
		return rule{}, false
	}
	bound := strconv.FormatFloat(limit, 'f', -1, 64)
	msg := "must be at most " + bound + unit
	if kind == ruleMin {
		msg = "must be at least " + bound + unit
	}
	return rule{kind: kind, shape: shape, limit: limit, msg: msg}, true
}

// parseOneOf parses a oneof rule: space-separated values, each of which must
// parse as the field's kind (string, integer, unsigned integer, or number).
func parseOneOf(arg string, shape valueShape) (rule, bool) {
	allowed := strings.Fields(arg)
	if len(allowed) == 0 {
		return rule{}, false
	}
	enum := make([]any, len(allowed))
	for i, tok := range allowed {
		v, ok := parseEnumValue(tok, shape)
		if !ok {
			return rule{}, false
		}
		enum[i] = v
	}
	return rule{
		kind: ruleOneOf, shape: shape, allowed: allowed, enum: enum,
		msg: "must be one of: " + strings.Join(allowed, ", "),
	}, true
}

// parseEnumValue parses one oneof value as the given shape.
func parseEnumValue(tok string, shape valueShape) (any, bool) {
	switch shape { //nolint:exhaustive // other shapes take no oneof
	case shapeString:
		return tok, true
	case shapeInt:
		n, err := strconv.ParseInt(tok, 10, 64)
		return n, err == nil
	case shapeUint:
		n, err := strconv.ParseUint(tok, 10, 64)
		return n, err == nil
	case shapeFloat:
		n, err := strconv.ParseFloat(tok, 64)
		return n, err == nil && !math.IsInf(n, 0) && !math.IsNaN(n)
	default:
		return nil, false
	}
}

// fails reports whether the present value v (pointers already followed)
// violates the rule. required never fails here: the caller decides it from the
// value's presence.
func (r *rule) fails(v reflect.Value) bool {
	switch r.kind { //nolint:exhaustive // the default case is ruleRFC3339
	case ruleRequired:
		return false
	case ruleMin:
		return measure(v, r.shape) < r.limit
	case ruleMax:
		return measure(v, r.shape) > r.limit
	case ruleOneOf:
		return !r.allows(v)
	case ruleUUID:
		_, err := uuid.Parse(v.String())
		return err != nil
	case ruleEmail:
		_, err := mail.ParseAddress(v.String())
		return err != nil
	case ruleE164:
		return !isE164(v.String())
	default:
		_, err := time.Parse(time.RFC3339, v.String())
		return err != nil
	}
}

// measure returns what a min/max rule bounds: a string's length in runes (the
// unit of JSON Schema's minLength), a collection's length, or a number's value.
func measure(v reflect.Value, shape valueShape) float64 {
	switch shape { //nolint:exhaustive // parseBound admits only these shapes
	case shapeString:
		return float64(utf8.RuneCountInString(v.String()))
	case shapeItems, shapeProps:
		return float64(v.Len())
	case shapeInt:
		return float64(v.Int())
	case shapeUint:
		return float64(v.Uint())
	default:
		return v.Float()
	}
}

// allows reports whether v is one of a oneof rule's values, compared as the
// field's own type.
func (r *rule) allows(v reflect.Value) bool {
	switch r.shape { //nolint:exhaustive // parseOneOf admits only these shapes
	case shapeString:
		return slices.Contains(r.allowed, v.String())
	case shapeInt:
		n := v.Int()
		return slices.ContainsFunc(r.enum, func(e any) bool { return e == any(n) })
	case shapeUint:
		n := v.Uint()
		return slices.ContainsFunc(r.enum, func(e any) bool { return e == any(n) })
	default:
		n := v.Float()
		return slices.ContainsFunc(r.enum, func(e any) bool { return e == any(n) })
	}
}

// isE164 reports whether s is a canonical E.164 phone string: "+", a non-zero
// digit, then 1 to 14 more digits. It matches exactly e164Pattern.
func isE164(s string) bool {
	if len(s) < 3 || len(s) > 16 || s[0] != '+' || s[1] < '1' || s[1] > '9' {
		return false
	}
	for i := 2; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isEmptyValue reports whether fv is empty for the required rule and the
// zero-value skip: an empty string, slice, or map; a zero number; false; a nil
// pointer or interface; or the zero value of a struct or array.
func isEmptyValue(fv reflect.Value) bool {
	switch fv.Kind() { //nolint:exhaustive // every other kind uses IsZero
	case reflect.Slice, reflect.Map:
		return fv.Len() == 0
	case reflect.Float32, reflect.Float64:
		return fv.Float() == 0
	default:
		return fv.IsZero()
	}
}

// vnodeKind is the shape of a value the validator descends into.
type vnodeKind uint8

const (
	vnodeStruct vnodeKind = iota
	vnodeSeq              // slice or array: each element
	vnodeMap              // map: each value
)

// vnode is the compiled validation plan for one value type.
type vnode struct {
	kind vnodeKind
	// fields are a struct's checked fields; elem is a sequence's or map's
	// element plan.
	fields []vfield
	elem   *vnode
	// live reports that some rule is reachable through this node; a node that
	// is not live is never visited.
	live bool
}

// vfield is one checked struct field.
type vfield struct {
	name  string
	index []int
	rules []rule
	// required reports that rules contains required.
	required bool
	// sub is the plan for the field's value, nil when nothing under it is
	// checked.
	sub *vnode
}

// validator is the compiled validation plan for one request type.
type validator struct {
	root *vnode
}

// validatorCache holds one compiled *validator per request struct type.
//
//nolint:gochecknoglobals // an immutable per-type compilation cache
var validatorCache sync.Map

// validatorFor returns the compiled validator for request struct type t,
// building and caching it on first use.
func validatorFor(t reflect.Type) *validator {
	if v, ok := validatorCache.Load(t); ok {
		return v.(*validator)
	}
	v, _ := validatorCache.LoadOrStore(t, compileValidator(t))
	return v.(*validator)
}

// validateRequest checks a decoded request against its validate struct tags
// and its Validatable hook. It returns an *APIError (status 422, code
// VALIDATION_ERROR) with a field-keyed detail list, or nil when the request is
// valid. A value that is not a struct, or a pointer to one, is not validated.
// The same parsed tags feed the JSON Schema generator, so the published
// contract cannot diverge from runtime validation.
func validateRequest(v any) *APIError {
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
	return validatorFor(rv.Type()).validate(rv, v)
}

// validate checks the struct value rv. v is the value handed to
// validateRequest, used for the Validatable hook when rv is not addressable.
func (val *validator) validate(rv reflect.Value, v any) *APIError {
	var w walker
	if val.root.live {
		val.root.check(&w, rv, noParent)
	}
	fields := w.errs
	target := v
	if rv.CanAddr() {
		target = rv.Addr().Interface()
	}
	fields = append(fields, customValidation(target)...)
	if len(fields) == 0 {
		return nil
	}
	return &APIError{
		Status:  http.StatusUnprocessableEntity,
		Code:    CodeValidation,
		Message: "validation failed",
		Fields:  fields,
	}
}

// customValidation runs a request's optional Validatable hook and translates its
// error into field details. An *APIError contributes its Fields (or its message
// when it carries none); any other error becomes one message-level failure.
func customValidation(v any) []FieldError {
	cv, ok := v.(Validatable)
	if !ok {
		return nil
	}
	err := cv.Validate()
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if len(apiErr.Fields) > 0 {
			return apiErr.Fields
		}
		return []FieldError{{Message: apiErr.Message}}
	}
	return []FieldError{{Message: err.Error()}}
}

// check validates v (pointers already followed) against the node. parent is
// the walker segment naming v, or noParent at the request root.
func (n *vnode) check(w *walker, v reflect.Value, parent int) {
	switch n.kind {
	case vnodeStruct:
		for i := range n.fields {
			f := &n.fields[i]
			fv, present := fieldByIndex(v, f.index)
			f.checkRules(w, fv, present, parent)
			if f.sub != nil && present {
				if ev, ok := deref(fv); ok {
					at := w.push(pathSeg{parent: parent, label: f.name, kind: segField})
					f.sub.check(w, ev, at)
					w.pop(at)
				}
			}
		}
	case vnodeSeq:
		for i := range v.Len() {
			if ev, ok := deref(v.Index(i)); ok {
				at := w.push(pathSeg{parent: parent, index: i, kind: segIndex})
				n.elem.check(w, ev, at)
				w.pop(at)
			}
		}
	case vnodeMap:
		n.checkMap(w, v, parent)
	}
}

// checkMap validates each value of the map v, in key order so the failures
// are deterministic.
func (n *vnode) checkMap(w *walker, v reflect.Value, parent int) {
	type entry struct {
		key string
		val reflect.Value
	}
	entries := make([]entry, 0, v.Len())
	for it := v.MapRange(); it.Next(); {
		entries = append(entries, entry{key: mapKeyString(it.Key()), val: it.Value()})
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.key, b.key) })
	for _, e := range entries {
		if ev, ok := deref(e.val); ok {
			at := w.push(pathSeg{parent: parent, label: e.key, kind: segKey})
			n.elem.check(w, ev, at)
			w.pop(at)
		}
	}
}

// checkRules applies the field's rules to fv. A value that is absent — a nil
// pointer, or a field behind a nil embedded pointer — fails only required. A
// non-pointer empty value (see isEmptyValue) likewise skips every other rule.
// A non-nil pointer is always checked, so an explicit zero is validated.
func (f *vfield) checkRules(w *walker, fv reflect.Value, present bool, parent int) {
	if len(f.rules) == 0 {
		return
	}
	v, ok := fv, present
	isPointer := false
	for ok && v.Kind() == reflect.Pointer {
		isPointer = true
		if v.IsNil() {
			ok = false
			break
		}
		v = v.Elem()
	}
	if !ok || (!isPointer && isEmptyValue(v)) {
		if f.required {
			w.fail(parent, f.name, "is required")
		}
		return
	}
	for i := range f.rules {
		if r := &f.rules[i]; r.fails(v) {
			w.fail(parent, f.name, r.msg)
		}
	}
}

// fieldByIndex returns the field of struct v at index, reporting false when a
// nil embedded pointer on the path leaves it absent.
func fieldByIndex(v reflect.Value, index []int) (reflect.Value, bool) {
	for i, x := range index {
		if i > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v, true
}

// deref follows pointers from v, reporting false at a nil one.
func deref(v reflect.Value) (reflect.Value, bool) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	return v, true
}

// mapKeyString renders a map key for an error path.
func mapKeyString(k reflect.Value) string {
	switch k.Kind() { //nolint:exhaustive // other key kinds use fmt
	case reflect.String:
		return k.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(k.Uint(), 10)
	default:
		return fmt.Sprint(k)
	}
}

// noParent is the parent segment of a request's top-level fields.
const noParent = -1

// walker carries one validation pass: the failures so far and the stack of
// path segments leading to the value being checked. Segments link to their
// parent by index and are rendered only when a rule fails, so a valid request
// allocates nothing for its paths.
type walker struct {
	segs []pathSeg
	errs []FieldError
}

// segKind is what a pathSeg names.
type segKind uint8

const (
	segField segKind = iota // a struct field: label is its name
	segIndex                // a slice or array element: index is its position
	segKey                  // a map value: label is its key
)

// pathSeg is one step of an error path.
type pathSeg struct {
	label  string
	parent int
	index  int
	kind   segKind
}

// segCapacity is the initial depth of a walker's segment stack, enough for
// typical nesting without regrowth.
const segCapacity = 8

// push adds a segment and returns its index.
func (w *walker) push(seg pathSeg) int {
	if w.segs == nil {
		w.segs = make([]pathSeg, 0, segCapacity)
	}
	w.segs = append(w.segs, seg)
	return len(w.segs) - 1
}

// pop removes the segment at index at, and every segment after it.
func (w *walker) pop(at int) {
	w.segs = w.segs[:at]
}

// fail records a failure of the field name under the segment parent.
func (w *walker) fail(parent int, name, message string) {
	var b strings.Builder
	if parent != noParent {
		w.writePath(&b, parent)
		b.WriteByte('.')
	}
	b.WriteString(name)
	w.errs = append(w.errs, FieldError{Field: b.String(), Message: message})
}

// writePath renders the path ending at segment at: fields join with ".",
// slice indexes and map keys are bracketed — e.g. "items[0].name" or
// "attrs[color].value".
func (w *walker) writePath(b *strings.Builder, at int) {
	seg := w.segs[at]
	if seg.parent != noParent {
		w.writePath(b, seg.parent)
	}
	switch seg.kind {
	case segIndex:
		b.WriteByte('[')
		b.WriteString(strconv.Itoa(seg.index))
		b.WriteByte(']')
	case segKey:
		b.WriteByte('[')
		b.WriteString(seg.label)
		b.WriteByte(']')
	case segField:
		if seg.parent != noParent {
			b.WriteByte('.')
		}
		b.WriteString(seg.label)
	}
}

// validatorCompiler builds the vnodes of one request type.
type validatorCompiler struct {
	nodes map[reflect.Type]*vnode
	all   []*vnode
}

// compileValidator builds the validation plan for request struct type t: its
// JSON fields (see jsonFields) and parameter fields (see paramFields), each
// with its parsed rules, recursing into nested structs, slices, arrays, and
// maps. A parameter field reports errors under its parameter name.
func compileValidator(t reflect.Type) *validator {
	c := &validatorCompiler{nodes: map[reflect.Type]*vnode{}}
	root := &vnode{kind: vnodeStruct}
	c.all = append(c.all, root)

	params := paramFields(t)
	paramAt := make(map[int]paramField, len(params))
	for _, p := range params {
		paramAt[p.index] = p
	}
	for _, jf := range jsonFields(t) {
		name := jf.name
		if len(jf.index) == 1 {
			if p, ok := paramAt[jf.index[0]]; ok {
				name = p.name
				delete(paramAt, p.index)
			}
		}
		root.fields = append(root.fields, c.field(name, jf.index, jf.field))
	}
	for _, p := range params {
		if _, unbound := paramAt[p.index]; unbound {
			root.fields = append(root.fields, newVField(p.name, []int{p.index}, p.field.Tag.Get("validate"), p.field.Type))
		}
	}
	slices.SortFunc(root.fields, func(a, b vfield) int { return slices.Compare(a.index, b.index) })

	c.resolveLiveness()
	return &validator{root: root}
}

// field compiles one JSON field, including the plan for its nested value.
func (c *validatorCompiler) field(name string, index []int, sf reflect.StructField) vfield {
	f := newVField(name, index, sf.Tag.Get("validate"), sf.Type)
	f.sub = c.nodeFor(sf.Type)
	return f
}

// newVField builds a field with its parsed rules.
func newVField(name string, index []int, tag string, t reflect.Type) vfield {
	f := vfield{name: name, index: index, rules: parseRules(tag, t)}
	for _, r := range f.rules {
		if r.kind == ruleRequired {
			f.required = true
		}
	}
	return f
}

// nodeFor returns the plan for values of type t (pointers followed), or nil
// when the validator does not descend into it. It descends into exactly the
// types the schema generator describes field by field: structs other than
// time.Time and types with their own JSON or text encoding, and the slices,
// arrays, and maps holding them. A type under construction is returned as its
// placeholder, which handles recursive types.
func (c *validatorCompiler) nodeFor(t reflect.Type) *vnode {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if n, seen := c.nodes[t]; seen {
		return n
	}
	if encodesItself(t) {
		c.nodes[t] = nil
		return nil
	}
	var n *vnode
	switch t.Kind() { //nolint:exhaustive // other kinds hold no fields
	case reflect.Struct:
		n = &vnode{kind: vnodeStruct}
		c.nodes[t] = n
		for _, jf := range jsonFields(t) {
			n.fields = append(n.fields, c.field(jf.name, jf.index, jf.field))
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		kind := vnodeSeq
		if t.Kind() == reflect.Map {
			kind = vnodeMap
		}
		n = &vnode{kind: kind}
		c.nodes[t] = n
		n.elem = c.nodeFor(t.Elem())
	default:
		c.nodes[t] = nil
		return nil
	}
	c.all = append(c.all, n)
	return n
}

// resolveLiveness marks, to a fixpoint, every node through which some rule is
// reachable, then prunes fields and elements that lead nowhere.
func (c *validatorCompiler) resolveLiveness() {
	for changed := true; changed; {
		changed = false
		for _, n := range c.all {
			if !n.live && n.reachesRule() {
				n.live = true
				changed = true
			}
		}
	}
	for _, n := range c.all {
		kept := n.fields[:0]
		for _, f := range n.fields {
			if f.sub != nil && !f.sub.live {
				f.sub = nil
			}
			if len(f.rules) > 0 || f.sub != nil {
				kept = append(kept, f)
			}
		}
		n.fields = kept
	}
}

// reachesRule reports whether the node has a rule of its own or an element or
// field plan that is live.
func (n *vnode) reachesRule() bool {
	if n.elem != nil {
		return n.elem.live
	}
	for _, f := range n.fields {
		if len(f.rules) > 0 || (f.sub != nil && f.sub.live) {
			return true
		}
	}
	return false
}
