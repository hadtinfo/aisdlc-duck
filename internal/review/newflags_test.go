package review

import (
	"reflect"
	"testing"
)

// So thẻ nộp lại: cờ so theo code; chỉ đổi thông điệp (đổi locale) không phải cờ mới.
func TestNewFlagsComparesByCode(t *testing.T) {
	prev := []ReviewFlag{NewFlag("SCOPE_TOO_WIDE", WARN, "S1: scope quá rộng", "", nil)}
	cur := []ReviewFlag{
		NewFlag("SCOPE_TOO_WIDE", WARN, "S1: scope too wide", "", nil),
		NewFlag("BUDGET_HIGH", WARN, "S1: budget", "", nil),
	}
	got := NewFlags(prev, cur)
	if len(got) != 1 || got[0].Code != "BUDGET_HIGH" {
		t.Errorf("NewFlags = %+v", got)
	}
	if got := NewFlags(cur, cur); !reflect.DeepEqual(got, []ReviewFlag(nil)) {
		t.Errorf("không đổi gì mà có cờ mới: %+v", got)
	}
}
