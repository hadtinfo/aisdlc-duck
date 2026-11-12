package cli

import (
	"errors"
	"regexp"

	"github.com/spf13/cobra"

	"aisdlc/internal/i18n"
)

// viErrorRules dịch thông báo lỗi cú pháp của cobra v1.10 và pflag v1.0 (lệnh lạ, cờ lạ, cờ thiếu giá trị, giá trị cờ
// sai, sai số đối số, thiếu cờ bắt buộc) theo locale: tiếng Việt, hoặc giữ nguyên câu gốc tiếng Anh (bản dịch en của
// từng mẫu là chính câu của cobra/pflag). Thông báo không khớp mẫu nào giữ nguyên.
var viErrorRules = []*regexp.Regexp{
	regexp.MustCompile(`unknown command ("[^"]*") for ("[^"]*")`),
	regexp.MustCompile(`\n\nDid you mean this\?\n`),
	regexp.MustCompile(`^unknown flag: (.*)$`),
	regexp.MustCompile(`^unknown shorthand flag: (.*) in (.*)$`),
	regexp.MustCompile(`^flag needs an argument: (.*) in (.*)$`),
	regexp.MustCompile(`^flag needs an argument: (.*)$`),
	regexp.MustCompile(`^invalid argument ("[^"]*") for ("[^"]*") flag: `),
	regexp.MustCompile(`^bad flag syntax: (.*)$`),
	regexp.MustCompile(`^required flag\(s\) (.*) not set$`),
	regexp.MustCompile(`^accepts 0 arg\(s\), received (\d+)$`),
	regexp.MustCompile(`^accepts (\d+) arg\(s\), received (\d+)$`),
	regexp.MustCompile(`^requires at least (\d+) arg\(s\), only received (\d+)$`),
	regexp.MustCompile(`^accepts at most (\d+) arg\(s\), received (\d+)$`),
	regexp.MustCompile(`^accepts between (\d+) and (\d+) arg\(s\), received (\d+)$`),
}

// viErrorReps là chuỗi thay thế của từng mẫu trong viErrorRules (cùng thứ tự), dịch lúc dùng.
func viErrorReps() []string {
	return []string{
		i18n.T(`lệnh lạ $1 của $2`),
		i18n.T("\n\nCó phải ý bạn là:\n"),
		i18n.T(`cờ lạ: $1`),
		i18n.T(`cờ ngắn lạ: $1 trong $2`),
		i18n.T(`cờ cần giá trị: $1 trong $2`),
		i18n.T(`cờ cần giá trị: $1`),
		i18n.T(`giá trị $1 không hợp lệ cho cờ $2: `),
		i18n.T(`cú pháp cờ sai: $1`),
		i18n.T(`thiếu cờ bắt buộc $1`),
		i18n.T(`không nhận đối số, nhận được $1`),
		i18n.T(`cần đúng $1 đối số, nhận được $2`),
		i18n.T(`cần ít nhất $1 đối số, nhận được $2`),
		i18n.T(`nhận nhiều nhất $1 đối số, nhận được $2`),
		i18n.T(`cần từ $1 đến $2 đối số, nhận được $3`),
	}
}

// viCobraError là thông báo lỗi cú pháp của cobra/pflag theo locale.
func viCobraError(msg string) string {
	reps := viErrorReps()
	for i, re := range viErrorRules {
		msg = re.ReplaceAllString(msg, reps[i])
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
