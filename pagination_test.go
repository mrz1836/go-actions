package actions

import (
	"strconv"
	"testing"
)

func TestClampLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int
		def   int
		max   int
		want  int
	}{
		{"zero returns default", 0, 25, 100, 25},
		{"negative returns default", -3, 25, 100, 25},
		{"within range unchanged", 40, 25, 100, 40},
		{"above max clamps", 500, 25, 100, 100},
		{"at max unchanged", 100, 25, 100, 100},
		{"at one unchanged", 1, 25, 100, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ClampLimit(tt.limit, tt.def, tt.max); got != tt.want {
				t.Fatalf("ClampLimit(%d, %d, %d) = %d, want %d", tt.limit, tt.def, tt.max, got, tt.want)
			}
		})
	}
}

// cursorOfStr is a trivial cursor function for the pagination tests: it renders
// each row's own value as the cursor so assertions can pin the derived cursor.
func cursorOfStr(v int) string { return "cur-" + strconv.Itoa(v) }

//nolint:gocognit // Test function with multiple sub-tests
func TestPaginate(t *testing.T) {
	t.Parallel()

	t.Run("fewer than limit has no next page", func(t *testing.T) {
		t.Parallel()

		got := Paginate([]int{1, 2}, 3, cursorOfStr)
		if got.HasMore {
			t.Fatalf("HasMore = true, want false")
		}
		if got.NextCursor != "" {
			t.Fatalf("NextCursor = %q, want empty", got.NextCursor)
		}
		if len(got.Items) != 2 {
			t.Fatalf("Items = %v, want 2 items", got.Items)
		}
	})

	t.Run("exactly limit has no next page", func(t *testing.T) {
		t.Parallel()

		got := Paginate([]int{1, 2, 3}, 3, cursorOfStr)
		if got.HasMore {
			t.Fatalf("HasMore = true, want false")
		}
		if len(got.Items) != 3 {
			t.Fatalf("Items = %v, want 3 items", got.Items)
		}
	})

	t.Run("one extra row signals another page and trims it", func(t *testing.T) {
		t.Parallel()

		got := Paginate([]int{1, 2, 3, 4}, 3, cursorOfStr)
		if !got.HasMore {
			t.Fatalf("HasMore = false, want true")
		}
		if len(got.Items) != 3 {
			t.Fatalf("Items = %v, want 3 items", got.Items)
		}
		if got.NextCursor != "cur-3" {
			t.Fatalf("NextCursor = %q, want the last kept row's cursor cur-3", got.NextCursor)
		}
	})

	t.Run("nil rows normalize to empty items", func(t *testing.T) {
		t.Parallel()

		got := Paginate(nil, 3, cursorOfStr)
		if got.Items == nil {
			t.Fatal("Items is nil, want non-nil empty slice")
		}
		if len(got.Items) != 0 || got.HasMore {
			t.Fatalf("got %+v, want empty single page", got)
		}
	})

	t.Run("non-positive limit disables trimming", func(t *testing.T) {
		t.Parallel()

		got := Paginate([]int{1, 2, 3}, 0, cursorOfStr)
		if got.HasMore {
			t.Fatalf("HasMore = true, want false for limit<=0")
		}
		if len(got.Items) != 3 {
			t.Fatalf("Items = %v, want all 3 rows", got.Items)
		}
	})
}

// BenchmarkClampLimit measures the branch-only clamp on the default, in-range,
// and over-max paths.
func BenchmarkClampLimit(b *testing.B) {
	for _, limit := range []int{0, 40, 500} {
		b.Run(strconv.Itoa(limit), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = ClampLimit(limit, 25, 100)
			}
		})
	}
}

// BenchmarkPaginate covers the two shapes a list endpoint hits: a partial final
// page (no trim, no cursor) and a full page with an extra row (trim + derive
// cursor).
func BenchmarkPaginate(b *testing.B) {
	full := make([]int, 26) // limit+1 → triggers HasMore + cursor derivation
	for i := range full {
		full[i] = i
	}
	partial := full[:20] // fewer than limit → single page

	b.Run("has_more", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = Paginate(full, 25, cursorOfStr)
		}
	})
	b.Run("single_page", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = Paginate(partial, 25, cursorOfStr)
		}
	})
}
