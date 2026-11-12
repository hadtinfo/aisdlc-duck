package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/risk"
)

func newPolicyCmd(ctx *Context) *cobra.Command {
	c := &cobra.Command{
		Use:   "policy",
		Short: "Chính sách rủi ro của repo: xem, lấy từ web, so với mẫu mới.",
		Args:  cobra.NoArgs,
		RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
			fmt.Fprint(ctx.Err(), cmd.UsageString())
			return ExitUsage, nil
		}),
	}
	c.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "In thang mức, các rule và nhãn mà rule label dùng (để điền impact).",
			Args:  cobra.NoArgs,
			RunE:  Wrap(ctx, func(*cobra.Command, []string) (int, error) { return policyShow(ctx) }),
		},
		&cobra.Command{
			Use:   "pull",
			Short: "Lấy chính sách mới nhất từ web (lần đầu: gửi aisdlc/risk-policy.yaml lên).",
			Args:  cobra.NoArgs,
			RunE:  Wrap(ctx, func(*cobra.Command, []string) (int, error) { return policyPull(ctx) }),
		},
		&cobra.Command{
			Use:   "migrate",
			Short: "Chuyển risk-policy.yaml từ schema 1 sang schema 2 (mặc định chỉ in diff; --write để ghi).",
			Args:  cobra.NoArgs,
			RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
				write, _ := cmd.Flags().GetBool("write")
				return policyMigrate(ctx, write)
			}),
		},
		&cobra.Command{
			Use:   "diff-preset",
			Short: "So mẫu mà chính sách dựa trên với bản mới nhất của mẫu đó.",
			Args:  cobra.NoArgs,
			RunE:  Wrap(ctx, func(*cobra.Command, []string) (int, error) { return policyDiffPreset(ctx) }),
		},
	)
	for _, sub := range c.Commands() {
		if sub.Name() == "migrate" {
			sub.Flags().Bool("write", false, "ghi kết quả vào "+RepoPolicyFile)
		}
	}
	return c
}

func policyShow(ctx *Context) (int, error) {
	root, err := ctx.RepoRoot()
	if err != nil {
		return 0, err
	}
	l, err := LoadPolicy(ctx, root, false)
	if err != nil {
		return 0, err
	}
	p := l.Policy
	preset := ""
	if p.Preset != nil {
		preset = " — mẫu " + p.Preset.Name + " v" + p.Preset.Version
	}
	ctx.Println("Chính sách v" + strconv.Itoa(p.Version) + " (" + l.Origin + ")" + preset)
	ctx.Println("Thang mức: " + strings.Join(p.Levels, " < ") + " (mặc định: " + p.DefaultLevel + ")")
	if p.Schema == 2 {
		ctx.Println("Phân loại: devLower=" + string(p.Classification.DevLower))
	} else {
		ctx.Println("Phân loại: agent=" + string(p.Classification.Agent) + ", devLower=" +
			string(p.Classification.DevLower))
	}
	ctx.Println("Rule:")
	if len(p.Rules) == 0 {
		ctx.Println("  (không có)")
	}
	var labels []string
	for _, r := range p.Rules {
		kind := string(r.Kind)
		if kind == "" {
			kind = "?"
		}
		off := ""
		if p.Schema == 2 {
			if !r.Enabled {
				off = " (tắt)"
			}
			fmt.Fprintf(ctx.Out(), "  %-4s %-6s %s%s\n", r.ID, r.Level, r.Describe, off)
			continue
		}
		note := ""
		if r.Note != "" {
			note = " — " + r.Note
		}
		if !r.Enabled {
			off = " (tắt)"
		}
		fmt.Fprintf(ctx.Out(), "  %-4s %-8s %s → %s%s%s\n", r.ID, kind, strings.Join(r.Match, " | "), r.Level, note, off)
		if r.Enabled && r.Kind == risk.MatchLabel {
			for _, m := range r.Match {
				if !slices.Contains(labels, m) {
					labels = append(labels, m)
				}
			}
		}
	}
	shown := "(không có)"
	if len(labels) > 0 {
		shown = strings.Join(labels, ", ")
	}
	if p.Schema != 2 {
		ctx.Println("Nhãn cho impact.labels (rule label): " + shown)
	}
	ctx.Println("Yêu cầu theo mức:")
	for _, level := range p.Levels {
		reqs := p.RequirementsFor(level)
		text := "(không có)"
		if len(reqs) > 0 {
			parts := make([]string, len(reqs))
			for i, q := range reqs {
				parts[i] = describeRequirement(q)
			}
			text = strings.Join(parts, ", ")
		}
		ctx.Println("  " + level + ": " + text)
	}
	return ExitOK, nil
}

// describeRequirement là "id k=v ..."; tham số theo thứ tự khóa (map của Go không giữ thứ tự trong YAML).
func describeRequirement(q risk.Requirement) string {
	keys := make([]string, 0, len(q.Params))
	for k := range q.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(q.ID)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%v", k, q.Params[k])
	}
	return b.String()
}

// policyPull: lấy chính sách mới nhất từ web (lần đầu: gửi aisdlc/risk-policy.yaml lên). Chưa `aisdlc web login`
// thì không cần pull.
func policyPull(ctx *Context) (int, error) {
	root, err := ctx.RepoRoot()
	if err != nil {
		return 0, err
	}
	web := NewWebCache(ctx, root)
	if !web.PolicyWebMode() {
		return 0, abort(2, "Máy này chưa `aisdlc web login`: chính sách là "+RepoPolicyFile+
			" trong repo, không cần pull.")
	}
	c, err := web.PolicyPull()
	if err != nil {
		return 0, err
	}
	// chính sách trên web hỏng thì báo luôn (Abort 2, liệt kê lỗi)
	if _, err := LoadPolicy(ctx, root, false); err != nil {
		return 0, err
	}
	ctx.Println("Chính sách v" + strconv.Itoa(c.Version) + " (web)")
	return ExitOK, nil
}

func policyDiffPreset(ctx *Context) (int, error) {
	root, err := ctx.RepoRoot()
	if err != nil {
		return 0, err
	}
	l, err := LoadPolicy(ctx, root, false)
	if err != nil {
		return 0, err
	}
	ref := l.Policy.Preset
	if ref == nil || ref.Name == "" || ref.Version == "" {
		return 0, abort(2, "Chính sách không ghi `preset: { name, version }`; không có mẫu để so.")
	}
	names := risk.PresetNames()
	if !slices.Contains(names, ref.Name) {
		return 0, abort(2, "Không có mẫu '"+ref.Name+"'; các mẫu có sẵn: "+strings.Join(names, ", ")+".")
	}
	latest := risk.PresetLatestVersion(ref.Name)
	if latest == ref.Version {
		ctx.Println("Chính sách theo mẫu " + ref.Name + " v" + ref.Version + ", đã là bản mới nhất.")
		return ExitOK, nil
	}
	from, err := risk.LoadPreset(ref.Name, ref.Version)
	if err != nil {
		return 0, err
	}
	to, err := risk.LatestPreset(ref.Name)
	if err != nil {
		return 0, err
	}
	changes := risk.DiffPolicies(from, to)
	ctx.Println("Mẫu " + ref.Name + " v" + ref.Version + " → v" + latest + ": " + strconv.Itoa(len(changes)) +
		" thay đổi")
	for _, c := range changes {
		var what string
		switch {
		case c.Before == "":
			what = c.After
		case c.After == "":
			what = c.Before + " (bỏ)"
		default:
			what = c.Before + " → " + c.After
		}
		loose := ""
		if c.Loosening {
			loose = " (nới)"
		}
		ctx.Println("  " + c.Kind + " " + c.Key + ": " + what + loose)
	}
	ctx.Println("Không tự áp dụng: chọn mục cần lấy rồi sửa " + RepoPolicyFile +
		" qua MR (repo dùng web thì sửa trên web).")
	return ExitOK, nil
}

// policyMigrate chuyển aisdlc/risk-policy.yaml từ schema 1 sang schema 2: in diff, chỉ ghi file khi có --write.
func policyMigrate(ctx *Context, write bool) (int, error) {
	root, err := ctx.RepoRoot()
	if err != nil {
		return 0, err
	}
	if NewWebCache(ctx, root).PolicyWebMode() {
		return 0, abort(2, "Chế độ web: chuyển chính sách trên trang chính sách của web.")
	}
	f := filepath.Join(root, RepoPolicyFile)
	b, err := os.ReadFile(f)
	if err != nil {
		return 0, abort(2, "Không đọc được "+RepoPolicyFile+": "+err.Error())
	}
	from, err := parsePolicy(string(b), RepoPolicyFile)
	if err != nil {
		return 0, err
	}
	if from.Schema == 2 {
		ctx.Println(RepoPolicyFile + " đã là schema 2, không cần chuyển.")
		return ExitOK, nil
	}
	to, err := risk.MigrateToV2(from)
	if err != nil {
		return 0, err
	}
	out, err := to.ToYAML()
	if err != nil {
		return 0, err
	}
	if _, err := risk.ParsePolicy(out); err != nil {
		return 0, err
	}
	changes := risk.DiffPolicies(from, to)
	ctx.Println("Chuyển " + RepoPolicyFile + " sang schema 2: " + strconv.Itoa(len(changes)) + " thay đổi")
	for _, c := range changes {
		switch {
		case c.Before == "":
			ctx.Println("  " + c.Kind + " " + c.Key + ": " + c.After)
		case c.After == "":
			ctx.Println("  " + c.Kind + " " + c.Key + ": " + c.Before + " (bỏ)")
		default:
			ctx.Println("  " + c.Kind + " " + c.Key + ": " + c.Before + " → " + c.After)
		}
	}
	if !write {
		ctx.Println("Chưa ghi gì. Chạy lại với --write để ghi " + RepoPolicyFile + ", rồi commit qua MR.")
		return ExitOK, nil
	}
	if err := os.WriteFile(f, []byte(out), 0o644); err != nil {
		return 0, errors.New("Không ghi được " + RepoPolicyFile + ": " + err.Error())
	}
	ctx.Println("Đã ghi " + RepoPolicyFile + " (schema 2). Xem lại các mô tả rồi commit qua MR.")
	return ExitOK, nil
}
