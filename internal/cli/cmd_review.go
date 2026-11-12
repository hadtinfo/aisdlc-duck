package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/gate"
	"aisdlc/internal/review"
	"aisdlc/internal/review/cardbuild"
)

// cardWidth là bề rộng review card ở terminal.
const cardWidth = 100

func newReviewCmd(ctx *Context) *cobra.Command {
	var fo FeatureOption
	var asJSON bool
	c := &cobra.Command{
		Use:   "review <gate>",
		Short: "In review card của một cổng (cờ đỏ, giả định, tóm tắt); không đổi trạng thái.",
		Args:  cobra.ExactArgs(1),
	}
	fo.AddTo(c)
	c.Flags().BoolVar(&asJSON, "json", false, "In JSON thay vì chữ")
	c.RunE = Wrap(ctx, func(_ *cobra.Command, args []string) (int, error) {
		if asJSON {
			ctx.WithoutBanner()
		}
		f, err := fo.Feature(ctx)
		if err != nil {
			return 0, err
		}
		g, err := gateInFlow(f, args[0])
		if err != nil {
			return 0, err
		}
		text, err := guarded(func() (string, error) {
			card, err := CardFor(ctx, f, g)
			if err != nil {
				return "", err
			}
			if asJSON {
				b, err := jsonMarshal(card)
				if err != nil {
					return "", err
				}
				return string(jacksonIndent(b)) + "\n", nil
			}
			return review.RenderCard(f.Key.String(), card, cardWidth, ctx.Color(), gateApprovalDisplay(f, g)), nil
		})
		if err != nil {
			// không bao giờ in stack trace: một dòng lỗi là đủ
			msg, _, _ := strings.Cut(err.Error(), "\n")
			return 0, abort(1, "Không dựng được review card: "+msg)
		}
		fmt.Fprint(ctx.Out(), text)
		return ExitOK, nil
	})
	return c
}

// CardFor là review card của cổng g (ReviewCommand.card).
func CardFor(ctx *Context, f *Feature, g gate.Gate) (review.ReviewCard, error) {
	return cardbuild.BuildCard(cardbuild.ReviewInput{Paths: f.Paths, Meta: f.Meta, Flow: f.Flow(),
		Config: f.Config, Git: f.Git(), Adapter: f.Adapter}, g)
}

// safeCard như CardFor nhưng panic cũng thành lỗi (Java bắt mọi Throwable trừ OutOfMemoryError).
func safeCard(ctx *Context, f *Feature, g gate.Gate) (review.ReviewCard, error) {
	return guarded(func() (review.ReviewCard, error) { return CardFor(ctx, f, g) })
}

// guarded chạy fn, đổi panic thành lỗi.
func guarded[T any](fn func() (T, error)) (v T, err error) {
	defer func() {
		if p := recover(); p != nil {
			var zero T
			v, err = zero, fmt.Errorf("%v", p)
		}
	}()
	return fn()
}

// gateApprovalDisplay là dòng "Duyệt: …" của thẻ (AutoFlow.display của bản duyệt đang lưu); "" khi không có.
func gateApprovalDisplay(f *Feature, g gate.Gate) string {
	a, ok, err := f.Gates.Approval(g)
	if err != nil {
		return ""
	}
	return approvalDisplay(a, ok)
}

// jacksonIndent định dạng lại JSON gọn như DefaultPrettyPrinter của Jackson (INDENT_OUTPUT): mỗi khóa một dòng thụt 2
// dấu cách, `"khóa" : giá trị`, mảng trên một dòng `[ a, b ]`, rỗng là `{ }` và `[ ]`.
func jacksonIndent(compact []byte) []byte {
	var out bytes.Buffer
	var stack []byte // '{' hoặc '['
	nesting := 0
	newline := func() {
		out.WriteByte('\n')
		out.WriteString(strings.Repeat("  ", nesting))
	}
	inStr, esc := false, false
	for i := 0; i < len(compact); i++ {
		c := compact[i]
		if inStr {
			out.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
			out.WriteByte(c)
		case '{':
			if i+1 < len(compact) && compact[i+1] == '}' {
				out.WriteString("{ }")
				i++
				continue
			}
			stack = append(stack, '{')
			nesting++
			out.WriteByte('{')
			newline()
		case '[':
			if i+1 < len(compact) && compact[i+1] == ']' {
				out.WriteString("[ ]")
				i++
				continue
			}
			stack = append(stack, '[')
			out.WriteString("[ ")
		case '}':
			stack = stack[:len(stack)-1]
			nesting--
			newline()
			out.WriteByte('}')
		case ']':
			stack = stack[:len(stack)-1]
			out.WriteString(" ]")
		case ',':
			out.WriteByte(',')
			if stack[len(stack)-1] == '{' {
				newline()
			} else {
				out.WriteByte(' ')
			}
		case ':':
			out.WriteString(" : ")
		default:
			out.WriteByte(c)
		}
	}
	return out.Bytes()
}
