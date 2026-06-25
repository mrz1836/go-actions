package actions

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestNewList(t *testing.T) {
	t.Parallel()

	t.Run("sets total to item count", func(t *testing.T) {
		t.Parallel()
		got := NewList([]string{"a", "b", "c"})
		if got.Meta.Total != 3 {
			t.Fatalf("Total = %d, want 3", got.Meta.Total)
		}
		if len(got.Items) != 3 {
			t.Fatalf("Items = %v", got.Items)
		}
	})

	t.Run("normalizes a nil slice to an empty slice", func(t *testing.T) {
		t.Parallel()
		got := NewList[string](nil)
		if got.Items == nil {
			t.Fatal("Items is nil, want non-nil empty slice")
		}
		if got.Meta.Total != 0 {
			t.Fatalf("Total = %d, want 0", got.Meta.Total)
		}
	})
}

func TestListEncodesAsItemsAndMeta(t *testing.T) {
	t.Parallel()
	type item struct {
		Name string `json:"name"`
	}
	w := httptest.NewRecorder()
	encodeResponse(w, NewList([]item{{Name: "court-1"}}))

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got struct {
		Items []item `json:"items"`
		Meta  struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Meta.Total != 1 || len(got.Items) != 1 || got.Items[0].Name != "court-1" {
		t.Fatalf("decoded = %+v", got)
	}
}

// TestListNilItemsEncodesEmptyArray proves a NewList-wrapped nil slice serializes
// as "items": [] rather than null.
func TestListNilItemsEncodesEmptyArray(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	encodeResponse(w, NewList[string](nil))
	if body := w.Body.String(); body != `{"items":[],"meta":{"total":0}}` {
		t.Fatalf("body = %s", body)
	}
}
