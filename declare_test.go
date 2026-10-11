package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
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

// Fixtures for the declaration checks.
type (
	declLine struct {
		Qty int `json:"qty" validate:"emial"`
	}
	declOrder struct {
		Lines []declLine `json:"lines"`
	}
	declAddress struct {
		Zip string `json:"zip" validate:"min=-1"`
	}
	declByKey struct {
		ByKey map[string]*declLine `json:"by_key"`
	}
	declNested struct {
		Address declAddress `json:"address"`
	}
	declOmit struct {
		Limit *int `json:"limit" validate:"omitempty,min=1"`
	}
	declParamFirst struct {
		Name  string `json:"name" validate:"emial"`
		Limit int    `json:"-" query:"limit" validate:"min=abc"`
	}
	declBodyBeforeResponse struct {
		Name string `json:"name" validate:"max=x"`
	}
	DeclTree struct {
		Label    string     `json:"label" validate:"required"`
		Children []DeclTree `json:"children"`
		Bad      string     `json:"bad" validate:"uuid=4"`
	}
	declMatrix struct {
		Grid [][]declLine `json:"grid"`
	}
	// declOpaque encodes itself, so its fields' tags are never read.
	declOpaque struct {
		Hidden int `json:"hidden" validate:"emial"`
	}
	declHoldsOpaque struct {
		Opaque declOpaque `json:"opaque"`
	}
)

// MarshalJSON makes declOpaque encode itself.
func (declOpaque) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

func TestFreezeRefusesARuleThatCannotApply(t *testing.T) {
	type (
		oneofSlice struct {
			States []string `json:"states" validate:"oneof=queued running"`
		}
		emailInt struct {
			Code int `json:"code" validate:"email"`
		}
		minBool struct {
			Flag bool `json:"flag" validate:"min=1"`
		}
		negativeLength struct {
			Name string `json:"name" validate:"min=-1"`
		}
		badOneOf struct {
			Level int `json:"level" validate:"oneof=1 two"`
		}
		formatTime struct {
			When time.Time `json:"when" validate:"required,rfc3339"`
		}
		requiredArgument struct {
			Name string `json:"name" validate:"required=true"`
		}
		dive struct {
			Tags []string `json:"tags" validate:"dive,min=2"`
		}
		paramRule struct {
			Limit int `json:"-" query:"limit" validate:"min=abc"`
		}
	)
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			"oneof on a slice", freezeRefusal[oneofSlice, Empty](t),
			`actions: action "x.list": field "states": validate rule "oneof" does not apply to []string`,
		},
		{
			"omitempty in a request", freezeRefusal[declOmit, Empty](t),
			`actions: action "x.list": field "limit": unknown validate rule "omitempty"`,
		},
		{
			"omitempty in a response", freezeRefusal[struct{}, declOmit](t),
			`actions: action "x.list": field "limit": unknown validate rule "omitempty"`,
		},
		{
			"omitempty in a wrapped response", freezeRefusal[struct{}, Created[[]declOmit]](t),
			`actions: action "x.list": field "[].limit": unknown validate rule "omitempty"`,
		},
		{
			"a format rule on an int", freezeRefusal[emailInt, Empty](t),
			`actions: action "x.list": field "code": validate rule "email" does not apply to int`,
		},
		{
			"a format rule on a time.Time", freezeRefusal[formatTime, Empty](t),
			`actions: action "x.list": field "when": validate rule "rfc3339" does not apply to time.Time`,
		},
		{
			"a bound on a bool", freezeRefusal[minBool, Empty](t),
			`actions: action "x.list": field "flag": validate rule "min" does not apply to bool`,
		},
		{
			"a negative length", freezeRefusal[negativeLength, Empty](t),
			`actions: action "x.list": field "name": validate rule "min" has an invalid argument`,
		},
		{
			"a oneof value of the wrong type", freezeRefusal[badOneOf, Empty](t),
			`actions: action "x.list": field "level": validate rule "oneof" has an invalid argument`,
		},
		{
			"an argument on a rule that takes none", freezeRefusal[requiredArgument, Empty](t),
			`actions: action "x.list": field "name": validate rule "required" has an invalid argument`,
		},
		{
			"dive is not a rule", freezeRefusal[dive, Empty](t),
			`actions: action "x.list": field "tags": unknown validate rule "dive"`,
		},
		{
			"a parameter's rule", freezeRefusal[paramRule, Empty](t),
			`actions: action "x.list": field "limit": validate rule "min" has an invalid argument`,
		},
		{
			"a slice element's field", freezeRefusal[declOrder, Empty](t),
			`actions: action "x.list": field "lines[].qty": unknown validate rule "emial"`,
		},
		{
			"a nested struct's field", freezeRefusal[declNested, Empty](t),
			`actions: action "x.list": field "address.zip": validate rule "min" has an invalid argument`,
		},
		{
			"a map value's field", freezeRefusal[declByKey, Empty](t),
			`actions: action "x.list": field "by_key[].qty": unknown validate rule "emial"`,
		},
		{
			"an element of an element", freezeRefusal[declMatrix, Empty](t),
			`actions: action "x.list": field "grid[][].qty": unknown validate rule "emial"`,
		},
		{
			"a recursive type", freezeRefusal[DeclTree, Empty](t),
			`actions: action "x.list": field "bad": validate rule "uuid" has an invalid argument`,
		},
		{
			"parameters come first", freezeRefusal[declParamFirst, Empty](t),
			`actions: action "x.list": field "limit": validate rule "min" has an invalid argument`,
		},
		{
			"the request body comes before the response", freezeRefusal[declBodyBeforeResponse, declOmit](t),
			`actions: action "x.list": field "name": validate rule "max" has an invalid argument`,
		},
		{"a type that encodes itself is not read", freezeRefusal[declHoldsOpaque, declHoldsOpaque](t), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
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

// fitStatus is a named string type, for rules on named types.
type fitStatus string

// fitParams binds a parameter of every type the decoder converts.
type fitParams struct {
	ID       string        `json:"-" path:"id" validate:"required,uuid"`
	Expand   bool          `json:"-" query:"expand"`
	Limit    int           `json:"-" query:"limit" validate:"min=1,max=100"`
	Small    *int8         `json:"-" query:"small" validate:"oneof=1 2"`
	Count    uint16        `json:"-" header:"X-Count" validate:"max=9"`
	Ratio    *float64      `json:"-" query:"ratio" validate:"min=0"`
	Since    time.Time     `json:"-" query:"since" validate:"required"`
	Ref      *uuid.UUID    `json:"-" query:"ref"`
	Addr     *netip.Addr   `json:"-" header:"X-Client-IP"`
	Status   fitStatus     `json:"-" query:"status" validate:"oneof=open closed"`
	States   []string      `json:"-" query:"state" validate:"min=1,max=5"`
	Limits   []int         `json:"-" query:"limits"`
	IDs      []uuid.UUID   `json:"-" query:"ids"`
	Times    []time.Time   `json:"-" query:"times"`
	Statuses []fitStatus   `json:"-" query:"statuses"`
	Addrs    []netip.Addr  `json:"-" query:"addrs"`
	Floats   []float32     `json:"-" query:"floats"`
	Flags    []bool        `json:"-" query:"flags"`
	Uints    []uint64      `json:"-" query:"uints"`
	Body     fitBodyFields `json:"body"`
}

// fitBodyFields carries every rule of the README's table on a field it fits.
type fitBodyFields struct {
	Any     any               `json:"any" validate:"required"`
	Name    string            `json:"name" validate:"required,min=1,max=50"`
	Named   fitStatus         `json:"named" validate:"oneof=open closed"`
	Age     int               `json:"age" validate:"min=0,max=150"`
	Score   float64           `json:"score" validate:"min=-1.5,max=1.5"`
	Rank    uint              `json:"rank" validate:"oneof=1 2 3"`
	Ratio   float32           `json:"ratio" validate:"oneof=0.5 1"`
	Tags    []string          `json:"tags" validate:"min=1,max=10"`
	Pair    [2]int            `json:"pair" validate:"min=2"`
	Attrs   map[string]string `json:"attrs" validate:"max=20"`
	Raw     []byte            `json:"raw" validate:"max=64"`
	ID      string            `json:"id" validate:"uuid"`
	Email   *string           `json:"email" validate:"email"`
	Phone   string            `json:"phone" validate:"e164"`
	When    string            `json:"when" validate:"rfc3339"`
	Born    *string           `json:"born" validate:"date"`
	At      time.Time         `json:"at" validate:"required"`
	Ref     uuid.UUID         `json:"ref" validate:"required"`
	Payload json.RawMessage   `json:"payload" openapi:"type=object" validate:"required"`
	Blank   string            `json:"blank" validate:" required ,, max=3 "`
}

func TestFreezeAcceptsEveryFittingRule(t *testing.T) {
	assert.Empty(t, freezeRefusal[fitParams, fitBodyFields](t), "a request and a response")
	assert.Empty(t, freezeRefusal[*fitParams, Created[[]fitBodyFields]](t), "pointer and wrapped types")
	assert.Empty(t, freezeRefusal[int, Response[map[string]fitBodyFields]](t), "a non-struct request")
}
