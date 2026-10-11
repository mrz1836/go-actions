package actions

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// freezeRefusal registers one POST action "x.list" with request type Req and
// response type Resp, documenting Resp's success status, freezes the
// registry, and returns the text Freeze panicked with, or "" when it accepted
// the declaration.
func freezeRefusal[Req, Resp any](t *testing.T) (refusal string) {
	t.Helper()
	status, fixed := successStatus(reflect.TypeFor[Resp]())
	if !fixed {
		status = http.StatusOK
	}
	reg := NewRegistry()
	Register(reg, Action[Req, Resp]{
		ID: "x.list", Method: http.MethodPost, Path: "/x",
		Statuses: []StatusDoc{{Code: status}},
		Handle: func(context.Context, Req) (Resp, error) {
			var zero Resp
			return zero, nil
		},
	})
	defer func() {
		if r := recover(); r != nil {
			refusal = fmt.Sprint(r)
		}
	}()
	reg.Freeze()
	return ""
}

func TestFreezeRefusesAParameterThatCannotBind(t *testing.T) {
	type (
		point     struct{ X int }
		pathSlice struct {
			Tags []string `json:"-" path:"tags"`
		}
		headerSlice struct {
			Tags []string `json:"-" header:"X-Tags"`
		}
		queryMap struct {
			Attrs map[string]string `json:"-" query:"attrs"`
		}
		queryStruct struct {
			At point `json:"-" query:"at"`
		}
		queryBytes struct {
			Raw []byte `json:"-" query:"raw"`
		}
		queryPointers struct {
			IDs []*int `json:"-" query:"id"`
		}
		queryNested struct {
			Groups [][]string `json:"-" query:"group"`
		}
		queryPointerToSlice struct {
			Tags *[]string `json:"-" query:"tag"`
		}
		queryAny struct {
			Value any `json:"-" query:"value"`
		}
		queryArray struct {
			Pair [2]int `json:"-" query:"pair"`
		}
		queryTwoPointers struct {
			Limit **int `json:"-" query:"limit"`
		}
	)
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			"a path slice", freezeRefusal[pathSlice, Empty](t),
			`actions: action "x.list": parameter "tags": []string cannot be a path parameter`,
		},
		{
			"a header slice", freezeRefusal[headerSlice, Empty](t),
			`actions: action "x.list": parameter "X-Tags": []string cannot be a header parameter`,
		},
		{
			"a map", freezeRefusal[queryMap, Empty](t),
			`actions: action "x.list": parameter "attrs": map[string]string cannot be a query parameter`,
		},
		{
			"a struct", freezeRefusal[queryStruct, Empty](t),
			`actions: action "x.list": parameter "at": actions.point cannot be a query parameter`,
		},
		{
			"a byte slice", freezeRefusal[queryBytes, Empty](t),
			`actions: action "x.list": parameter "raw": []uint8 cannot be a query parameter`,
		},
		{
			"a slice of pointers", freezeRefusal[queryPointers, Empty](t),
			`actions: action "x.list": parameter "id": []*int cannot be a query parameter`,
		},
		{
			"a slice of slices", freezeRefusal[queryNested, Empty](t),
			`actions: action "x.list": parameter "group": [][]string cannot be a query parameter`,
		},
		{
			"a pointer to a slice", freezeRefusal[queryPointerToSlice, Empty](t),
			`actions: action "x.list": parameter "tag": *[]string cannot be a query parameter`,
		},
		{
			"an interface", freezeRefusal[queryAny, Empty](t),
			`actions: action "x.list": parameter "value": interface {} cannot be a query parameter`,
		},
		{
			"an array", freezeRefusal[queryArray, Empty](t),
			`actions: action "x.list": parameter "pair": [2]int cannot be a query parameter`,
		},
		{
			"a pointer to a pointer", freezeRefusal[queryTwoPointers, Empty](t),
			`actions: action "x.list": parameter "limit": **int cannot be a query parameter`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}
