package actions

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// requestBinder is the compiled binding plan for one request type: which
// fields bind from the path, query string, and headers. It is built once per
// type (see binderFor), so a request pays no struct-tag parsing.
type requestBinder struct {
	// isStruct reports that the request type is a struct, or a pointer to one;
	// any other request type binds nothing.
	isStruct bool
	params   []paramBinding
	// hasQuery reports that some parameter binds from the query string, so the
	// query is parsed (once) per request.
	hasQuery bool
}

// paramBinding is one parameter field plus its pre-computed lookup key.
type paramBinding struct {
	paramField

	// headerKey is the canonical header key for an "in: header" parameter.
	headerKey string
	// slice reports a query parameter that binds every value of the repeated
	// parameter (see isSliceParam); elem is then the slice's element type.
	slice bool
	elem  reflect.Type
}

// binderCache holds one compiled *requestBinder per request type.
//
//nolint:gochecknoglobals // an immutable per-type compilation cache
var binderCache sync.Map

// binderFor returns the compiled binder for request type t, building and
// caching it on first use.
func binderFor(t reflect.Type) *requestBinder {
	if b, ok := binderCache.Load(t); ok {
		return b.(*requestBinder)
	}
	b, _ := binderCache.LoadOrStore(t, newRequestBinder(t))
	return b.(*requestBinder)
}

// newRequestBinder compiles the binding plan for request type t.
func newRequestBinder(t reflect.Type) *requestBinder {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	b := &requestBinder{}
	if t.Kind() != reflect.Struct {
		return b
	}
	b.isStruct = true
	for _, p := range paramFields(t) {
		pb := paramBinding{paramField: p}
		switch p.in {
		case "query":
			b.hasQuery = true
			if isSliceParam(p) {
				pb.slice, pb.elem = true, p.field.Type.Elem()
			}
		case "header":
			pb.headerKey = textproto.CanonicalMIMEHeaderKey(p.name)
		}
		b.params = append(b.params, pb)
	}
	return b
}

// decodeRequest binds an incoming *http.Request into a typed Req value, reading
// the body only when the request method carries one (see methodHasBody). It is
// the generic form of requestBinder.bind.
func decodeRequest[Req any](r *http.Request, strict bool) (Req, error) {
	var req Req
	err := binderFor(reflect.TypeFor[Req]()).bind(r, reflect.ValueOf(&req).Elem(), methodHasBody(r.Method), strict)
	return req, err
}

// bind decodes r into dst, an addressable request value. A struct request (or a
// pointer to one, which is allocated) takes its JSON fields from the body when
// withBody is set, then its path/query/header fields from the URL and headers;
// a parameter overrides a body field of the same Go field. A scalar parameter
// binds its first value, and a slice query parameter every value (see
// setSlice). An absent or empty parameter leaves the field untouched. A
// malformed body yields a 400 (see decodeBody); a parameter that cannot be
// converted yields a 422.
func (b *requestBinder) bind(r *http.Request, dst reflect.Value, withBody, strict bool) error {
	if !b.isStruct {
		return nil
	}
	v := dst
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if withBody {
		if err := decodeBody(r, v.Addr().Interface(), strict); err != nil {
			return err
		}
	}
	if len(b.params) == 0 {
		return nil
	}
	var query url.Values
	if b.hasQuery {
		query = r.URL.Query()
	}
	for i := range b.params {
		p := &b.params[i]
		var err error
		if p.slice {
			err = setSlice(v.Field(p.index), query[p.name], p.elem, p.name)
		} else {
			err = setScalar(v.Field(p.index), p.firstValue(r, query), p.name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// firstValue returns the scalar parameter's value: the path segment, or the
// first value of the query parameter or header ("" when absent).
func (p *paramBinding) firstValue(r *http.Request, query url.Values) string {
	switch p.in {
	case "path":
		return chi.URLParam(r, p.name)
	case "query":
		return query.Get(p.name)
	default:
		if vals := r.Header[p.headerKey]; len(vals) > 0 {
			return vals[0]
		}
		return ""
	}
}

// jsonMediaType is the Content-Type prefix whose request bodies are decoded.
const jsonMediaType = "application/json"

// malformedBodyMessage is the client-facing message for a body that fails to
// decode. Strict mode sends it alone, with no parser detail.
const malformedBodyMessage = "malformed JSON body"

// decodeBody decodes a JSON request body into req (a pointer) when the request
// carries one: an absent or empty body, or a Content-Type other than JSON, is
// skipped. The JSON media type matches case-insensitively (RFC 9110). In strict
// mode (WithStrictDecoding) an unknown field, or any data after the first JSON
// value, is rejected, and every malformed body carries only the generic
// message.
func decodeBody(r *http.Request, req any, strict bool) error {
	if r.Body == nil {
		return nil
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && !isJSONContentType(ct) {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(req); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return bodyError(err, strict)
	}
	if strict {
		// Exactly one JSON value: anything but whitespace after it is rejected.
		if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return bodyError(err, strict)
		}
	}
	return nil
}

// isJSONContentType reports whether ct starts with the JSON media type,
// ignoring case.
func isJSONContentType(ct string) bool {
	return len(ct) >= len(jsonMediaType) && strings.EqualFold(ct[:len(jsonMediaType)], jsonMediaType)
}

// bodyError maps a body decode failure to an APIError: a 413 when the body
// exceeded the size cap, otherwise a 400 whose message appends the parser
// detail only outside strict mode. err may be nil in strict mode (a second,
// well-formed value followed the first).
func bodyError(err error, strict bool) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return &APIError{
			Status:  http.StatusRequestEntityTooLarge,
			Code:    CodePayloadTooLarge,
			Message: "request body too large",
		}
	}
	message := malformedBodyMessage
	if !strict && err != nil {
		message += ": " + err.Error()
	}
	return &APIError{
		Status:  http.StatusBadRequest,
		Code:    CodeBadRequest,
		Message: message,
	}
}

// errInvalidTime signals that a time-typed parameter could not be parsed.
var errInvalidTime = errors.New("invalid time value")

// parseTimeValue parses a query/path/header time parameter, accepting RFC3339
// (with or without sub-second precision) and a bare calendar date (2006-01-02,
// interpreted as UTC midnight). It is the time counterpart to the strconv
// conversions in setScalar.
func parseTimeValue(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: %q", errInvalidTime, raw)
}

// textUnmarshalerType is the reflect.Type of encoding.TextUnmarshaler.
//
//nolint:gochecknoglobals // a cached reflect.Type is an intentional package global
var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

// isSliceParam reports whether a parameter binds every value of a repeated
// query parameter: a query field of slice type, other than a type whose
// pointer implements encoding.TextUnmarshaler, which binds one value.
func isSliceParam(p paramField) bool {
	t := p.field.Type
	return p.in == "query" && t.Kind() == reflect.Slice && !reflect.PointerTo(t).Implements(textUnmarshalerType)
}

// setSlice binds the values of a repeated query parameter into the slice field
// fv, in order, converting each into an element of type elem as setScalar
// does. An empty value is skipped, but it still counts toward the position i
// that names a failure "<name>[<i>]", so the name points at the value as the
// client sent it. A comma is part of a value. The field is set only when some
// value converted, so an absent or all-empty parameter leaves it untouched.
func setSlice(fv reflect.Value, vals []string, elem reflect.Type, name string) error {
	var out reflect.Value
	for i, raw := range vals {
		if raw == "" {
			continue
		}
		ev := reflect.New(elem).Elem()
		if err := setScalar(ev, raw, fmt.Sprintf("%s[%d]", name, i)); err != nil {
			return err
		}
		if !out.IsValid() {
			out = reflect.MakeSlice(fv.Type(), 0, len(vals))
		}
		out = reflect.Append(out, ev)
	}
	if out.IsValid() {
		fv.Set(out)
	}
	return nil
}

// paramError is the 422 for a parameter value that cannot be converted.
func paramError(name, message string) error {
	return &APIError{
		Status: http.StatusUnprocessableEntity, Code: CodeValidation,
		Message: "validation failed", Fields: []FieldError{{Field: name, Message: message}},
	}
}

// setScalar converts the string raw into the parameter field fv, reporting a
// failure under name. An empty raw leaves the field untouched. It allocates and
// follows a pointer field — so an optional parameter can be typed as *bool,
// *int, *time.Time, and so on — binds an RFC3339 (or bare calendar date) string
// into a time.Time, defers to encoding.TextUnmarshaler (so uuid.UUID binds),
// and otherwise converts by kind: string, bool, and the integer and float kinds.
//
//nolint:gocyclo // one switch over the bindable kinds
func setScalar(fv reflect.Value, raw, name string) error {
	if raw == "" {
		return nil
	}
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			fv.Set(reflect.New(fv.Type().Elem()))
		}
		return setScalar(fv.Elem(), raw, name)
	}
	if fv.Type() == timeType {
		t, err := parseTimeValue(raw)
		if err != nil {
			return paramError(name, "must be an RFC3339 timestamp")
		}
		fv.Set(reflect.ValueOf(t))
		return nil
	}
	if reflect.PointerTo(fv.Type()).Implements(textUnmarshalerType) {
		u := fv.Addr().Interface().(encoding.TextUnmarshaler)
		if err := u.UnmarshalText([]byte(raw)); err != nil {
			return paramError(name, textErrorMessage(fv.Type()))
		}
		return nil
	}
	switch fv.Kind() { //nolint:exhaustive // unhandled kinds fall through to the default
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return paramError(name, "must be a boolean")
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, fv.Type().Bits())
		if err != nil {
			return paramError(name, "must be an integer")
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, fv.Type().Bits())
		if err != nil {
			return paramError(name, "must be a non-negative integer")
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(raw, fv.Type().Bits())
		if err != nil {
			return paramError(name, "must be a number")
		}
		fv.SetFloat(n)
	default:
		// Freeze refuses a parameter of any other type (see bindable), so a
		// frozen registry never reaches this case. It stays as a defense: a
		// declaration error is a 500, never a panic on SetString.
		return &APIError{
			Status:  http.StatusInternalServerError,
			Code:    CodeInternal,
			Message: "unsupported request field type: " + fv.Kind().String(),
		}
	}
	return nil
}

// textErrorMessage is the 422 detail for a TextUnmarshaler parameter that
// rejected its value.
func textErrorMessage(t reflect.Type) string {
	if t == uuidType {
		return "must be a valid UUID"
	}
	return "has an invalid format"
}
