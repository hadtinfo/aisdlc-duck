package eval

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"aisdlc/internal/gitx"
	"aisdlc/internal/i18n"
	"aisdlc/internal/review"
	"aisdlc/internal/risk"
	"aisdlc/internal/run"
	"aisdlc/internal/step"
)

const (
	maxMarkdownLines = 40
	maxCell          = 160
)

var lineBreakRe = regexp.MustCompile(`\s*(?:\r\n|[\n\x0B\x0C\r\x{85}\x{2028}\x{2029}])\s*`)

func oneLine(s string) string {
	t := strings.TrimFunc(strings.ReplaceAll(lineBreakRe.ReplaceAllString(s, " "), "|", `\|`), func(r rune) bool { return r <= ' ' })
	if utf8.RuneCountInString(t) <= maxCell {
		return t
	}
	return string([]rune(t)[:maxCell-1]) + "…"
}

func row(id, result, detail string) string {
	return "| " + id + " | " + result + " | " + oneLine(detail) + " |"
}

// splitLines tách dòng như String.lines của Java (\n, \r, \r\n; không trả dòng rỗng cuối).
func splitLines(s string) []string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// RenderReport dựng bản tóm tắt eval/report.md (tối đa 40 dòng, dùng làm mô tả MR).
func RenderReport(r EvalReport, notPassed []run.CaseResult) string {
	var lines []string
	pass := i18n.T("KHÔNG ĐẠT")
	if r.Passed {
		pass = i18n.T("ĐẠT")
	}
	lines = append(lines, "Eval "+r.Feature+": "+pass, "", i18n.Tf("Chế độ %s · %s", r.Mode, review.InstantString(r.At)), "")
	if r.Risk != nil {
		lines = addRisk(lines, *r.Risk)
	}
	lines = append(lines, i18n.T("| Kiểm tra cứng | Kết quả | Chi tiết |"), "|---|---|---|")
	for _, h := range r.Hard {
		res := i18n.T("KHÔNG ĐẠT")
		if h.Passed {
			res = i18n.T("ĐẠT")
		}
		lines = append(lines, row(h.ID, res, h.Detail))
	}
	lines = append(lines, "", softHeader(i18n.Default())+" "+i18n.T("Mức | Chi tiết |"), "|---|---|---|")
	for _, s := range r.Soft {
		lines = append(lines, row(s.ID, s.Level, s.Detail))
	}
	lines = append(lines, "")
	if len(notPassed) == 0 {
		lines = append(lines, i18n.T("Case không pass: không có."))
	} else {
		lines = append(lines, i18n.T("Case không pass:"))
		room := maxMarkdownLines - len(lines)
		shown := max(0, room-1)
		if len(notPassed) <= room {
			shown = len(notPassed)
		}
		for _, c := range notPassed[:shown] {
			lines = append(lines, "- "+c.ID+" ("+c.Step+", "+string(c.Outcome)+"): "+oneLine(c.Message))
		}
		if shown < len(notPassed) {
			lines = append(lines, i18n.Tf("- … và %d case khác", len(notPassed)-shown))
		}
	}
	if len(lines) > maxMarkdownLines {
		cut := append([]string{}, lines[:maxMarkdownLines-1]...)
		cut = append(cut, i18n.T("… (đã cắt bớt, xem eval/report.json)"))
		return strings.Join(cut, "\n") + "\n"
	}
	return strings.Join(lines, "\n") + "\n"
}

func addRisk(lines []string, k RiskSection) []string {
	lines = append(lines, i18n.T("## Rủi ro"), "")
	head := i18n.Tf("Mức %s · chính sách v%d", k.Level, k.PolicyVersion)
	if len(k.Matched) > 0 {
		head += i18n.Tf(" · khớp: %s", strings.Join(k.Matched, ", "))
	}
	if k.Override != nil {
		head += i18n.Tf(" · đổi tay: %s", *k.Override)
	}
	lines = append(lines, oneLine(head), "")
	for _, s := range k.Requirements {
		mark := "[~]"
		switch s.Status {
		case risk.RequirementOK:
			mark = "[x]"
		case risk.RequirementMissing:
			mark = "[ ]"
		}
		d := ""
		if strings.TrimSpace(s.Detail) != "" {
			d = " — " + s.Detail
		}
		lines = append(lines, oneLine("- "+mark+" "+s.Name+d))
	}
	return append(lines, "")
}

// WithSoftRow thay dòng của chỉ số mềm s.ID trong report.md đã render; chưa có thì chèn vào cuối bảng chỉ số mềm.
// Vẫn giữ tối đa 40 dòng.
func WithSoftRow(md string, s SoftCheck) string {
	lines := splitLines(md)
	r := row(s.ID, s.Level, s.Detail)
	prefix := "| " + s.ID + " |"
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) {
			lines[i] = r
			return strings.Join(lines, "\n") + "\n"
		}
	}
	header := -1
	for i, l := range lines {
		// report.md có thể viết lúc repo còn locale khác: nhận tiêu đề bảng của cả hai ngôn ngữ
		if strings.HasPrefix(l, softHeader(i18n.VI)) || strings.HasPrefix(l, softHeader(i18n.EN)) {
			header = i
		}
	}
	if header < 0 {
		return md
	}
	at := header + 2
	for at < len(lines) && strings.HasPrefix(lines[at], "| ") {
		at++
	}
	lines = append(lines[:at], append([]string{r}, lines[at:]...)...)
	if len(lines) > maxMarkdownLines {
		// bỏ bớt dòng cuối (danh sách case không pass), giữ dòng báo cắt
		cut := append([]string{}, lines[:maxMarkdownLines-1]...)
		lines = append(cut, i18n.T("… (đã cắt bớt, xem eval/report.json)"))
	}
	return strings.Join(lines, "\n") + "\n"
}

// softHeader là ô đầu của tiêu đề bảng chỉ số mềm trong report.md.
func softHeader(loc i18n.Locale) string { return loc.T("| Chỉ số mềm |") }

// DiffByStep là diff nhóm theo bước, cùng định dạng aisdlc diff --by-step [--patch].
func DiffByStep(records []step.StepRecord, titles map[string]string, git *gitx.Repo, patch bool) (string, error) {
	ctx := context.Background()
	var out strings.Builder
	live := ""
	for _, r := range records {
		end := r.EndTree
		changes := r.Changes
		if end == "" { // đang làm: so với working tree hiện tại
			if live == "" {
				t, err := git.SnapshotTree(ctx)
				if err != nil {
					return "", err
				}
				live = t
			}
			end = live
			c, err := git.Diff(ctx, r.StartTree, end)
			if err != nil {
				return "", err
			}
			changes = c
		}
		out.WriteString("## " + r.ID + " — " + titles[r.ID] + "\n\n")
		for _, c := range changes {
			path := c.Path
			if c.OldPath != "" {
				path = c.OldPath + " => " + c.Path
			}
			if c.Binary {
				out.WriteString("-\t-")
			} else {
				out.WriteString(strconv.Itoa(c.Added) + "\t" + strconv.Itoa(c.Deleted))
			}
			out.WriteString("\t" + path + "\n")
		}
		if patch {
			p, err := git.Patch(ctx, r.StartTree, end)
			if err != nil {
				return "", err
			}
			out.WriteString("\n```diff\n" + p + "```\n")
		}
		out.WriteString("\n")
	}
	return out.String(), nil
}
