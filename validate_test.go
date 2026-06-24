package actions

import "testing"

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
}
