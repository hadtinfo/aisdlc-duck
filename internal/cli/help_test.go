package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Help và cách dùng của mọi lệnh bằng tiếng Việt: không còn chuỗi tiếng Anh mặc định của cobra.
func TestHelpIsVietnamese(t *testing.T) {
	english := []string{"Usage:", "Flags:", "Global Flags:", "Available Commands:", "Additional Commands:", "Aliases:",
		"Examples:", "help for", "Help about any command", "for more information", "[flags]", "[command]"}
	check := func(name, s string, want ...string) {
		t.Helper()
		for _, e := range english {
			if strings.Contains(s, e) {
				t.Errorf("%s còn %q:\n%s", name, e, s)
			}
		}
		for _, w := range want {
			if !strings.Contains(s, w) {
				t.Errorf("%s thiếu %q:\n%s", name, w, s)
			}
		}
	}
	run := func(args ...string) (string, string, int) {
		o, e := &strings.Builder{}, &strings.Builder{}
		ctx := NewContext(strings.NewReader(""), o, e, noConsole(), t.TempDir())
		code := Run(ctx, args...)
		return o.String(), e.String(), code
	}
	out, _, code := run("--help")
	eq(t, code, ExitOK)
	check("aisdlc --help", out, "Cách dùng:", "aisdlc [cờ]", "aisdlc [lệnh]", "Lệnh:", "Cờ:", "trợ giúp cho aisdlc",
		"In trợ giúp của một lệnh", `Chạy "aisdlc [lệnh] --help" để xem chi tiết một lệnh.`)
	out, _, _ = run("help")
	check("aisdlc help", out, "Cách dùng:")
	out, _, _ = run("help", "step")
	check("aisdlc help step", out, "Cách dùng:", "aisdlc step [lệnh]")
	out, _, _ = run("help", "khong-co")
	check("aisdlc help khong-co", out, "Không có chủ đề trợ giúp")
	_, errOut, code := run("--khong-co")
	eq(t, code, ExitUsage)
	check("aisdlc --khong-co", errOut, "Cách dùng:")
	_, errOut, _ = run()
	check("aisdlc", errOut, "Cách dùng:")

	// mọi lệnh con, mọi cấp
	root := newRoot(NewContext(strings.NewReader(""), &strings.Builder{}, &strings.Builder{}, noConsole(), t.TempDir()))
	var paths [][]string
	var walk func(prefix []string, cs []*cobraCmd)
	walk = func(prefix []string, cs []*cobraCmd) {
		for _, c := range cs {
			p := append(append([]string{}, prefix...), c.Name())
			if !c.DisableFlagParsing { // lệnh nền (web flush) không phân tích cờ, không in gì
				paths = append(paths, p)
			}
			walk(p, c.Commands())
		}
	}
	walk(nil, root.Commands())
	if len(paths) < 20 {
		t.Fatalf("ít lệnh: %v", paths)
	}
	for _, p := range paths {
		out, _, _ := run(append(p, "--help")...)
		check("aisdlc "+strings.Join(p, " ")+" --help", out, "Cách dùng:", "trợ giúp cho "+p[len(p)-1])
	}
}

type cobraCmd = cobra.Command
