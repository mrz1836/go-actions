package actions

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// diveReq puts element rules on body fields and on a query parameter.
type diveReq struct {
	Tags   []string  `json:"tags" validate:"max=3,dive,min=2,max=8"`
	IDs    []*string `json:"ids" validate:"dive,required,uuid"`
	Names  []string  `json:"names" validate:"dive,required"`
	Pair   [2]int    `json:"pair" validate:"dive,min=1"`
	States []string  `json:"-" query:"state" validate:"dive,oneof=low high"`
}

func TestDiveChecksEachElement(t *testing.T) {
	require.Empty(t, freezeRefusal[diveReq, Empty](t), "Freeze accepts dive on a slice and an array")

	nope, id := "nope", "01900000-0000-7000-8000-000000000001"
	t.Run("valid elements pass", func(t *testing.T) {
		assert.Nil(t, fieldErrors(t, diveReq{
			Tags: []string{"ab", "abcdefgh"}, IDs: []*string{&id}, Names: []string{"a"},
			Pair: [2]int{1, 2}, States: []string{"low", "high"},
		}))
	})

	t.Run("collection rules check the collection, element rules each element", func(t *testing.T) {
		assert.Equal(t, []string{
			"tags: must be at most 3 items",
			"tags[1]: must be at least 2 characters",
			"tags[2]: must be at most 8 characters",
		}, fieldErrors(t, diveReq{Tags: []string{"ab", "x", "abcdefghij", "ok"}}))
	})

	t.Run("required after dive is checked on each element, a nil pointer included", func(t *testing.T) {
		assert.Equal(t, []string{
			"ids[0]: is required",
			"ids[1]: must be a valid UUID",
			"names[1]: is required",
		}, fieldErrors(t, diveReq{IDs: []*string{nil, &nope, &id}, Names: []string{"a", ""}}))
	})

	t.Run("an array's elements, and a parameter's named by the parameter", func(t *testing.T) {
		assert.Equal(t, []string{
			"pair[1]: must be at least 1",
			"state[1]: must be one of: low, high",
		}, fieldErrors(t, diveReq{Pair: [2]int{1, -1}, States: []string{"low", "mid"}}))
	})
}

// diveSchemaReq has element rules on body fields and a parameter.
type diveSchemaReq struct {
	Tags   []string    `json:"tags" validate:"max=3,dive,min=2,max=8"`
	IDs    []*string   `json:"ids" validate:"dive,required,uuid"`
	States []EnumState `json:"states" validate:"dive,required"`
	Levels []int       `json:"-" query:"level" validate:"min=1,dive,min=1,max=5"`
}

func TestDiveElementRulesBecomeItemsKeywords(t *testing.T) {
	for _, version := range []string{"3.1.0", "3.0.3"} {
		t.Run(version, func(t *testing.T) {
			doc := contractFor[diveSchemaReq, Empty](t, WithOpenAPIVersion(version))
			body := requestBodyOf(t, doc)
			assert.JSONEq(t, `{"type":"array","maxItems":3,"items":{"type":"string","minLength":2,"maxLength":8}}`,
				jsonOf(t, propertyOf(t, body, "tags")))
			assert.JSONEq(t, `{"type":"array","items":{"type":"string","format":"uuid"}}`,
				jsonOf(t, propertyOf(t, body, "ids")), "required after dive writes no keyword")
			assert.JSONEq(t, `{"type":"array","items":{"$ref":"#/components/schemas/EnumState"}}`,
				jsonOf(t, propertyOf(t, body, "states")), "nothing is written beside a $ref")
			assert.NotContains(t, requiredOf(body), "ids", "required after dive doesn't require the field")

			level := parameterOf(t, doc, "level")
			assert.JSONEq(t, `{"type":"array","minItems":1,"items":{"type":"integer","minimum":1,"maximum":5}}`,
				jsonOf(t, level["schema"]), "a parameter's element rules are its items' keywords")
		})
	}
}

func TestParseRulesSplitsAtDive(t *testing.T) {
	const misused = "dive applies once, to a slice or an array"
	stringSlice := reflect.TypeFor[[]string]()
	intSlice := reflect.TypeFor[[]int]()
	tests := []struct {
		name        string
		tag         string
		typ         reflect.Type
		field, elem []ruleKind
		err         string
	}{
		{name: "rules before and after dive", tag: "max=3,dive,min=2,max=8", typ: stringSlice, field: []ruleKind{ruleMax}, elem: []ruleKind{ruleMin, ruleMax}},
		{name: "dive first", tag: "dive,required,uuid", typ: reflect.TypeFor[[]*string](), elem: []ruleKind{ruleRequired, ruleUUID}},
		{name: "no dive", tag: "required,min=1", typ: intSlice, field: []ruleKind{ruleRequired, ruleMin}},
		{name: "an array", tag: "dive,min=1", typ: reflect.TypeFor[[2]int](), elem: []ruleKind{ruleMin}},
		{name: "a pointer to a slice", tag: "dive,email", typ: reflect.TypeFor[*[]string](), elem: []ruleKind{ruleEmail}},
		{name: "an element that is a slice", tag: "dive,min=1", typ: reflect.TypeFor[[][]string](), elem: []ruleKind{ruleMin}},
		{name: "dive twice", tag: "dive,dive,min=1", typ: reflect.TypeFor[[][]string](), elem: []ruleKind{ruleMin}, err: misused},
		{name: "dive on a string", tag: "dive,min=1", typ: reflect.TypeFor[string](), field: []ruleKind{ruleMin}, err: misused},
		{name: "dive on a byte slice", tag: "dive,max=3", typ: reflect.TypeFor[[]byte](), field: []ruleKind{ruleMax}, err: misused},
		{name: "dive on a type that encodes itself", tag: "dive", typ: reflect.TypeFor[uuid.UUID](), err: misused},
		{name: "dive with an argument", tag: "dive=1,min=1", typ: intSlice, field: []ruleKind{ruleMin}, err: `validate rule "dive" has an invalid argument`},
		{name: "the suffix on a collection rule", tag: "oneof=a b,dive,min=1", typ: stringSlice, elem: []ruleKind{ruleMin}, err: `validate rule "oneof" does not apply to []string; check elements after dive`},
		{name: "no suffix on an element rule", tag: "dive,email", typ: intSlice, err: `validate rule "email" does not apply to int`},
		{name: "no suffix on an element that is a slice", tag: "dive,email", typ: reflect.TypeFor[[][]string](), err: `validate rule "email" does not apply to []string`},
		{name: "an enum element takes required", tag: "dive,required", typ: reflect.TypeFor[[]EnumState](), elem: []ruleKind{ruleRequired}},
		{name: "an enum element refuses oneof", tag: "dive,oneof=queued", typ: reflect.TypeFor[[]EnumState](), err: `validate rule "oneof" does not apply to the enum type github.com/mrz1836/go-actions.EnumState`},
		{name: "an enum element refuses a bound", tag: "dive,max=3", typ: reflect.TypeFor[[]EnumState](), err: `validate rule "max" does not apply to actions.EnumState`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := parseRules(tc.tag, tc.typ)
			assert.Equal(t, tc.field, kindsOf(rules.field), "the collection's rules")
			assert.Equal(t, tc.elem, kindsOf(rules.elem), "each element's rules")
			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.err)
			}
		})
	}
}

// kindsOf returns the kinds of rules, nil for none.
func kindsOf(rules []rule) []ruleKind {
	if len(rules) == 0 {
		return nil
	}
	kinds := make([]ruleKind, 0, len(rules))
	for _, r := range rules {
		kinds = append(kinds, r.kind)
	}
	return kinds
}
