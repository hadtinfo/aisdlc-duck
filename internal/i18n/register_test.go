package i18n

import "testing"

func TestRegisterMergesAndKeepsIdentical(t *testing.T) {
	const k = "khóa thử register"
	t.Cleanup(func() { delete(en, k) })
	register(map[string]string{k: "register test key"})
	register(map[string]string{k: "register test key"}) // trùng nguyên văn: không lỗi
	if got := EN.T(k); got != "register test key" {
		t.Fatalf("En.T = %q", got)
	}
}

func TestRegisterConflictPanics(t *testing.T) {
	const k = "khóa thử xung đột"
	t.Cleanup(func() { delete(en, k) })
	register(map[string]string{k: "first"})
	defer func() {
		if recover() == nil {
			t.Fatal("register không panic khi cùng khóa khác bản dịch")
		}
	}()
	register(map[string]string{k: "second"})
}
