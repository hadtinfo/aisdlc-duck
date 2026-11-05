// Package cli là CLI aisdlc (cobra): lệnh, hook và phần đọc cache web.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Mã thoát, như Main.java.
const (
	ExitOK       = 0
	ExitFindings = 1
	ExitUsage    = 2
	ExitNotDev   = 3
)

// Version là chuỗi `aisdlc --version`.
const Version = "aisdlc 0.1.0-SNAPSHOT"

// Main chạy CLI với stdin/stdout/stderr, console và thư mục hiện tại của tiến trình rồi thoát.
func Main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Lỗi: "+err.Error())
		os.Exit(ExitUsage)
	}
	os.Exit(Run(NewContext(os.Stdin, os.Stdout, os.Stderr, SystemConsole(), cwd), os.Args[1:]...))
}

// Run chạy một lệnh trong tiến trình và trả mã thoát.
func Run(ctx *Context, args ...string) int {
	return runWith(ctx, nil, args...)
}

// runWith như Run, thêm các lệnh extra vào cây (test dùng). Panic thành "Lỗi: <giá trị>" và exit 2 (hook vẫn chặn,
// fail closed).
func runWith(ctx *Context, extra []func(*Context) *cobra.Command, args ...string) (code int) {
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(ctx.err, "Lỗi: %v\n", p)
			code = ExitUsage
		}
	}()
	if args == nil {
		args = []string{} // nil thì cobra đọc os.Args
	}
	ctx.withArgs(args)
	root := newRoot(ctx)
	for _, f := range extra {
		root.AddCommand(f(ctx))
	}
	return execute(ctx, root, args)
}

func newRoot(ctx *Context) *cobra.Command {
	root := &cobra.Command{
		Use:           "aisdlc",
		Short:         "Quy trình AISDLC: BRD -> Thiết kế -> Case -> Code từng bước -> Review.",
		Version:       Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
			fmt.Fprint(ctx.err, cmd.UsageString())
			return ExitUsage, nil
		}),
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetVersionTemplate("{{.Version}}\n")
	root.Flags().BoolP("version", "V", false, "In phiên bản rồi thoát.")
	root.AddCommand(newSetupCmd(ctx), newInitCmd(ctx), newUseCmd(ctx), newStatusCmd(ctx),
		newSubmitCmd(ctx), newApproveCmd(ctx), newRejectCmd(ctx), newReopenCmd(ctx), newReviewCmd(ctx))
	return root
}

// runError bọc lỗi của phần thân lệnh để phân biệt với lỗi cú pháp (cờ lạ, thiếu đối số) của cobra.
type runError struct{ err error }

func (e *runError) Error() string { return e.err.Error() }
func (e *runError) Unwrap() error { return e.err }

// Wrap biến thân lệnh (trả mã thoát) thành RunE của cobra; mã thoát về Context, lỗi được bọc để Run in đúng kiểu.
func Wrap(ctx *Context, fn func(cmd *cobra.Command, args []string) (int, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		code, err := fn(cmd, args)
		if err != nil {
			return &runError{err}
		}
		ctx.exit = code
		return nil
	}
}

func execute(ctx *Context, root *cobra.Command, args []string) int {
	root.SetArgs(args)
	root.SetIn(ctx.in)
	root.SetOut(ctx.out)
	root.SetErr(ctx.err)
	ctx.exit = ExitOK
	cmd, err := root.ExecuteC()
	if err == nil {
		return ctx.exit
	}
	var re *runError
	if errors.As(err, &re) {
		return ctx.report(re.err)
	}
	// lỗi dùng lệnh (picocli: in lỗi kèm cách dùng ra stderr, exit 2)
	fmt.Fprintln(ctx.err, err.Error())
	if cmd != nil {
		fmt.Fprint(ctx.err, cmd.UsageString())
	}
	return ExitUsage
}

// report in lỗi của lệnh như execution handler của Main.java: Abort in nguyên thông báo với mã của nó, lỗi khác
// in "Lỗi: <msg>" và exit 2.
func (c *Context) report(err error) int {
	var a *Abort
	if errors.As(err, &a) {
		fmt.Fprintln(c.err, a.Msg)
		return a.Code
	}
	fmt.Fprintln(c.err, "Lỗi: "+err.Error())
	return ExitUsage
}
