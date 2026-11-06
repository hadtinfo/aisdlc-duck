package cli

import (
	"errors"
	"regexp"

	"github.com/spf13/cobra"
)

// viErrorRules dịch thông báo lỗi cú pháp của cobra v1.10 và pflag v1.0 (lệnh lạ, cờ lạ, cờ thiếu giá trị, giá trị cờ
// sai, sai số đối số, thiếu cờ bắt buộc) sang tiếng Việt. Thông báo không khớp mẫu nào giữ nguyên.
var viErrorRules = []struct {
	re  *regexp.Regexp
	rep string
}{
	{regexp.MustCompile(`unknown command ("[^"]*") for ("[^"]*")`), `lệnh lạ $1 của $2`},
	{regexp.MustCompile(`\n\nDid you mean this\?\n`), "\n\nCó phải ý bạn là:\n"},
	{regexp.MustCompile(`^unknown flag: (.*)$`), `cờ lạ: $1`},
	{regexp.MustCompile(`^unknown shorthand flag: (.*) in (.*)$`), `cờ ngắn lạ: $1 trong $2`},
	{regexp.MustCompile(`^flag needs an argument: (.*) in (.*)$`), `cờ cần giá trị: $1 trong $2`},
	{regexp.MustCompile(`^flag needs an argument: (.*)$`), `cờ cần giá trị: $1`},
	{regexp.MustCompile(`^invalid argument ("[^"]*") for ("[^"]*") flag: `), `giá trị $1 không hợp lệ cho cờ $2: `},
	{regexp.MustCompile(`^bad flag syntax: (.*)$`), `cú pháp cờ sai: $1`},
	{regexp.MustCompile(`^required flag\(s\) (.*) not set$`), `thiếu cờ bắt buộc $1`},
	{regexp.MustCompile(`^accepts 0 arg\(s\), received (\d+)$`), `không nhận đối số, nhận được $1`},
	{regexp.MustCompile(`^accepts (\d+) arg\(s\), received (\d+)$`), `cần đúng $1 đối số, nhận được $2`},
	{regexp.MustCompile(`^requires at least (\d+) arg\(s\), only received (\d+)$`), `cần ít nhất $1 đối số, nhận được $2`},
	{regexp.MustCompile(`^accepts at most (\d+) arg\(s\), received (\d+)$`), `nhận nhiều nhất $1 đối số, nhận được $2`},
	{regexp.MustCompile(`^accepts between (\d+) and (\d+) arg\(s\), received (\d+)$`), `cần từ $1 đến $2 đối số, nhận được $3`},
}

// viCobraError là thông báo lỗi cú pháp của cobra/pflag bằng tiếng Việt.
func viCobraError(msg string) string {
	for _, r := range viErrorRules {
		msg = r.re.ReplaceAllString(msg, r.rep)
	}
	return msg
}

type viError struct{ err error }

func (e *viError) Error() string { return viCobraError(e.err.Error()) }
func (e *viError) Unwrap() error { return e.err }

func viErr(err error) error {
	var v *viError
	if err == nil || errors.As(err, &v) {
		return err
	}
	return &viError{err}
}

// localizeErrors đặt lỗi cú pháp tiếng Việt cho root và mọi lệnh con (gọi sau khi đã thêm hết lệnh): lỗi cờ qua
// SetFlagErrorFunc (lệnh con thừa hưởng của root), lỗi số đối số bằng cách bọc validator Args của từng lệnh. Lệnh lạ
// dưới một lệnh nhóm (Args nil: cobra báo ngay trong Find) và thiếu cờ bắt buộc được dịch ở execute.
func localizeErrors(root *cobra.Command) {
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return viErr(err) })
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if args := c.Args; args != nil {
			c.Args = func(cmd *cobra.Command, a []string) error { return viErr(args(cmd, a)) }
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(root)
}
