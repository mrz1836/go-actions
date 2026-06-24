package actions

import (
	"net/http"
	"net/mail"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// e164Pattern matches a canonical E.164 phone string.
var e164Pattern = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

// validateRequest checks a decoded Req against its validate struct tags. It
// returns an *APIError (status 422, code VALIDATION_ERROR) with a field-keyed
// detail list, or nil when the request is valid. The same tags feed the JSON
// Schema generator, so the published contract cannot diverge from runtime
// validation.
//
//nolint:gocognit // one loop over a struct's validate tags
func validateRequest(v any) *APIError {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}

	var fields []FieldError
	rt := rv.Type()
	for i := range rt.NumField() {
		f := rt.Field(i)
		tag := f.Tag.Get("validate")
		if tag == "" {
			continue
		}
		fv := rv.Field(i)
		name := tagName(f)
		for _, rule := range strings.Split(tag, ",") {
			if rule == "" {
				continue
			}
			if msg := applyRule(strings.TrimSpace(rule), fv); msg != "" {
				fields = append(fields, FieldError{Field: name, Message: msg})
			}
		}
	}
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

// applyRule evaluates one validate rule against fv, returning a failure message
// or "" when the rule holds. Format rules are skipped on an empty value — only
// "required" fails on emptiness.
//
//nolint:gocognit,gocyclo // one switch over the small fixed rule vocabulary
func applyRule(rule string, fv reflect.Value) string {
	key, arg, _ := strings.Cut(rule, "=")
	if key == "required" {
		if isEmptyValue(fv) {
			return "is required"
		}
		return ""
	}
	if isEmptyValue(fv) {
		return ""
	}

	switch key {
	case "min":
		return checkBound(fv, arg, true)
	case "max":
		return checkBound(fv, arg, false)
	case "oneof":
		allowed := strings.Fields(arg)
		s := stringOf(fv)
		for _, a := range allowed {
			if s == a {
				return ""
			}
		}
		return "must be one of: " + strings.Join(allowed, ", ")
	case "uuid":
		if _, err := uuid.Parse(stringOf(fv)); err != nil {
			return "must be a valid UUID"
		}
	case "email":
		if _, err := mail.ParseAddress(stringOf(fv)); err != nil {
			return "must be a valid email address"
		}
	case "e164":
		if !e164Pattern.MatchString(stringOf(fv)) {
			return "must be a valid E.164 phone number"
		}
	case "rfc3339":
		if _, err := time.Parse(time.RFC3339, stringOf(fv)); err != nil {
			return "must be an RFC 3339 timestamp"
		}
	}
	return ""
}

// checkBound evaluates a min/max rule. For strings and slices it bounds the
// length; for numbers it bounds the value.
//
//nolint:gocyclo // one switch over the numeric and length-bearing kinds
func checkBound(fv reflect.Value, arg string, isMin bool) string {
	limit, err := strconv.ParseFloat(arg, 64)
	if err != nil {
		return ""
	}
	var actual float64
	var unit string
	switch fv.Kind() { //nolint:exhaustive // unhandled kinds fall through to the default
	case reflect.String:
		actual, unit = float64(len(fv.String())), " characters"
	case reflect.Slice, reflect.Array, reflect.Map:
		actual, unit = float64(fv.Len()), " items"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		actual = float64(fv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		actual = float64(fv.Uint())
	case reflect.Float32, reflect.Float64:
		actual = fv.Float()
	default:
		return ""
	}
	if isMin && actual < limit {
		return "must be at least " + arg + unit
	}
	if !isMin && actual > limit {
		return "must be at most " + arg + unit
	}
	return ""
}

// isEmptyValue reports whether fv holds its zero value.
func isEmptyValue(fv reflect.Value) bool {
	switch fv.Kind() { //nolint:exhaustive // unhandled kinds fall through to the default
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return fv.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return fv.IsNil()
	case reflect.Bool:
		return !fv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return fv.Float() == 0
	default:
		return false
	}
}

// stringOf returns fv as a string when it is a string or string-pointer,
// otherwise "".
func stringOf(fv reflect.Value) string {
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return ""
		}
		fv = fv.Elem()
	}
	if fv.Kind() == reflect.String {
		return fv.String()
	}
	return ""
}
