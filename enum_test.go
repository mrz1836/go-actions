package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Enum fixtures. A method can't be declared on a type local to a test, so the
// enum types live here.
type (
	// EnumState is a string enum, exported, so its schema is a component.
	EnumState string
	// EnumPriority is an integer enum.
	EnumPriority int
	// enumLevel is unexported, so its schema is inlined.
	enumLevel string
	// EnumView is a response holding an optional enum value.
	EnumView struct {
		State *EnumState `json:"state"`
	}
)

// EnumValues lists the states.
func (EnumState) EnumValues() []EnumState { return []EnumState{"queued", "running", "failed"} }

// EnumValues lists the priorities.
func (EnumPriority) EnumValues() []EnumPriority { return []EnumPriority{1, 5, 10} }

// EnumValues lists the levels.
func (enumLevel) EnumValues() []enumLevel { return []enumLevel{"low", "high"} }

// enumReq uses the enums as body fields and as a repeated query parameter.
type enumReq struct {
	States []EnumState          `json:"-" query:"state"`
	State  EnumState            `json:"state"`
	Prio   EnumPriority         `json:"prio"`
	Level  enumLevel            `json:"level"`
	ByKey  map[string]EnumState `json:"by_key"`
}

func TestEnumIsOneComponentReferencedFromFieldAndParameter(t *testing.T) {
	const stateRef = `{"$ref":"#/components/schemas/EnumState"}`
	for _, tc := range []struct {
		version  string
		nullable string
	}{
		{"3.1.0", `{"oneOf":[` + stateRef + `,{"type":"null"}]}`},
		{"3.0.3", `{"allOf":[` + stateRef + `],"nullable":true}`},
	} {
		t.Run(tc.version, func(t *testing.T) {
			doc := contractFor[enumReq, EnumView](t, WithOpenAPIVersion(tc.version))

			assert.JSONEq(t, `{"type":"string","enum":["queued","running","failed"]}`,
				jsonOf(t, component(t, doc, "EnumState")), "a string enum's component lists its values in order")
			assert.JSONEq(t, `{"type":"integer","enum":[1,5,10]}`,
				jsonOf(t, component(t, doc, "EnumPriority")), "an integer enum's component")
			assert.NotContains(t, componentsOf(doc), "EnumStateInput", "a request and a response share one component")
			assert.NotContains(t, componentsOf(doc), "enumLevel", "an unexported enum is no component")

			body := requestBodyOf(t, doc)
			assert.JSONEq(t, stateRef, jsonOf(t, propertyOf(t, body, "state")), "a field references the component")
			assert.JSONEq(t, `{"$ref":"#/components/schemas/EnumPriority"}`, jsonOf(t, propertyOf(t, body, "prio")))
			assert.JSONEq(t, `{"type":"object","additionalProperties":`+stateRef+`}`,
				jsonOf(t, propertyOf(t, body, "by_key")), "a map value references the component")
			assert.JSONEq(t, `{"type":"string","enum":["low","high"]}`,
				jsonOf(t, propertyOf(t, body, "level")), "an unexported enum is inlined")

			param := parameterOf(t, doc, "state")
			assert.JSONEq(t, `{"type":"array","items":`+stateRef+`}`, jsonOf(t, param["schema"]),
				"a slice parameter's items reference the component")

			view := component(t, doc, "EnumView")
			assert.JSONEq(t, tc.nullable, jsonOf(t, propertyOf(t, view, "state")),
				"a response pointer is a nullable reference")
		})
	}
}

// Fixtures for enum values at any depth.
type (
	enumLine struct {
		State EnumState `json:"state"`
	}
	enumDeepReq struct {
		State  EnumState            `json:"state"`
		Ptr    *EnumState           `json:"ptr"`
		States []EnumState          `json:"states"`
		Ptrs   []*EnumState         `json:"ptrs"`
		ByKey  map[string]EnumState `json:"by_key"`
		Grid   [][]EnumState        `json:"grid"`
		Line   enumLine             `json:"line"`
		Prio   EnumPriority         `json:"prio"`
	}
	enumRequiredReq struct {
		State EnumState `json:"state" validate:"required"`
	}
)

func TestEnumValidationAtAnyDepth(t *testing.T) {
	const states = "must be one of: queued, running, failed"
	paused, queued, empty := EnumState("paused"), EnumState("queued"), EnumState("")

	t.Run("values in the set pass", func(t *testing.T) {
		assert.Nil(t, fieldErrors(t, enumDeepReq{
			State: "running", Ptr: &queued, States: []EnumState{queued, "failed"},
			Ptrs: []*EnumState{nil, &queued}, ByKey: map[string]EnumState{"a": "running"},
			Grid: [][]EnumState{{queued}, {"failed"}}, Line: enumLine{State: "failed"}, Prio: 5,
		}))
	})

	t.Run("a value outside the set fails wherever it sits, with no validate tag", func(t *testing.T) {
		assert.Equal(t, []string{
			"state: " + states,
			"ptr: " + states,
			"states[2]: " + states,
			"ptrs[1]: " + states,
			"by_key[a]: " + states,
			"grid[1][1]: " + states,
			"line.state: " + states,
			"prio: must be one of: 1, 5, 10",
		}, fieldErrors(t, enumDeepReq{
			State: paused, Ptr: &paused, States: []EnumState{queued, "running", paused},
			Ptrs: []*EnumState{nil, &paused}, ByKey: map[string]EnumState{"a": paused, "b": queued},
			Grid: [][]EnumState{{queued}, {queued, paused}}, Line: enumLine{State: paused}, Prio: 7,
		}))
	})

	t.Run("the pointer rule", func(t *testing.T) {
		assert.Nil(t, fieldErrors(t, enumDeepReq{}), "an absent pointer and an empty value pass")
		assert.Equal(t, []string{"ptr: " + states}, fieldErrors(t, enumDeepReq{Ptr: &empty}),
			"a present pointer is checked even when it holds the empty value")
		assert.Equal(t, []string{"state: is required"}, fieldErrors(t, enumRequiredReq{}),
			"an empty value fails only required")
	})
}

// enumParamReq takes enums as a scalar and as a repeated query parameter.
type enumParamReq struct {
	States []EnumState `json:"-" query:"state"`
	One    EnumState   `json:"-" query:"one"`
}

func TestEnumParameterOutsideItsValuesIsA422NamingThem(t *testing.T) {
	reg := NewRegistry()
	Register(reg, Action[enumParamReq, EnumView]{
		ID: "x.list", Method: http.MethodGet, Path: "/x",
		Statuses: []StatusDoc{{Code: http.StatusOK}},
		Handle: func(context.Context, enumParamReq) (EnumView, error) {
			return EnumView{}, nil
		},
	})
	reg.Freeze()
	handler := reg.Handler()

	for _, tc := range []struct {
		name, query string
		status      int
		message     string
	}{
		{"values in the set", "?state=queued&state=failed&one=running", http.StatusOK, ""},
		{
			"a repeated value outside the set", "?state=queued&state=paused", http.StatusUnprocessableEntity,
			"validation failed: state[1]: must be one of: queued, running, failed",
		},
		{
			"a scalar value outside the set", "?one=paused", http.StatusUnprocessableEntity,
			"validation failed: one: must be one of: queued, running, failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x"+tc.query, nil))
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.message == "" {
				return
			}
			var got errorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			assert.Equal(t, tc.message, got.Error)
			assert.Equal(t, CodeValidation, got.Code)
		})
	}
}

// parameterOf returns the named parameter of contractFor's operation.
func parameterOf(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	paths, _ := doc["paths"].(map[string]any)
	item, _ := paths["/schema"].(map[string]any)
	op, _ := item["post"].(map[string]any)
	params, _ := op["parameters"].([]any)
	for _, p := range params {
		if param, ok := p.(map[string]any); ok && param["name"] == name {
			return param
		}
	}
	t.Fatalf("parameter %q missing from %v", name, params)
	return nil
}

// Fixtures for EnumValues methods Freeze refuses, or doesn't read as enums.
type (
	enumPointerReceiver string
	enumWrongReturn     string
	enumTakesArgument   string
	enumNoValues        string
	enumTwice           string
	enumIntTwice        int
	enumMarshalsJSON    string
	enumMarshalsText    string
	enumFloat           float64
	enumBool            bool
	enumRecord          struct {
		X int `json:"x"`
	}
	// EnumHolder embeds an enum, so it has an EnumValues method it didn't
	// declare, which returns another type's values.
	EnumHolder struct {
		EnumState

		Name string `json:"name"`
	}
)

func (*enumPointerReceiver) EnumValues() []enumPointerReceiver { return []enumPointerReceiver{"a"} }

func (enumWrongReturn) EnumValues() []string { return []string{"a"} }

func (enumTakesArgument) EnumValues(int) []enumTakesArgument { return []enumTakesArgument{"a"} }

func (enumNoValues) EnumValues() []enumNoValues { return nil }

func (enumTwice) EnumValues() []enumTwice { return []enumTwice{"queued", "running", "queued"} }

func (enumIntTwice) EnumValues() []enumIntTwice { return []enumIntTwice{1, 2, 1} }

func (enumMarshalsJSON) EnumValues() []enumMarshalsJSON { return []enumMarshalsJSON{"a"} }

// MarshalJSON makes enumMarshalsJSON encode itself.
func (enumMarshalsJSON) MarshalJSON() ([]byte, error) { return []byte(`"a"`), nil }

func (enumMarshalsText) EnumValues() []enumMarshalsText { return []enumMarshalsText{"a"} }

// MarshalText makes enumMarshalsText encode itself.
func (enumMarshalsText) MarshalText() ([]byte, error) { return []byte("a"), nil }

func (enumFloat) EnumValues() []enumFloat { return []enumFloat{0.5} }

func (enumBool) EnumValues() []enumBool { return []enumBool{true} }

func (enumRecord) EnumValues() []enumRecord { return []enumRecord{{X: 1}} }

// enumField is a request whose one body field has type T.
type enumField[T any] struct {
	V T `json:"v"`
}

func TestFreezeRefusesABadEnumValuesMethod(t *testing.T) {
	const pkg = "github.com/mrz1836/go-actions."
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			"a pointer receiver", freezeRefusal[enumField[enumPointerReceiver], Empty](t),
			"actions: type " + pkg + "enumPointerReceiver: EnumValues must be func() []enumPointerReceiver on the value",
		},
		{
			"another return type", freezeRefusal[enumField[enumWrongReturn], Empty](t),
			"actions: type " + pkg + "enumWrongReturn: EnumValues must be func() []enumWrongReturn on the value",
		},
		{
			"an argument", freezeRefusal[enumField[enumTakesArgument], Empty](t),
			"actions: type " + pkg + "enumTakesArgument: EnumValues must be func() []enumTakesArgument on the value",
		},
		{
			"no values", freezeRefusal[enumField[enumNoValues], Empty](t),
			"actions: type " + pkg + "enumNoValues: EnumValues returns no values",
		},
		{
			"a string twice", freezeRefusal[enumField[enumTwice], Empty](t),
			"actions: type " + pkg + "enumTwice: EnumValues returns queued twice",
		},
		{
			"an integer twice", freezeRefusal[enumField[[]enumIntTwice], Empty](t),
			"actions: type " + pkg + "enumIntTwice: EnumValues returns 1 twice",
		},
		{
			"a type that marshals itself to JSON, reached through a field",
			freezeRefusal[enumField[*enumMarshalsJSON], Empty](t),
			"actions: type " + pkg + "enumMarshalsJSON: EnumValues is not allowed on a type that marshals itself",
		},
		{
			"a type that marshals itself to text, reached through a map value",
			freezeRefusal[struct{}, map[string]enumMarshalsText](t),
			"actions: type " + pkg + "enumMarshalsText: EnumValues is not allowed on a type that marshals itself",
		},
		{
			"a float", freezeRefusal[enumField[enumFloat], Empty](t),
			"actions: type " + pkg + "enumFloat: EnumValues is allowed only on a string or integer type",
		},
		{
			"a bool, as a parameter", freezeRefusal[struct {
				B enumBool `json:"-" query:"b"`
			}, Empty](t),
			"actions: type " + pkg + "enumBool: EnumValues is allowed only on a string or integer type",
		},
		{
			"a struct that declares it", freezeRefusal[enumField[enumRecord], Empty](t),
			"actions: type " + pkg + "enumRecord: EnumValues is allowed only on a string or integer type",
		},
		{"a struct that only embeds an enum", freezeRefusal[enumField[EnumHolder], EnumHolder](t), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}
