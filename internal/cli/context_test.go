package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

// probe là lệnh chỉ có trong test: phân giải tính năng như mọi lệnh rồi đòi mạng ở chế độ web.
func netProbe(ctx *Context) *cobra.Command {
	var fo FeatureOption
	c := &cobra.Command{Use: "net-probe", Args: cobra.NoArgs}
	fo.AddTo(c)
	c.RunE = Wrap(ctx, func(cmd *cobra.Command, args []string) (int, error) {
		f, err := fo.Feature(ctx)
		if err != nil {
			return 0, err
		}
		if err := ctx.WebNetwork(f); err != nil {
			return 0, err
		}
		ctx.Println("đã gọi mạng")
		return ExitOK, nil
	})
	return c
}

func TestWebModeNetworkCommandAborts(t *testing.T) {
	r := newTestRepo(t)
	r.extra = append(r.extra, netProbe)
	r.ok(noConsole(), "setup", "--approvals", "web")
	r.ok(noConsole(), "init", "PAY-1")

	eq(t, r.run("net-probe"), ExitUsage)
	contains(t, r.err, "Chế độ duyệt trên web chưa có trong bản Go (giai đoạn 4).",
		"Chưa `aisdlc web login` trên máy này")
	notContains(t, r.err, "Lỗi:")
	notContains(t, r.out, "đã gọi mạng")

	// cùng lệnh ở chế độ local: chạy bình thường
	r.write("aisdlc/config.yaml", "language: go\n")
	eq(t, r.run("net-probe"), ExitOK)
	contains(t, r.out, "đã gọi mạng")
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

func TestDefaultRunnerNeedsHarness(t *testing.T) {
	_, err := defaultRunner("/x").RunCases("PAY-1", nil, "cases")
	var a *Abort
	if !asAbort(err, &a) || a.Code != 2 || a.Msg != "Chạy case cần harness Go (giai đoạn 3)." {
		t.Fatalf("got %v", err)
	}
}
