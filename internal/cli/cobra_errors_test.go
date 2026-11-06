package cli

import (
	"strings"
	"testing"
)

// Lỗi cú pháp của cobra/pflag (lệnh lạ, cờ lạ, cờ thiếu giá trị, sai số đối số, thiếu cờ bắt buộc) bằng tiếng Việt,
// vẫn exit 2 kèm cách dùng.
func TestCobraErrorsAreVietnamese(t *testing.T) {
	english := []string{"unknown command", "unknown flag", "unknown shorthand", "flag needs an argument",
		"invalid argument", "bad flag syntax", "accepts ", "arg(s)", "received", "requires at least", "required flag",
		"Did you mean", " for \"aisdlc"}
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"khong-co"}, []string{`lệnh lạ "khong-co" của "aisdlc"`}},
		{[]string{"step", "khong-co"}, []string{`lệnh lạ "khong-co" của "aisdlc step"`}},
		{[]string{"--khong-co"}, []string{"cờ lạ: --khong-co"}},
		{[]string{"status", "-Z"}, []string{"cờ ngắn lạ: 'Z' trong -Z"}},
		{[]string{"reject", "brd", "--message"}, []string{"cờ cần giá trị: --message"}},
		{[]string{"status", "x"}, []string{`lệnh lạ "x" của "aisdlc status"`}}, // cobra.NoArgs báo như lệnh lạ
		{[]string{"use"}, []string{"cần đúng 1 đối số, nhận được 0"}},
		{[]string{"use", "A-1", "B-2"}, []string{"cần đúng 1 đối số, nhận được 2"}},
		{[]string{"reject", "brd"}, []string{`thiếu cờ bắt buộc "message"`}},
	}
	for _, c := range cases {
		o, e := &strings.Builder{}, &strings.Builder{}
		code := Run(NewContext(strings.NewReader(""), o, e, noConsole(), t.TempDir()), c.args...)
		name := "aisdlc " + strings.Join(c.args, " ")
		if code != ExitUsage {
			t.Errorf("%s: exit %d, muốn %d\n%s", name, code, ExitUsage, e.String())
		}
		got := e.String()
		for _, w := range append(c.want, "Cách dùng:") {
			if !strings.Contains(got, w) {
				t.Errorf("%s: thiếu %q:\n%s", name, w, got)
			}
		}
		for _, en := range english {
			if strings.Contains(got, en) {
				t.Errorf("%s: còn tiếng Anh %q:\n%s", name, en, got)
			}
		}
	}
}

// Bộ dịch giữ nguyên thông báo không nhận ra và dịch đủ các mẫu của cobra/pflag.
func TestViCobraError(t *testing.T) {
	for in, want := range map[string]string{
		`invalid argument "x" for "--n" flag: strconv.ParseInt: parsing "x": invalid syntax`: `giá trị "x" không hợp lệ cho cờ "--n": strconv.ParseInt: parsing "x": invalid syntax`,
		"bad flag syntax: ---x":                                                     "cú pháp cờ sai: ---x",
		`flag needs an argument: 'm' in -m`:                                         "cờ cần giá trị: 'm' trong -m",
		"requires at least 1 arg(s), only received 0":                               "cần ít nhất 1 đối số, nhận được 0",
		"accepts at most 2 arg(s), received 3":                                      "nhận nhiều nhất 2 đối số, nhận được 3",
		"accepts between 1 and 2 arg(s), received 3":                                "cần từ 1 đến 2 đối số, nhận được 3",
		"unknown command \"stat\" for \"aisdlc\"\n\nDid you mean this?\n\tstatus\n": "lệnh lạ \"stat\" của \"aisdlc\"\n\nCó phải ý bạn là:\n\tstatus\n",
		"lỗi khác": "lỗi khác",
	} {
		eq(t, viCobraError(in), want)
	}
}
