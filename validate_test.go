package actions

import (
	"errors"
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errPlain is a static, non-API error used to exercise the plain-error branch of
// custom validation.
var errPlain = errors.New("plain failure")

// customReq exercises the Validatable escape hatch alongside tag rules.
type customReq struct {
	Code string `json:"code" validate:"required"`
}

// Validate returns a field-keyed *APIError for "BAD", a plain error for "PLAIN",
// and nil otherwise.
func (c customReq) Validate() error {
	switch c.Code {
	case "BAD":
		return &APIError{Fields: []FieldError{{Field: "code", Message: "must not be BAD"}}}
	case "MSG":
		return &APIError{Message: "bad coupon"}
	case "PLAIN":
		return errPlain
	default:
		return nil
	}
}

func TestValidatable(t *testing.T) {
	t.Parallel()

	t.Run("custom APIError field details merge into the 422", func(t *testing.T) {
		t.Parallel()
		err := validateRequest(&customReq{Code: "BAD"})
		if err == nil || len(err.Fields) != 1 || err.Fields[0].Field != "code" {
			t.Fatalf("got %+v, want one 'code' field error", err)
		}
	})

	t.Run("custom plain error becomes a message-level failure", func(t *testing.T) {
		t.Parallel()
		err := validateRequest(&customReq{Code: "PLAIN"})
		if err == nil || len(err.Fields) != 1 || err.Fields[0].Message != "plain failure" {
			t.Fatalf("got %+v, want a message-level failure", err)
		}
	})

	t.Run("custom APIError without fields becomes a message-level failure", func(t *testing.T) {
		t.Parallel()
		err := validateRequest(&customReq{Code: "MSG"})
		if err == nil || len(err.Fields) != 1 || err.Fields[0].Field != "" || err.Fields[0].Message != "bad coupon" {
			t.Fatalf("got %+v, want one message-level failure", err)
		}
	})

	t.Run("a valid value passes custom validation", func(t *testing.T) {
		t.Parallel()
		if err := validateRequest(&customReq{Code: "OK"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("tag rules and custom validation both contribute", func(t *testing.T) {
		t.Parallel()
		// Empty Code fails the required tag; custom Validate returns nil for "".
		err := validateRequest(&customReq{})
		if err == nil || len(err.Fields) != 1 || err.Fields[0].Message != "is required" {
			t.Fatalf("got %+v, want the required-tag failure", err)
		}
	})
}

//nolint:gocognit,gocyclo // Test function with multiple sub-tests
func TestValidateRequest(t *testing.T) {
	t.Parallel()

	t.Run("required field missing returns 422 validation error", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Name string `json:"name" validate:"required"`
		}
		if err := validateRequest(req{}); err == nil {
			t.Fatal("expected a validation error for an empty required field")
		} else if err.Status != 422 || err.Code != CodeValidation {
			t.Fatalf("status/code = %d/%s, want 422/%s", err.Status, err.Code, CodeValidation)
		}
		if err := validateRequest(req{Name: "x"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("non-struct value validates as nil", func(t *testing.T) {
		t.Parallel()
		if err := validateRequest(42); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("pointer to struct is dereferenced", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Name string `json:"name" validate:"required"`
		}
		if err := validateRequest(&req{}); err == nil {
			t.Fatal("expected a validation error through the pointer")
		}
	})

	t.Run("min and max on a string bound its length", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Code string `json:"code" validate:"min=2,max=4"`
		}
		if err := validateRequest(req{Code: "x"}); err == nil {
			t.Fatal("expected a min-length violation")
		}
		if err := validateRequest(req{Code: "toolong"}); err == nil {
			t.Fatal("expected a max-length violation")
		}
		if err := validateRequest(req{Code: "ok"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("min and max on a slice bound its length", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Items []string `json:"items" validate:"min=1,max=2"`
		}
		if err := validateRequest(req{Items: nil}); err != nil {
			// nil slice is empty → format/range rules skip, only required would fail.
			t.Fatalf("unexpected error for empty optional slice: %v", err)
		}
		if err := validateRequest(req{Items: []string{"a", "b", "c"}}); err == nil {
			t.Fatal("expected a max-items violation")
		}
	})

	t.Run("format and range rules", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Email string `json:"email" validate:"email"`
			Phone string `json:"phone" validate:"e164"`
			ID    string `json:"id" validate:"uuid"`
			When  string `json:"when" validate:"rfc3339"`
			Kind  string `json:"kind" validate:"oneof=a b c"`
			Limit int    `json:"limit" validate:"min=1,max=100"`
		}

		tests := []struct {
			name       string
			in         req
			wantFields int
		}{
			{
				name:       "all rules violated",
				in:         req{Email: "nope", Phone: "12345", ID: "not-a-uuid", When: "yesterday", Kind: "z", Limit: 500},
				wantFields: 6,
			},
			{
				name: "all rules satisfied",
				in: req{
					Email: "jane@example.com",
					Phone: "+13055551234",
					ID:    "01900000-0000-7000-8000-000000000001",
					When:  "2026-05-20T00:00:00Z",
					Kind:  "b",
					Limit: 50,
				},
				wantFields: 0,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				err := validateRequest(tt.in)
				if tt.wantFields == 0 {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					return
				}
				if err == nil {
					t.Fatalf("expected validation errors, got nil")
				}
				if len(err.Fields) != tt.wantFields {
					t.Fatalf("field errors = %d, want %d: %+v", len(err.Fields), tt.wantFields, err.Fields)
				}
			})
		}
	})

	t.Run("optional field with format rule skips empty", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Email string `json:"email" validate:"email"`
		}
		if err := validateRequest(req{}); err != nil {
			t.Fatalf("empty optional field should pass: %v", err)
		}
	})

	t.Run("required across scalar kinds", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Flag  bool     `json:"flag" validate:"required"`
			Count uint     `json:"count" validate:"required"`
			Ratio float64  `json:"ratio" validate:"required"`
			Names []string `json:"names" validate:"required"`
		}
		err := validateRequest(req{})
		if err == nil || len(err.Fields) != 4 {
			t.Fatalf("expected 4 required violations, got %+v", err)
		}
		ok := validateRequest(req{Flag: true, Count: 1, Ratio: 0.5, Names: []string{"a"}})
		if ok != nil {
			t.Fatalf("unexpected error: %v", ok)
		}
	})

	t.Run("required pointer string and format via pointer", func(t *testing.T) {
		t.Parallel()
		type req struct {
			Email *string `json:"email" validate:"required,email"`
		}
		if err := validateRequest(req{}); err == nil {
			t.Fatal("expected required violation for nil pointer")
		}
		bad := "not-an-email"
		if err := validateRequest(req{Email: &bad}); err == nil {
			t.Fatal("expected email-format violation through the pointer")
		}
		good := "jane@example.com"
		if err := validateRequest(req{Email: &good}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("numeric min and max bound the value", func(t *testing.T) {
		t.Parallel()
		type req struct {
			N    int     `json:"n" validate:"min=10,max=20"`
			U    uint    `json:"u" validate:"max=5"`
			Frac float64 `json:"frac" validate:"min=1.5"`
		}
		if err := validateRequest(req{N: 5, U: 9, Frac: 0.2}); err == nil || len(err.Fields) != 3 {
			t.Fatalf("expected 3 numeric violations, got %+v", err)
		}
		if err := validateRequest(req{N: 15, U: 3, Frac: 2.0}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("format rule on a non-string field is ignored", func(t *testing.T) {
		t.Parallel()
		// Format rules apply only to string kinds, at runtime and in the schema.
		type req struct {
			Code int `json:"code" validate:"uuid"`
		}
		if err := validateRequest(req{Code: 7}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("min and max boundaries are inclusive", func(t *testing.T) {
		t.Parallel()
		type strReq struct {
			Code string `json:"code" validate:"min=2,max=4"`
		}
		type numReq struct {
			N int `json:"n" validate:"min=10,max=20"`
		}
		tests := []struct {
			name    string
			err     *APIError
			wantErr bool
		}{
			{"string at min length", validateRequest(strReq{Code: "ab"}), false},
			{"string below min length", validateRequest(strReq{Code: "a"}), true},
			{"string at max length", validateRequest(strReq{Code: "abcd"}), false},
			{"string above max length", validateRequest(strReq{Code: "abcde"}), true},
			{"number at min", validateRequest(numReq{N: 10}), false},
			{"number below min", validateRequest(numReq{N: 9}), true},
			{"number at max", validateRequest(numReq{N: 20}), false},
			{"number above max", validateRequest(numReq{N: 21}), true},
		}
		for _, tt := range tests {
			if tt.wantErr && tt.err == nil {
				t.Fatalf("%s: expected a boundary violation", tt.name)
			}
			if !tt.wantErr && tt.err != nil {
				t.Fatalf("%s: unexpected violation: %v", tt.name, tt.err)
			}
		}
	})
}

// FuzzValidateRequest proves validateRequest never panics across the full rule
// vocabulary (required/min/max/oneof/uuid/email/e164/rfc3339) — on top-level,
// pointer, and nested (struct, slice, and map) fields — when fed arbitrary
// values, and that every failure names its field.
func FuzzValidateRequest(f *testing.F) {
	f.Add("jane@example.com", "+13055551234", "01900000-0000-7000-8000-000000000001", "2026-05-20T00:00:00Z", "b", 50, "line", 2, true)
	f.Add("", "", "", "", "", 0, "", 0, false)
	f.Add("not-an-email", "12345", "not-a-uuid", "yesterday", "z", -1, "much-too-long", 0, true)
	f.Add("\x00", "\x00", "\x00", "\x00", "\x00", 1<<31, "\xff", -7, true)
	f.Fuzz(func(t *testing.T, email, phone, id, when, kind string, n int, name string, qty int, withPtr bool) {
		type line struct {
			Name string `json:"name" validate:"required,max=8"`
			Qty  *int   `json:"qty" validate:"min=1,oneof=1 2 3"`
		}
		type req struct {
			Email string           `json:"email" validate:"email"`
			Phone string           `json:"phone" validate:"e164"`
			ID    string           `json:"id" validate:"uuid"`
			When  string           `json:"when" validate:"rfc3339"`
			Kind  string           `json:"kind" validate:"required,oneof=a b c"`
			Limit int              `json:"limit" validate:"min=1,max=100"`
			Lines []line           `json:"lines" validate:"max=1"`
			ByKey map[string]*line `json:"by_key"`
			Ptr   *line            `json:"ptr"`
		}
		l := line{Name: name}
		if withPtr {
			l.Qty = &qty
		}
		v := req{
			Email: email, Phone: phone, ID: id, When: when, Kind: kind, Limit: n,
			Lines: []line{l, l}, ByKey: map[string]*line{name: &l, "nil": nil},
		}
		if withPtr {
			v.Ptr = &l
		}
		// contract: must not panic on any input, and every failure is named.
		if apiErr := validateRequest(&v); apiErr != nil {
			for _, fe := range apiErr.Fields {
				if fe.Field == "" || fe.Message == "" {
					t.Fatalf("unnamed failure %+v", fe)
				}
			}
		}
	})
}

// BenchmarkValidateRequest measures the reflection cost of validating a request
// struct across the full rule vocabulary on the per-request hot path.
func BenchmarkValidateRequest(b *testing.B) {
	type req struct {
		Email string `json:"email" validate:"required,email"`
		Phone string `json:"phone" validate:"e164"`
		ID    string `json:"id" validate:"uuid"`
		When  string `json:"when" validate:"rfc3339"`
		Kind  string `json:"kind" validate:"oneof=a b c"`
		Limit int    `json:"limit" validate:"min=1,max=100"`
	}
	v := req{
		Email: "jane@example.com",
		Phone: "+13055551234",
		ID:    "01900000-0000-7000-8000-000000000001",
		When:  "2026-05-20T00:00:00Z",
		Kind:  "b",
		Limit: 50,
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := validateRequest(v); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// fieldErrors returns the "field: message" pairs of a validation error, or nil
// when v is valid.
func fieldErrors(t *testing.T, v any) []string {
	t.Helper()
	apiErr := validateRequest(v)
	if apiErr == nil {
		return nil
	}
	require.Equal(t, 422, apiErr.Status)
	require.Equal(t, CodeValidation, apiErr.Code)
	out := make([]string, len(apiErr.Fields))
	for i, fe := range apiErr.Fields {
		out[i] = fe.Field + ": " + fe.Message
	}
	return out
}

// Fixtures for nested validation (A1).
type (
	nestedAddress struct {
		City string `json:"city" validate:"required"`
		Zip  string `json:"zip" validate:"max=5"`
	}
	nestedItem struct {
		Name string `json:"name" validate:"required"`
		Qty  int    `json:"qty" validate:"min=1"`
	}
	nestedAttr struct {
		Value string `json:"value" validate:"oneof=a b"`
	}
	nestedTree struct {
		Label    string       `json:"label" validate:"required"`
		Children []nestedTree `json:"children"`
	}
	nestedQuiet struct {
		Free string `json:"free"`
	}
	nestedEmbedded struct {
		Promoted string `json:"promoted" validate:"required"`
	}
	nestedReq struct {
		*nestedEmbedded

		Address  nestedAddress            `json:"address"`
		Billing  *nestedAddress           `json:"billing"`
		Items    []nestedItem             `json:"items" validate:"max=3"`
		Ptrs     []*nestedItem            `json:"ptrs"`
		Fixed    [1]nestedItem            `json:"fixed"`
		Attrs    map[string]nestedAttr    `json:"attrs"`
		ByNum    map[int]*nestedAttr      `json:"by_num"`
		Grid     [][]nestedItem           `json:"grid"`
		Tree     nestedTree               `json:"tree"`
		Quiet    []nestedQuiet            `json:"quiet"`
		Anything map[string]any           `json:"anything"`
		Unsigned map[uint]nestedAttr      `json:"unsigned"`
		Keyed    map[nestedKey]nestedAttr `json:"keyed"`
	}
	nestedKey struct{ K string }
)

// validNestedReq returns a nestedReq that passes every rule.
func validNestedReq() nestedReq {
	ok := nestedItem{Name: "x", Qty: 1}
	return nestedReq{
		nestedEmbedded: &nestedEmbedded{Promoted: "p"},
		Address:        nestedAddress{City: "Miami"},
		Items:          []nestedItem{ok},
		Ptrs:           []*nestedItem{&ok, nil},
		Fixed:          [1]nestedItem{ok},
		Tree:           nestedTree{Label: "root", Children: []nestedTree{{Label: "leaf"}}},
	}
}

func TestValidateNested(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*nestedReq)
		want   []string
	}{
		{name: "a valid nested request passes", mutate: func(*nestedReq) {}},
		{
			name:   "nested struct field",
			mutate: func(r *nestedReq) { r.Address.City = "" },
			want:   []string{"address.city: is required"},
		},
		{
			name:   "a nil nested pointer is skipped",
			mutate: func(r *nestedReq) { r.Billing = nil },
		},
		{
			name:   "a non-nil nested pointer is checked",
			mutate: func(r *nestedReq) { r.Billing = &nestedAddress{City: "x", Zip: "123456"} },
			want:   []string{"billing.zip: must be at most 5 characters"},
		},
		{
			name:   "slice elements carry their index",
			mutate: func(r *nestedReq) { r.Items = append(r.Items, nestedItem{Qty: 0}, nestedItem{Name: "y", Qty: -1}) },
			want:   []string{"items[1].name: is required", "items[2].qty: must be at least 1"},
		},
		{
			name: "the slice's own rule and its elements both report",
			mutate: func(r *nestedReq) {
				r.Items = []nestedItem{{Name: "a", Qty: 1}, {Name: "b", Qty: 1}, {Name: "c", Qty: 1}, {Qty: 1}}
			},
			want: []string{"items: must be at most 3 items", "items[3].name: is required"},
		},
		{
			name:   "pointer elements are followed and nil ones skipped",
			mutate: func(r *nestedReq) { r.Ptrs = []*nestedItem{nil, {Qty: 1}} },
			want:   []string{"ptrs[1].name: is required"},
		},
		{
			name:   "array elements",
			mutate: func(r *nestedReq) { r.Fixed[0].Name = "" },
			want:   []string{"fixed[0].name: is required"},
		},
		{
			name: "map values report in key order",
			mutate: func(r *nestedReq) {
				r.Attrs = map[string]nestedAttr{"z": {Value: "q"}, "a": {Value: "b"}, "m": {Value: "q"}}
			},
			want: []string{"attrs[m].value: must be one of: a, b", "attrs[z].value: must be one of: a, b"},
		},
		{
			name:   "integer map keys and pointer values",
			mutate: func(r *nestedReq) { r.ByNum = map[int]*nestedAttr{7: {Value: "q"}, 8: nil} },
			want:   []string{"by_num[7].value: must be one of: a, b"},
		},
		{
			name:   "unsigned map keys",
			mutate: func(r *nestedReq) { r.Unsigned = map[uint]nestedAttr{3: {Value: "q"}} },
			want:   []string{"unsigned[3].value: must be one of: a, b"},
		},
		{
			name:   "struct map keys",
			mutate: func(r *nestedReq) { r.Keyed = map[nestedKey]nestedAttr{{K: "k"}: {Value: "q"}} },
			want:   []string{"keyed[{k}].value: must be one of: a, b"},
		},
		{
			name:   "nested slices",
			mutate: func(r *nestedReq) { r.Grid = [][]nestedItem{{{Name: "a", Qty: 1}}, {{Name: "b", Qty: -1}}} },
			want:   []string{"grid[1][0].qty: must be at least 1"},
		},
		{
			name:   "recursive types",
			mutate: func(r *nestedReq) { r.Tree.Children = []nestedTree{{Label: "a", Children: []nestedTree{{}}}} },
			want:   []string{"tree.children[0].children[0].label: is required"},
		},
		{
			name:   "a field promoted through an embedded pointer",
			mutate: func(r *nestedReq) { r.Promoted = "" },
			want:   []string{"promoted: is required"},
		},
		{
			name:   "a nil embedded pointer leaves its required field absent",
			mutate: func(r *nestedReq) { r.nestedEmbedded = nil },
			want:   []string{"promoted: is required"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := validNestedReq()
			tc.mutate(&req)
			assert.Equal(t, tc.want, fieldErrors(t, &req))
		})
	}
}

func TestValidatorPrunesSubtreesWithoutRules(t *testing.T) {
	v := compileValidator(reflect.TypeFor[nestedReq]())
	names := make([]string, 0, len(v.root.fields))
	for _, f := range v.root.fields {
		names = append(names, f.name)
	}
	assert.NotContains(t, names, "quiet", "a slice of rule-free structs is never visited")
	assert.NotContains(t, names, "anything", "a map of interfaces is never visited")
	assert.Contains(t, names, "tree")

	type flat struct {
		A string `json:"a"`
		B []int  `json:"b"`
	}
	assert.False(t, compileValidator(reflect.TypeFor[flat]()).root.live, "a rule-free request skips traversal")
}

func TestValidatePointerSemantics(t *testing.T) {
	type req struct {
		Count *int    `json:"count" validate:"min=1,max=10"`
		Email *string `json:"email" validate:"email"`
		Must  *int    `json:"must" validate:"required"`
		Deep  **int   `json:"deep" validate:"min=1"`
	}
	intPtr := func(n int) *int { return &n }
	strPtr := func(s string) *string { return &s }
	must := intPtr(0)

	tests := []struct {
		name string
		in   req
		want []string
	}{
		{name: "nil pointers skip every rule but required", in: req{}, want: []string{"must: is required"}},
		{name: "a non-nil zero satisfies required", in: req{Must: must}},
		{
			name: "an explicit zero is checked",
			in:   req{Count: intPtr(0), Must: must},
			want: []string{"count: must be at least 1"},
		},
		{
			name: "an explicit empty string is checked",
			in:   req{Email: strPtr(""), Must: must},
			want: []string{"email: must be a valid email address"},
		},
		{name: "in range", in: req{Count: intPtr(5), Email: strPtr("a@b.co"), Must: must}},
		{
			name: "pointers to pointers are followed",
			in:   req{Deep: func() **int { p := intPtr(0); return &p }(), Must: must},
			want: []string{"deep: must be at least 1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, fieldErrors(t, tc.in))
		})
	}
}

func TestValidateOneOfTyped(t *testing.T) {
	type req struct {
		Str   string  `json:"str" validate:"oneof=red green"`
		Int   int     `json:"int" validate:"oneof=1 2 3"`
		Big   int64   `json:"big" validate:"oneof=9007199254740993"`
		Uint  uint8   `json:"uint" validate:"oneof=4 5"`
		Float float64 `json:"float" validate:"oneof=0.5 1.5"`
		Bad   int     `json:"bad" validate:"oneof=1 two"`
		Empty string  `json:"empty" validate:"oneof="`
		Flag  bool    `json:"flag" validate:"oneof=true"`
	}
	valid := req{Str: "red", Int: 2, Big: 9007199254740993, Uint: 5, Float: 1.5, Bad: 7, Empty: "x", Flag: true}
	assert.Nil(t, fieldErrors(t, valid), "values compare as their own type; malformed oneofs are ignored")

	invalid := req{Str: "blue", Int: 4, Big: 9007199254740992, Uint: 6, Float: 2.5}
	assert.Equal(t, []string{
		"str: must be one of: red, green",
		"int: must be one of: 1, 2, 3",
		"big: must be one of: 9007199254740993",
		"uint: must be one of: 4, 5",
		"float: must be one of: 0.5, 1.5",
	}, fieldErrors(t, invalid))
}

func TestValidateFormatRulesApplyOnlyToStrings(t *testing.T) {
	type named string
	type req struct {
		When  time.Time `json:"when" validate:"required,rfc3339"`
		ID    uuid.UUID `json:"id" validate:"uuid"`
		Code  int       `json:"code" validate:"email"`
		Items []string  `json:"items" validate:"e164"`
		Kind  named     `json:"kind" validate:"uuid"`
	}
	in := req{When: time.Now(), ID: uuid.New(), Code: 7, Items: []string{"x"}, Kind: "nope"}
	assert.Equal(t, []string{"kind: must be a valid UUID"}, fieldErrors(t, in),
		"a time.Time with rfc3339 no longer always fails; only the string-kinded field is format-checked")
}

func TestValidateStringLengthCountsRunes(t *testing.T) {
	type req struct {
		Name string `json:"name" validate:"min=2,max=5"`
	}
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "5 runes in 6 bytes is within max", in: "héllo"},
		{name: "2 runes in 4 bytes meets min", in: "ωω"},
		{name: "1 rune in 2 bytes is below min", in: "ω", want: []string{"name: must be at least 2 characters"}},
		{name: "6 runes is above max", in: "ωωωωωω", want: []string{"name: must be at most 5 characters"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, fieldErrors(t, req{Name: tc.in}))
		})
	}
}

func TestValidateRequiredZeroValues(t *testing.T) {
	type inner struct {
		N int `json:"n"`
	}
	type req struct {
		When  time.Time      `json:"when" validate:"required"`
		ID    uuid.UUID      `json:"id" validate:"required"`
		Inner inner          `json:"inner" validate:"required"`
		Any   any            `json:"any" validate:"required"`
		Map   map[string]int `json:"map" validate:"required"`
		Neg   float64        `json:"neg" validate:"required"`
	}
	assert.Equal(t, []string{
		"when: is required", "id: is required", "inner: is required",
		"any: is required", "map: is required", "neg: is required",
	}, fieldErrors(t, req{Map: map[string]int{}}))

	full := req{When: time.Now(), ID: uuid.New(), Inner: inner{N: 1}, Any: 0, Map: map[string]int{"a": 1}, Neg: -1}
	assert.Nil(t, fieldErrors(t, full))
}

func TestValidateParamFieldNames(t *testing.T) {
	type req struct {
		Limit int    `json:"-" query:"limit" validate:"max=10"`
		ID    string `json:"id" path:"pet_id" validate:"required"`
		Body  string `json:"body_field" validate:"required"`
		NoTag string `validate:"required"`
	}
	assert.Equal(t, []string{
		"limit: must be at most 10",
		"pet_id: is required",
		"body_field: is required",
		"NoTag: is required",
	}, fieldErrors(t, req{Limit: 11}))
}

func TestValidateRequestEdgeCases(t *testing.T) {
	t.Run("a nil struct pointer is not validated", func(t *testing.T) {
		type req struct {
			Name string `json:"name" validate:"required"`
		}
		assert.Nil(t, validateRequest((*req)(nil)))
	})

	t.Run("a pointer-receiver Validatable runs through a pointer request", func(t *testing.T) {
		assert.Equal(t, []string{": must be even"}, fieldErrors(t, &evenReq{N: 3}))
		assert.Nil(t, fieldErrors(t, &evenReq{N: 4}))
	})

	t.Run("validators are cached per type", func(t *testing.T) {
		typ := reflect.TypeFor[evenReq]()
		first, second := validatorFor(typ), validatorFor(typ)
		assert.Same(t, first, second)
	})
}

// evenReq implements Validatable on its pointer receiver.
type evenReq struct {
	N int `json:"n"`
}

// errOdd is evenReq's validation failure.
var errOdd = errors.New("must be even")

func (e *evenReq) Validate() error {
	if e.N%2 != 0 {
		return errOdd
	}
	return nil
}

func TestParseRules(t *testing.T) {
	strType := reflect.TypeFor[string]()
	intType := reflect.TypeFor[int]()
	sliceType := reflect.TypeFor[[]int]()
	tests := []struct {
		name  string
		tag   string
		typ   reflect.Type
		kinds []ruleKind
	}{
		{name: "empty tag", tag: "", typ: strType},
		{name: "blank segments and spaces", tag: " required ,, max=3 ", typ: strType, kinds: []ruleKind{ruleRequired, ruleMax}},
		{name: "unknown rule", tag: "dive,required", typ: strType, kinds: []ruleKind{ruleRequired}},
		{name: "non-numeric bound", tag: "min=abc", typ: intType},
		{name: "NaN and Inf bounds", tag: "min=NaN,max=Inf", typ: intType},
		{name: "fractional number bound", tag: "min=0.5", typ: intType, kinds: []ruleKind{ruleMin}},
		{name: "fractional length bound", tag: "min=1.5", typ: strType},
		{name: "negative length bound", tag: "min=-1", typ: sliceType},
		{name: "negative number bound", tag: "min=-1", typ: intType, kinds: []ruleKind{ruleMin}},
		{name: "bound on a bool", tag: "min=1", typ: reflect.TypeFor[bool](), kinds: nil},
		{name: "format on a slice", tag: "uuid,email,e164,rfc3339", typ: sliceType},
		{name: "formats on a string", tag: "uuid,email,e164,rfc3339", typ: strType, kinds: []ruleKind{ruleUUID, ruleEmail, ruleE164, ruleRFC3339}},
		{name: "infinite float oneof", tag: "oneof=1 Inf", typ: reflect.TypeFor[float64]()},
		{name: "negative uint oneof", tag: "oneof=-1", typ: reflect.TypeFor[uint]()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var kinds []ruleKind
			for _, r := range parseRules(tc.tag, tc.typ) {
				kinds = append(kinds, r.kind)
			}
			assert.Equal(t, tc.kinds, kinds)
		})
	}

	t.Run("bounds render canonically in messages", func(t *testing.T) {
		rules := parseRules("min=1e1,max=2.50", intType)
		require.Len(t, rules, 2)
		assert.Equal(t, "must be at least 10", rules[0].msg)
		assert.Equal(t, "must be at most 2.5", rules[1].msg)
	})
}

func TestShapeOf(t *testing.T) {
	tests := []struct {
		typ  reflect.Type
		want valueShape
	}{
		{reflect.TypeFor[*string](), shapeString},
		{reflect.TypeFor[int8](), shapeInt},
		{reflect.TypeFor[uintptr](), shapeUint},
		{reflect.TypeFor[float32](), shapeFloat},
		{reflect.TypeFor[[2]int](), shapeItems},
		{reflect.TypeFor[map[string]int](), shapeProps},
		{reflect.TypeFor[struct{}](), shapeOther},
	}
	for _, tc := range tests {
		t.Run(tc.typ.String(), func(t *testing.T) {
			assert.Equal(t, tc.want, shapeOf(tc.typ))
		})
	}
}

// e164Regexp is the schema's E.164 pattern, compiled to cross-check isE164.
var e164Regexp = regexp.MustCompile(e164Pattern)

func TestIsE164MatchesPattern(t *testing.T) {
	for _, s := range []string{
		"", "+", "+1", "+12", "+0123", "+13055551234", "+123456789012345", "+1234567890123456",
		"13055551234", "+1305555123a", "+١٢٣", " +123", "+123 ",
	} {
		t.Run(s, func(t *testing.T) {
			assert.Equal(t, e164Regexp.MatchString(s), isE164(s))
		})
	}
}

// FuzzIsE164 proves the hand-written E.164 check accepts exactly the strings
// the documented schema pattern accepts.
func FuzzIsE164(f *testing.F) {
	for _, seed := range []string{"+13055551234", "+0", "+1", "+123456789012345", "", "+12a"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if e164Regexp.MatchString(s) != isE164(s) {
			t.Fatalf("isE164(%q) = %v, pattern says %v", s, isE164(s), !isE164(s))
		}
	})
}

func TestMapKeyString(t *testing.T) {
	type key struct{ A int }
	tests := []struct {
		key  any
		want string
	}{
		{"s", "s"},
		{int8(-3), "-3"},
		{uint16(4), "4"},
		{key{A: 1}, "{1}"},
		{uuid.MustParse("01900000-0000-7000-8000-000000000001"), "01900000-0000-7000-8000-000000000001"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, mapKeyString(reflect.ValueOf(tc.key)))
		})
	}
}

func TestIsEmptyValue(t *testing.T) {
	var nilPtr *int
	tests := []struct {
		name string
		v    any
		want bool
	}{
		{"empty string", "", true},
		{"string", "x", false},
		{"nil slice", []int(nil), true},
		{"empty map", map[string]int{}, true},
		{"negative zero float", math.Copysign(0, -1), true},
		{"float", 0.1, false},
		{"false", false, true},
		{"zero uint", uint(0), true},
		{"nil pointer", nilPtr, true},
		{"zero array", [2]int{}, true},
		{"array", [2]int{0, 1}, false},
		{"zero struct", time.Time{}, true},
		{"struct", time.Unix(1, 0), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isEmptyValue(reflect.ValueOf(tc.v)))
		})
	}
}

// BenchmarkValidateRequest_Nested measures validating a request holding a
// slice of ten structs, each with its own rules — the recursive path.
func BenchmarkValidateRequest_Nested(b *testing.B) {
	type line struct {
		SKU string `json:"sku" validate:"required,max=32"`
		Qty int    `json:"qty" validate:"min=1,max=99"`
	}
	type order struct {
		Customer string `json:"customer" validate:"required"`
		Lines    []line `json:"lines" validate:"min=1,max=50"`
	}
	v := order{Customer: "c_1"}
	for i := range 10 {
		v.Lines = append(v.Lines, line{SKU: "sku-" + strings.Repeat("x", i), Qty: i + 1})
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := validateRequest(&v); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
