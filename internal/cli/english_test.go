package cli

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/i18n"
)

// Luồng CLI và hook trên repo `locale: en`: chữ CLI/hook in ra không còn ký tự tiếng Việt. Không t.Parallel: locale
// mặc định là của cả tiến trình.

var vietnameseRe = regexp.MustCompile(`(?i)[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)

// noVietnamese đòi s không có ký tự tiếng Việt; báo cả dòng chứa ký tự đầu tiên.
func noVietnamese(t *testing.T, label, s string) {
	t.Helper()
	for _, line := range strings.Split(s, "\n") {
		if vietnameseRe.MatchString(line) {
			t.Errorf("%s: còn tiếng Việt: %q\n--- toàn bộ ---\n%s", label, line, s)
			return
		}
	}
}

// enRepo là repo mới, setup bằng `setup --locale en` (chưa có config: chỉ cờ quyết định locale).
func enRepo(t *testing.T) *testRepo {
	t.Helper()
	restoreLocale(t)
	r := newTestRepo(t)
	eq(t, r.run("setup", "--locale", "en"), 0)
	noVietnamese(t, "setup --locale en", r.out+r.err)
	contains(t, r.out, "Created aisdlc/config.yaml")
	contains(t, r.read("aisdlc/config.yaml"), "locale: en")
	return r
}

func TestEnglishCLIFlow(t *testing.T) {
	r := enRepo(t)
	step := func(code int, args ...string) {
		t.Helper()
		eq(t, r.run(args...), code)
		noVietnamese(t, strings.Join(args, " "), r.out+r.err)
	}

	step(0, "init", "PAY-1")
	contains(t, r.out, "Created feature PAY-1", "Next: write brd.md")
	step(0, "status")
	contains(t, r.out, "Locale: en", "Feature PAY-1", "Gates:", "Next gate to handle: BRD")
	step(0, "status", "--json")

	// help, lệnh lạ, cờ lạ, sai số đối số: chữ của cobra theo locale
	step(0, "--help")
	contains(t, r.out, "Usage:", "aisdlc [flags]", "Available Commands:", "Flags:", "help for aisdlc",
		"AISDLC process: BRD -> Design")
	step(0, "step", "--help")
	contains(t, r.out, "Work step by step Sx")
	step(0, "help", "submit")
	step(2, "bogus")
	contains(t, r.err, `unknown command "bogus" for "aisdlc"`, "Usage:")
	step(2, "status", "--nope")
	contains(t, r.err, "unknown flag: --nope")
	step(2, "use")
	contains(t, r.err, "accepts 1 arg(s), received 0")

	// cổng chưa nộp; cổng sai tên
	eq(t, r.runC(typed("PAY-1"), "approve", "brd"), 2)
	noVietnamese(t, "approve brd", r.out+r.err)
	contains(t, r.err, "only be approved when AWAITING_APPROVAL")
	step(2, "submit", "nope")

	// BRD còn là template: phát hiện của validator theo locale
	step(1, "submit", "brd")
	contains(t, r.out, "BRD_EMPTY_SECTION", "Fix them, then run `aisdlc submit brd` again.")

	// BRD hợp lệ: nộp được; duyệt không có console (agent) bị từ chối
	r.write("aisdlc/features/PAY-1/brd.md", validBRD)
	step(0, "submit", "brd")
	contains(t, r.out, "Submitted the BRD gate, no findings.")
	// stdout là review card (internal/review, đợt 3): chỉ xét stderr của CLI
	eq(t, r.run("approve", "brd"), 3)
	noVietnamese(t, "approve brd (stderr)", r.err)
	contains(t, r.err, "Only the dev may run this command, in a real terminal")

	// chưa qua cổng thiết kế: step start bị từ chối
	step(2, "step", "start", "S1")
}

func TestEnglishHooks(t *testing.T) {
	r := enRepo(t)
	r.ok(noConsole(), "init", "PAY-1")
	hookOut := func(event, stdin string) string {
		t.Helper()
		r.hook(event, stdin)
		noVietnamese(t, "hook "+event, r.out+r.err)
		return r.out
	}

	// SessionStart: quy tắc và trạng thái
	o := hookSpecific(t, hookOut("session-start", hookIn("SessionStart", r.root, "", "", "")))
	contains(t, str(o, "additionalContext"), "AISDLC rules:", "Status: feature PAY-1")

	// PreToolUse: ghi code khi cổng chưa duyệt, sửa file trạng thái, sửa config, Bash ghi file được bảo vệ
	o = hookSpecific(t, hookOut("pre-tool-use", r.codeEdit()))
	eq(t, str(o, "permissionDecision"), "deny")
	contains(t, str(o, "permissionDecisionReason"), "Writing code not allowed yet: the BRD gate is not approved")
	o = hookSpecific(t, hookOut("pre-tool-use", hookIn("PreToolUse", r.root, "Write", "file_path",
		filepath.Join(r.root, "aisdlc/features/PAY-1/gates.json"))))
	contains(t, str(o, "permissionDecisionReason"), "State file managed by the CLI")
	o = hookSpecific(t, hookOut("pre-tool-use", hookIn("PreToolUse", r.root, "Edit", "file_path",
		filepath.Join(r.root, "aisdlc/config.yaml"))))
	contains(t, str(o, "permissionDecisionReason"), "is edited only by the dev")
	o = hookSpecific(t, hookOut("pre-tool-use", hookIn("PreToolUse", r.root, "Bash", "command",
		"echo x > aisdlc/features/PAY-1/steps.json")))
	contains(t, str(o, "permissionDecisionReason"), "The command seems to write a protected file")
	o = hookSpecific(t, hookOut("pre-tool-use", hookIn("PreToolUse", r.root, "Bash", "command", "aisdlc approve brd")))
	contains(t, str(o, "permissionDecisionReason"), "Only the dev may run this command")
}

func TestEnglishOutOfScopeAndStop(t *testing.T) {
	restoreLocale(t)
	r := newTestRepo(t)
	r.env["AISDLC_LOCALE"] = "en" // chưa có config: setup theo AISDLC_LOCALE
	r.casesApproved()
	eq(t, i18n.Default(), i18n.EN)
	contains(t, r.read("aisdlc/config.yaml"), "locale: en")

	r.ok(noConsole(), "step", "start", "S1")
	contains(t, r.out, "Starting step S1", "Edit only files in scope.")
	noVietnamese(t, "step start", r.out+r.err)

	// ngoài phạm vi bước
	r.hook("pre-tool-use", hookIn("PreToolUse", r.root, "Write", "file_path", filepath.Join(r.root, "internal/other/nope.go")))
	noVietnamese(t, "hook out of scope", r.out+r.err)
	o := hookSpecific(t, r.out)
	contains(t, str(o, "permissionDecisionReason"), "Outside the scope of S1", "aisdlc scope allow internal/other/nope.go")

	// Stop giữa bước
	r.hook("stop", hookIn("Stop", r.root, "", "", ""))
	noVietnamese(t, "hook stop", r.out+r.err)
	var stop map[string]any
	must(t, json.Unmarshal([]byte(r.out), &stop))
	eq(t, str(stop, "decision"), "block")
	contains(t, str(stop, "reason"), "Step S1 is not finished. Run `aisdlc step finish S1`")
}
