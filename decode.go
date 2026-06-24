package actions

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// decodeRequest binds an incoming *http.Request into a typed Req value. JSON
// body fields bind from the request body; fields tagged path/query/header bind
// from the URL, query string, and headers respectively. A field with no
// binding tag is ignored. A malformed JSON body yields a 400; a path/query/
// header value that cannot be converted yields a 422.
//
//nolint:gocognit,gocyclo // one loop over a struct's binding tags
func decodeRequest[Req any](r *http.Request) (Req, error) {
	var req Req
	rv := reflect.ValueOf(&req).Elem()
	if rv.Kind() != reflect.Struct {
		return req, nil
	}

	if err := decodeBody(r, &req); err != nil {
		return req, err
	}

	rt := rv.Type()
	for i := range rt.NumField() {
		f := rt.Field(i)
		fv := rv.Field(i)
		if !fv.CanSet() {
			continue
		}
		switch {
		case hasTag(f, "path"):
			if err := setScalar(fv, chi.URLParam(r, f.Tag.Get("path")), tagName(f)); err != nil {
				return req, err
			}
		case hasTag(f, "query"):
			if v := r.URL.Query().Get(f.Tag.Get("query")); v != "" {
				if err := setScalar(fv, v, tagName(f)); err != nil {
					return req, err
				}
			}
		case hasTag(f, "header"):
			if v := r.Header.Get(f.Tag.Get("header")); v != "" {
				if err := setScalar(fv, v, tagName(f)); err != nil {
					return req, err
				}
			}
		}
	}
	return req, nil
}

// decodeBody decodes a JSON request body into req when the request carries one.
func decodeBody[Req any](r *http.Request, req *Req) error {
	if r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodDelete {
		return nil
	}
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(req); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return &APIError{
			Status:  http.StatusBadRequest,
			Code:    CodeBadRequest,
			Message: "malformed JSON body: " + err.Error(),
		}
	}
	return nil
}

// hasTag reports whether field f carries a non-empty struct tag named key.
func hasTag(f reflect.StructField, key string) bool {
	v, ok := f.Tag.Lookup(key)
	return ok && v != ""
}

// tagName returns the field's JSON name for error reporting, falling back to
// the Go field name.
func tagName(f reflect.StructField) string {
	if j := f.Tag.Get("json"); j != "" && j != "-" {
		return strings.Split(j, ",")[0]
	}
	return f.Name
}

// setScalar converts the string raw into the scalar field fv.
func setScalar(fv reflect.Value, raw, name string) error {
	if raw == "" {
		return nil
	}
	switch fv.Kind() { //nolint:exhaustive // unhandled kinds fall through to the default
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return &APIError{
				Status: http.StatusUnprocessableEntity, Code: CodeValidation,
				Message: "validation failed", Fields: []FieldError{{Field: name, Message: "must be a boolean"}},
			}
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return &APIError{
				Status: http.StatusUnprocessableEntity, Code: CodeValidation,
				Message: "validation failed", Fields: []FieldError{{Field: name, Message: "must be an integer"}},
			}
		}
		fv.SetInt(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return &APIError{
				Status: http.StatusUnprocessableEntity, Code: CodeValidation,
				Message: "validation failed", Fields: []FieldError{{Field: name, Message: "must be a number"}},
			}
		}
		fv.SetFloat(n)
	default:
		fv.SetString(raw)
	}
	return nil
}
