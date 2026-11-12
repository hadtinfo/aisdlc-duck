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

// Cùng mã nhưng chỉ sang chỗ khác hoặc nâng mức là cờ mới.
func TestNewFlagsSameCodeDifferentRefsOrLevel(t *testing.T) {
	prev := []ReviewFlag{
		NewFlag("SCOPE_OVERLAP", WARN, "S1/S2 chồng phạm vi", "", []string{"S1", "S2"}),
		NewFlag("DB_TOUCHES_EXISTING", WARN, "bảng orders", "", []string{"orders"}),
	}
	cur := []ReviewFlag{
		NewFlag("SCOPE_OVERLAP", WARN, "S1/S2 scopes overlap", "", []string{"S1", "S2"}),
		NewFlag("SCOPE_OVERLAP", WARN, "S2/S3 scopes overlap", "", []string{"S2", "S3"}),
		NewFlag("DB_TOUCHES_EXISTING", HIGH, "table orders", "", []string{"orders"}),
	}
	got := NewFlags(prev, cur)
	if len(got) != 2 || got[0].Refs[1] != "S3" || got[1].Level != HIGH {
		t.Errorf("NewFlags = %+v", got)
	}
}
