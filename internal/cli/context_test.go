package cli

import (
	"strings"
	"testing"
)

// ContextColorTest

func TestNoConsoleMeansNoColor(t *testing.T) {
	eq(t, colorDefault(false, map[string]string{"TERM": "xterm"}), false)
}

func TestEnvDecides(t *testing.T) {
	eq(t, colorAllowed(map[string]string{"TERM": "xterm-256color"}), true)
	eq(t, colorAllowed(map[string]string{"TERM": "xterm", "NO_COLOR": "1"}), false)
	eq(t, colorAllowed(map[string]string{"TERM": "dumb"}), false)
	eq(t, colorAllowed(map[string]string{}), false)
}

// Lỗi không lường trước (không phải Abort): in "Lỗi: <msg>" ra stderr và exit 2, như execution handler của Main.java.
func TestUnknownErrorPrintsLoiAndExit2(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.write("aisdlc/features/PAY-1/feature.yaml", "key: [không phải key\n")
	eq(t, r.run("use", "PAY-1"), ExitUsage)
	if !strings.HasPrefix(r.err, "Lỗi: ") {
		t.Fatalf("stderr phải bắt đầu bằng \"Lỗi: \":\n%s", r.err)
	}
	contains(t, r.err, "Lỗi: ", "feature.yaml")
}

func TestNoArgsPrintsUsageExit2(t *testing.T) {
	r := newTestRepo(t)
	eq(t, r.run(), ExitUsage)
	contains(t, r.err, "setup", "status")
}

func TestUnknownFlagExit2(t *testing.T) {
	r := newTestRepo(t)
	eq(t, r.run("status", "--nope"), ExitUsage)
	contains(t, r.err, "--nope")
}

func TestHandlesSignalsExceptHooks(t *testing.T) {
	eq(t, handlesSignals([]string{"eval"}), true)
	eq(t, handlesSignals([]string{"step", "finish", "S1"}), true)
	eq(t, handlesSignals(nil), true)
	eq(t, handlesSignals([]string{"hook", "pre-tool-use"}), false)
}

// session_id cắt còn tối đa 100 đơn vị UTF-16 (server đếm như Java), không cắt giữa ký tự ngoài BMP.
func TestTruncSessionCountsUTF16Units(t *testing.T) {
	utf16Len := func(s string) int {
		n := 0
		for _, r := range s {
			n++
			if r >= 0x10000 {
				n++
			}
		}
		return n
	}
	eq(t, truncSession("  "), "")
	eq(t, truncSession("abc"), "abc")
	eq(t, truncSession(strings.Repeat("a", 150)), strings.Repeat("a", 100))
	emoji := truncSession(strings.Repeat("😀", 60)) // 120 đơn vị UTF-16
	eq(t, emoji, strings.Repeat("😀", 50))
	odd := truncSession("a" + strings.Repeat("😀", 60)) // 1 + 2*49 = 99; ký tự kế tiếp (2 đơn vị) vượt 100
	eq(t, utf16Len(odd), 99)
	eq(t, utf16Len(truncSession(strings.Repeat("é", 120))), 100)
}
