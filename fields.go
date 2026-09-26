package actions

import (
	"cmp"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// jsonField is one field of a struct type as encoding/json sees it: its JSON
// object key and the index path that reaches it through embedded structs.
type jsonField struct {
	// name is the JSON object key: the json tag's name, else the Go field name.
	name string
	// tagged reports that name came from a json tag.
	tagged bool
	// index is the field's index path from the outer struct (reflect.FieldByIndex).
	index []int
	// field is the leaf struct field.
	field reflect.StructField
	// viaPointer reports that an embedded pointer lies on the path, so the field
	// is absent from the encoded object while that pointer is nil.
	viaPointer bool
}

// jsonFields returns the fields encoding/json encodes and decodes for struct
// type t, following its rules: exported fields and the exported fields of
// embedded structs (including embedded unexported struct types and embedded
// pointers) are promoted breadth-first; a json tag renames a field, and a tag
// without a name keeps the Go name; "-" skips a field. When two fields share a
// name, the shallowest wins; at equal depth a tagged field beats an untagged
// one; otherwise the name is dropped. The result is ordered by index path.
//
//nolint:gocognit,gocyclo // a close copy of encoding/json's typeFields
func jsonFields(t reflect.Type) []jsonField {
	type queued struct {
		typ        reflect.Type
		index      []int
		viaPointer bool
	}
	var current []queued
	next := []queued{{typ: t}}
	var count, nextCount map[reflect.Type]int
	visited := map[reflect.Type]bool{}
	var fields []jsonField

	for len(next) > 0 {
		current, next = next, current[:0]
		count, nextCount = nextCount, map[reflect.Type]int{}

		for _, q := range current {
			if visited[q.typ] {
				continue
			}
			visited[q.typ] = true

			for i := range q.typ.NumField() {
				sf := q.typ.Field(i)
				if sf.Anonymous {
					et := sf.Type
					if et.Kind() == reflect.Pointer {
						et = et.Elem()
					}
					if !sf.IsExported() && et.Kind() != reflect.Struct {
						continue // an embedded unexported non-struct type
					}
					// An embedded unexported struct type may still promote
					// exported fields, so it is explored.
				} else if !sf.IsExported() {
					continue
				}
				tag := sf.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name := jsonFirstSegment(tag)
				if !isValidJSONTag(name) {
					name = ""
				}
				index := make([]int, len(q.index)+1)
				copy(index, q.index)
				index[len(q.index)] = i

				ft := sf.Type
				viaPointer := q.viaPointer
				if ft.Name() == "" && ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
					if sf.Anonymous {
						viaPointer = true
					}
				}

				if name != "" || !sf.Anonymous || ft.Kind() != reflect.Struct {
					tagged := name != ""
					if name == "" {
						name = sf.Name
					}
					f := jsonField{name: name, tagged: tagged, index: index, field: sf, viaPointer: q.viaPointer}
					fields = append(fields, f)
					if count[q.typ] > 1 {
						// The same embedded type reached twice at this depth:
						// record a duplicate so the conflict drops the name.
						fields = append(fields, f)
					}
					continue
				}

				nextCount[ft]++
				if nextCount[ft] == 1 {
					next = append(next, queued{typ: ft, index: index, viaPointer: viaPointer})
				}
			}
		}
	}

	slices.SortFunc(fields, func(a, b jsonField) int {
		if c := strings.Compare(a.name, b.name); c != 0 {
			return c
		}
		if c := cmp.Compare(len(a.index), len(b.index)); c != 0 {
			return c
		}
		if a.tagged != b.tagged {
			if a.tagged {
				return -1
			}
			return 1
		}
		return slices.Compare(a.index, b.index)
	})

	out := fields[:0]
	for i := 0; i < len(fields); {
		fi := fields[i]
		advance := 1
		for i+advance < len(fields) && fields[i+advance].name == fi.name {
			advance++
		}
		// Sorted by depth, then tagged-first: the first field of a name
		// dominates unless the runner-up ties it on both.
		if advance == 1 || len(fi.index) != len(fields[i+1].index) || fi.tagged != fields[i+1].tagged {
			out = append(out, fi)
		}
		i += advance
	}

	slices.SortFunc(out, func(a, b jsonField) int { return slices.Compare(a.index, b.index) })
	return out
}

// isValidJSONTag reports whether s is a json tag name encoding/json honors; an
// invalid one falls back to the Go field name.
func isValidJSONTag(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
			// Backslash and quote are reserved; other punctuation is allowed.
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}

// paramLocations are the struct tags that bind a request field from outside
// the body, in precedence order.
//
//nolint:gochecknoglobals // a fixed lookup table is an intentional package global
var paramLocations = [...]string{"path", "query", "header"}

// paramField is one request field bound from the path, query string, or a
// header.
type paramField struct {
	// in is the parameter location: "path", "query", or "header".
	in string
	// name is the parameter name from the tag.
	name string
	// index is the field's position in the request struct.
	index int
	// field is the struct field.
	field reflect.StructField
}

// paramFields returns the top-level exported fields of struct type t that carry
// a non-empty path, query, or header tag; the first such tag in that order
// wins. Fields of embedded structs are not parameters.
func paramFields(t reflect.Type) []paramField {
	var params []paramField
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		for _, in := range paramLocations {
			if name := f.Tag.Get(in); name != "" {
				params = append(params, paramField{in: in, name: name, index: i, field: f})
				break
			}
		}
	}
	return params
}

// methodHasBody reports whether requests with the given method carry a JSON
// body that the framework decodes. GET and DELETE bodies are never read, so
// their actions document no request body.
func methodHasBody(method string) bool {
	return method != http.MethodGet && method != http.MethodDelete
}
