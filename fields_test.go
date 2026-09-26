package actions

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures for jsonFields: each mirrors one encoding/json embedding rule.
type (
	embedBase struct {
		ID   string `json:"id"`
		Note string `json:"note"`
	}
	embedDeep struct {
		Deep string `json:"deep"`
	}
	embedMid struct {
		embedDeep

		Mid string `json:"mid"`
	}
	EmbedExported struct {
		Exp string `json:"exp"`
	}
	embedName  string
	EmbedAlias string
	embedA     struct {
		Same string `json:"same"`
	}
	embedTagged struct {
		Other string `json:"Same"`
	}
	embedUntagged struct {
		Same string
	}
	// ConflictA and ConflictB tag one name alike; they are embedded together
	// only at runtime (see conflictingEmbeds).
	ConflictA struct {
		Same string `json:"same"`
	}
	ConflictB struct {
		Same string `json:"same"`
	}
)

// jsonFieldCase is one struct shape and the keys encoding/json writes for it.
type jsonFieldCase struct {
	name  string
	value any
	want  []string
}

// jsonFieldCases covers encoding/json's field rules. Every value is fully
// populated, so json.Marshal writes every key that jsonFields reports.
func jsonFieldCases() []jsonFieldCase {
	return []jsonFieldCase{
		{
			name: "tag renames and a nameless tag keeps the Go name",
			value: struct {
				A string `json:"a"`
				B string `json:",omitempty"`
				C string
			}{"1", "2", "3"},
			want: []string{"a", "B", "C"},
		},
		{
			name: "dash skips, dash-comma names the field -",
			value: struct {
				Skip string `json:"-"`
				Dash string `json:"-,"` //nolint:staticcheck // naming a field "-" is the rule under test
			}{"1", "2"},
			want: []string{"-"},
		},
		{
			name: "unexported fields are skipped",
			value: struct {
				Pub  string `json:"pub"`
				priv string
			}{Pub: "1", priv: "2"},
			want: []string{"pub"},
		},
		{
			name: "unexported embedded struct promotes its fields",
			value: struct {
				embedBase

				Extra string `json:"extra"`
			}{embedBase{"1", "2"}, "3"},
			want: []string{"id", "note", "extra"},
		},
		{
			name: "embedded pointer promotes its fields",
			value: struct {
				*EmbedExported

				Own string `json:"own"`
			}{&EmbedExported{"1"}, "2"},
			want: []string{"exp", "own"},
		},
		{
			name: "multi-level embedding is breadth-first",
			value: struct {
				embedMid

				Top string `json:"top"`
			}{embedMid{embedDeep{"1"}, "2"}, "3"},
			want: []string{"deep", "mid", "top"},
		},
		{
			name: "a shallower field shadows a promoted one",
			value: struct {
				embedBase

				ID string `json:"id"`
			}{embedBase{"inner", "n"}, "outer"},
			want: []string{"note", "id"},
		},
		{
			name: "at equal depth a tagged field beats an untagged one",
			value: struct {
				embedTagged
				embedUntagged
			}{embedTagged{"t"}, embedUntagged{"u"}},
			want: []string{"Same"},
		},
		{
			name:  "at equal depth two tagged fields cancel",
			value: conflictingEmbeds(),
			want:  []string{"keep"},
		},
		{
			name: "a type reached again at a deeper level is not revisited",
			value: struct {
				embedDeep
				embedMid
			}{embedDeep{"1"}, embedMid{embedDeep{"2"}, "3"}},
			want: []string{"deep", "mid"},
		},
		{
			name: "an embedded struct with a tag name is a named field",
			value: struct {
				EmbedExported `json:"nested"`
			}{EmbedExported{"1"}},
			want: []string{"nested"},
		},
		{
			name: "an embedded exported non-struct is a field; an unexported one is skipped",
			value: struct {
				EmbedAlias
				embedName
			}{"a", "b"},
			want: []string{"EmbedAlias"},
		},
	}
}

// conflictingEmbeds returns a populated value of
//
//	struct {
//		ConflictA // Same string `json:"same"`
//		ConflictB // Same string `json:"same"`
//		Keep string `json:"keep"`
//	}
//
// built at runtime, because go vet rejects the duplicate tag in source.
func conflictingEmbeds() any {
	typ := reflect.StructOf([]reflect.StructField{
		{Name: "ConflictA", Type: reflect.TypeFor[ConflictA](), Anonymous: true},
		{Name: "ConflictB", Type: reflect.TypeFor[ConflictB](), Anonymous: true},
		{Name: "Keep", Type: reflect.TypeFor[string](), Tag: `json:"keep"`},
	})
	v := reflect.New(typ).Elem()
	v.Field(2).SetString("k")
	return v.Interface()
}

func TestJSONFieldsMatchEncodingJSON(t *testing.T) {
	for _, tc := range jsonFieldCases() {
		t.Run(tc.name, func(t *testing.T) {
			var names []string
			for _, f := range jsonFields(reflect.TypeOf(tc.value)) {
				names = append(names, f.name)
			}
			assert.Equal(t, tc.want, names, "jsonFields order and names")

			raw, err := json.Marshal(tc.value)
			require.NoError(t, err)
			var encoded map[string]any
			require.NoError(t, json.Unmarshal(raw, &encoded))
			keys := make([]string, 0, len(encoded))
			for k := range encoded {
				keys = append(keys, k)
			}
			want := slices.Clone(tc.want)
			slices.Sort(want)
			slices.Sort(keys)
			assert.Equal(t, want, keys, "encoding/json writes exactly the reported keys")
		})
	}
}

func TestJSONFieldsIndexAndPointer(t *testing.T) {
	type outer struct {
		*EmbedExported
		embedBase

		Own string `json:"own"`
	}
	fields := jsonFields(reflect.TypeFor[outer]())
	require.Len(t, fields, 4)

	byName := map[string]jsonField{}
	for _, f := range fields {
		byName[f.name] = f
	}
	assert.Equal(t, []int{0, 0}, byName["exp"].index)
	assert.True(t, byName["exp"].viaPointer, "a field promoted through an embedded pointer")
	assert.Equal(t, []int{1, 0}, byName["id"].index)
	assert.False(t, byName["id"].viaPointer)
	assert.Equal(t, []int{2}, byName["own"].index)
	assert.False(t, byName["own"].viaPointer)
	assert.True(t, byName["own"].tagged)
}

func TestJSONFieldsDominantField(t *testing.T) {
	t.Run("the shallower field wins", func(t *testing.T) {
		type outer struct {
			embedBase

			ID string `json:"id"`
		}
		fields := jsonFields(reflect.TypeFor[outer]())
		require.Len(t, fields, 2)
		assert.Equal(t, "id", fields[1].name)
		assert.Equal(t, []int{1}, fields[1].index)
	})

	t.Run("the tagged field wins at equal depth", func(t *testing.T) {
		type outer struct {
			embedUntagged
			embedTagged
		}
		fields := jsonFields(reflect.TypeFor[outer]())
		require.Len(t, fields, 1)
		assert.Equal(t, []int{1, 0}, fields[0].index)
		assert.True(t, fields[0].tagged)
	})

	t.Run("the same embedded type twice at one depth drops the name", func(t *testing.T) {
		type left struct{ embedA }
		type right struct{ embedA }
		type outer struct {
			left
			right

			Keep string `json:"keep"`
		}
		fields := jsonFields(reflect.TypeFor[outer]())
		require.Len(t, fields, 1)
		assert.Equal(t, "keep", fields[0].name)
	})
}

func TestIsValidJSONTag(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{"", false},
		{"name", true},
		{"with space-and.punct!", true},
		{"ünïcode9", true},
		{`back\slash`, false},
		{`quo"te`, false},
	}
	for _, tc := range tests {
		t.Run(tc.tag, func(t *testing.T) {
			assert.Equal(t, tc.want, isValidJSONTag(tc.tag))
		})
	}
}

func TestParamFields(t *testing.T) {
	type embedded struct {
		Inner string `json:"-" query:"inner"`
	}
	type req struct {
		embedded

		ID     string `json:"-" path:"id"`
		Both   string `json:"-" query:"q" header:"X-Both"`
		Header string `json:"-" header:"X-Trace"`
		Empty  string `json:"-" query:""`
		Body   string `json:"body"`
		hidden string `query:"hidden"` //nolint:unused // proves unexported fields are not parameters
	}
	got := paramFields(reflect.TypeFor[req]())
	require.Len(t, got, 3)
	assert.Equal(t, paramField{in: "path", name: "id", index: 1, field: got[0].field}, got[0])
	assert.Equal(t, "query", got[1].in, "query wins over header")
	assert.Equal(t, "q", got[1].name)
	assert.Equal(t, "header", got[2].in)
	assert.Equal(t, "X-Trace", got[2].name)
}

func TestMethodHasBody(t *testing.T) {
	tests := []struct {
		method string
		want   bool
	}{
		{http.MethodGet, false},
		{http.MethodDelete, false},
		{http.MethodPost, true},
		{http.MethodPut, true},
		{http.MethodPatch, true},
	}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			assert.Equal(t, tc.want, methodHasBody(tc.method))
		})
	}
}
