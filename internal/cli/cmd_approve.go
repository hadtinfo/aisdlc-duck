package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/spf13/cobra"

	"aisdlc/internal/eval"
	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/policy"
	"aisdlc/internal/review"
	"aisdlc/internal/spec"
)

// maxIssues là số lỗi high của agent review in trước khi duyệt REVIEW.
const maxIssues = 10

func newApproveCmd(ctx *Context) *cobra.Command {
	var fo FeatureOption
	c := &cobra.Command{
		Use:   "approve <gate>",
		Short: "Chỉ dev: duyệt một cổng đã nộp (brd, design, cases, review).",
		Args:  cobra.ExactArgs(1),
	}
	fo.AddTo(c)
	c.RunE = Wrap(ctx, func(_ *cobra.Command, args []string) (int, error) {
		f, err := fo.Feature(ctx)
		if err != nil {
			return 0, err
		}
		if err := ctx.WebNetwork(f); err != nil {
			return 0, err
		}
		g, err := gateInFlow(f, args[0])
		if err != nil {
			return 0, err
		}
		state, err := gateSummary(ctx, f, g)
		if err != nil {
			return 0, err
		}
		if state != gate.AwaitingApproval {
			auto := ""
			if state == gate.Approved {
				if d := gateApprovalDisplay(f, g); d != "" {
					auto = " (" + d + ")"
				}
			}
			return 0, abort(2, "Cổng "+string(g)+" đang ở trạng thái "+string(state)+auto+
				"; chỉ duyệt được khi AWAITING_APPROVAL (đã nộp, không có phát hiện).")
		}
		if g == gate.REVIEW {
			x, open, err := stepsNotAccepted(f)
			if err != nil {
				return 0, err
			}
			if open {
				return 0, abort(1, "Không duyệt được cổng REVIEW: "+x.Code+": "+x.Message)
			}
			if err := requireFreshEval(f); err != nil {
				return 0, err
			}
		}
		printCard(ctx, f, g)
		hash, err := f.Gates.CurrentHash(g)
		if err != nil {
			return 0, err
		}
		channel, err := ctx.ConfirmDev(f.Key, invocation(ctx, "approve"), hash)
		if err != nil {
			return 0, err
		}
		approver, err := ctx.Approver(f.Root)
		if err != nil {
			return 0, err
		}
		a, err := f.Gates.Approve(g, approver, channel)
		if err != nil {
			return 0, err
		}
		ctx.Println("Đã duyệt cổng " + string(g) + " bởi " + a.Approver + " (hash " + a.Hash + ").")
		return ExitOK, nil
	})
	return c
}

// invocation là lệnh chỉ dev đang chạy, hoặc DevCommand rỗng (null của Java) khi không dựng được.
func invocation(ctx *Context, verb string) policy.DevCommand {
	d, _ := ctx.Invocation(verb)
	return d
}

// printCard in review card trước khi dev xác nhận; lỗi dựng thẻ không chặn việc duyệt.
func printCard(ctx *Context, f *Feature, g gate.Gate) {
	text, err := guarded(func() (string, error) {
		card, err := CardFor(ctx, f, g)
		if err != nil {
			return "", err
		}
		return review.RenderCard(f.Key.String(), card, cardWidth, ctx.Color(), gateApprovalDisplay(f, g)), nil
	})
	if err != nil {
		ctx.Println("Không dựng được review card: " + err.Error())
		return
	}
	ctx.Println("")
	fmt.Fprint(ctx.Out(), text)
	ctx.Println("")
}

// requireFreshEval: không duyệt REVIEW khi code đã đổi so với tree mà eval/report.json đã chấm.
func requireFreshEval(f *Feature) error {
	report := filepath.Join(f.Paths.EvalDir(), "report.json")
	var stale *spec.Finding
	r, err := eval.ReadEvalReport(report)
	if err == nil {
		stale, err = staleEval(f, r)
	}
	if err != nil {
		stale = &spec.Finding{Code: "REVIEW_NO_EVAL", Message: "Không đọc được " + f.Paths.Rel(report) +
			"; chạy lại `aisdlc eval` rồi `aisdlc submit review`."}
	}
	if stale != nil {
		return abort(1, "Không duyệt được cổng REVIEW: "+stale.Code+": "+stale.Message+
			" Sau đó nộp lại `aisdlc submit review`.")
	}
	return nil
}

// gateInFlow đọc tên cổng và đòi cổng thuộc luồng của tính năng.
func gateInFlow(f *Feature, name string) (gate.Gate, error) {
	g, err := GateOf(name)
	if err != nil {
		return "", err
	}
	if !slices.Contains(gate.Sequence(f.Flow()), g) {
		return "", abort(2, "Cổng "+string(g)+" không thuộc luồng "+string(f.Flow())+".")
	}
	return g, nil
}

// gateSummary in tóm tắt cổng để dev xem trước khi xác nhận: trạng thái, hash, file, phát hiện.
func gateSummary(ctx *Context, f *Feature, g gate.Gate) (gate.GateState, error) {
	state, err := f.Gates.State(g)
	if err != nil {
		return "", err
	}
	files := g.Artifacts(f.Paths, f.Flow())
	hash, err := model.SHA256Files(f.Root, files)
	if err != nil {
		return "", err
	}
	ctx.Println("Tính năng " + f.Key.String() + " — cổng " + string(g) + " (" + string(state) + ")")
	ctx.Println("Hash hiện tại: " + hash)
	ctx.Println("File:")
	for _, p := range files {
		ctx.Println("  - " + f.Paths.Rel(p))
	}
	sub, ok, err := f.Store.Submission(g)
	if err != nil {
		return "", err
	}
	if ok {
		ctx.Println("Nộp lúc " + review.InstantString(sub.At) + ", hash " + sub.Hash)
		if len(sub.Findings) == 0 {
			ctx.Println("Phát hiện: không có")
		} else {
			ctx.Println("Phát hiện:")
		}
		for _, x := range sub.Findings {
			ctx.Println("  - " + x.Code + ": " + x.Message)
		}
	} else {
		ctx.Println("Chưa nộp.")
	}
	if g == gate.REVIEW {
		agentReviewSummary(ctx, f)
	}
	return state, nil
}

// agentReviewSummary in kết quả review độc lập (review-agent.json) để dev đọc trước khi duyệt REVIEW.
func agentReviewSummary(ctx *Context, f *Feature) {
	s := eval.AgentReviewCheck(f.Paths.ReviewAgent())
	ctx.Println("Agent review: " + s.Detail)
	if s.Level != eval.SoftOK && s.Level != eval.SoftWarn {
		return
	}
	r, err := eval.ReadAgentReview(f.Paths.ReviewAgent())
	if err != nil {
		return // detail ở trên đã nêu lỗi
	}
	var high []eval.Issue
	for _, i := range r.Issues {
		if i.Severity == "high" {
			high = append(high, i)
		}
	}
	shown := min(len(high), maxIssues)
	for _, i := range high[:shown] {
		ctx.Println("  - high " + i.Step + " " + i.File + ":" + strconv.Itoa(i.Line) + ": " + i.Message)
	}
	if shown < len(high) {
		ctx.Println("  - … và " + strconv.Itoa(len(high)-shown) + " lỗi high khác")
	}
}
