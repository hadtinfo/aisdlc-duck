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

	"aisdlc/internal/i18n"
	"aisdlc/internal/risk"
)

func newPolicyCmd(ctx *Context) *cobra.Command {
	c := &cobra.Command{
		Use:   "policy",
		Short: i18n.T("Chính sách rủi ro của repo: xem, lấy từ web, so với mẫu mới."),
		Args:  cobra.NoArgs,
		RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
			fmt.Fprint(ctx.Err(), cmd.UsageString())
			return ExitUsage, nil
		}),
	}
	c.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: i18n.T("In thang mức, các rule và nhãn mà rule label dùng (để điền impact)."),
			Args:  cobra.NoArgs,
			RunE:  Wrap(ctx, func(*cobra.Command, []string) (int, error) { return policyShow(ctx) }),
		},
		&cobra.Command{
			Use:   "pull",
			Short: i18n.T("Lấy chính sách mới nhất từ web (lần đầu: gửi aisdlc/risk-policy.yaml lên)."),
			Args:  cobra.NoArgs,
			RunE:  Wrap(ctx, func(*cobra.Command, []string) (int, error) { return policyPull(ctx) }),
		},
		&cobra.Command{
			Use:   "migrate",
			Short: i18n.T("Chuyển risk-policy.yaml từ schema 1 sang schema 2 (mặc định chỉ in diff; --write để ghi)."),
			Args:  cobra.NoArgs,
			RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
				write, _ := cmd.Flags().GetBool("write")
				return policyMigrate(ctx, write)
			}),
		},
		&cobra.Command{
			Use:   "diff-preset",
			Short: i18n.T("So mẫu mà chính sách dựa trên với bản mới nhất của mẫu đó."),
			Args:  cobra.NoArgs,
			RunE:  Wrap(ctx, func(*cobra.Command, []string) (int, error) { return policyDiffPreset(ctx) }),
		},
	)
	for _, sub := range c.Commands() {
		if sub.Name() == "migrate" {
			sub.Flags().Bool("write", false, i18n.Tf("ghi kết quả vào %s", RepoPolicyFile))
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
		preset = i18n.Tf(" — mẫu %s v%s", p.Preset.Name, p.Preset.Version)
	}
	ctx.Println(i18n.Tf("Chính sách v%s (%s)%s", strconv.Itoa(p.Version), l.Origin, preset))
	ctx.Println(i18n.Tf("Thang mức: %s (mặc định: %s)", strings.Join(p.Levels, " < "), p.DefaultLevel))
	if p.Schema == 2 {
		ctx.Println(i18n.Tf("Phân loại: devLower=%s", string(p.Classification.DevLower)))
	} else {
		ctx.Println(i18n.Tf("Phân loại: agent=%s, devLower=%s", string(p.Classification.Agent), string(p.Classification.DevLower)))
	}
	ctx.Println("Rule:")
	if len(p.Rules) == 0 {
		ctx.Println(i18n.T("  (không có)"))
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
				off = i18n.T(" (tắt)")
			}
			fmt.Fprintf(ctx.Out(), "  %-4s %-6s %s%s\n", r.ID, r.Level, r.Describe, off)
			continue
		}
		note := ""
		if r.Note != "" {
			note = " — " + r.Note
		}
		if !r.Enabled {
			off = i18n.T(" (tắt)")
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
	shown := i18n.T("(không có)")
	if len(labels) > 0 {
		shown = strings.Join(labels, ", ")
	}
	if p.Schema != 2 {
		ctx.Println(i18n.Tf("Nhãn cho impact.labels (rule label): %s", shown))
	}
	ctx.Println(i18n.T("Yêu cầu theo mức:"))
	for _, level := range p.Levels {
		reqs := p.RequirementsFor(level)
		text := i18n.T("(không có)")
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
		return 0, abort(2, i18n.Tf("Máy này chưa `aisdlc web login`: chính sách là %s trong repo, không cần pull.", RepoPolicyFile))
	}
	c, err := web.PolicyPull()
	if err != nil {
		return 0, err
	}
	// chính sách trên web hỏng thì báo luôn (Abort 2, liệt kê lỗi)
	if _, err := LoadPolicy(ctx, root, false); err != nil {
		return 0, err
	}
	ctx.Println(i18n.Tf("Chính sách v%s (web)", strconv.Itoa(c.Version)))
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
		return 0, abort(2, i18n.T("Chính sách không ghi `preset: { name, version }`; không có mẫu để so."))
	}
	names := risk.PresetNames()
	if !slices.Contains(names, ref.Name) {
		return 0, abort(2, i18n.Tf("Không có mẫu '%s'; các mẫu có sẵn: %s.", ref.Name, strings.Join(names, ", ")))
	}
	latest := risk.PresetLatestVersion(ref.Name)
	if latest == ref.Version {
		ctx.Println(i18n.Tf("Chính sách theo mẫu %s v%s, đã là bản mới nhất.", ref.Name, ref.Version))
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
	ctx.Println(i18n.Tf("Mẫu %s v%s → v%s: %s thay đổi", ref.Name, ref.Version, latest, strconv.Itoa(len(changes))))
	for _, c := range changes {
		var what string
		switch {
		case c.Before == "":
			what = c.After
		case c.After == "":
			what = i18n.Tf("%s (bỏ)", c.Before)
		default:
			what = c.Before + " → " + c.After
		}
		loose := ""
		if c.Loosening {
			loose = i18n.T(" (nới)")
		}
		ctx.Println("  " + c.Kind + " " + c.Key + ": " + what + loose)
	}
	ctx.Println(i18n.Tf("Không tự áp dụng: chọn mục cần lấy rồi sửa %s qua MR (repo dùng web thì sửa trên web).", RepoPolicyFile))
	return ExitOK, nil
}

// policyMigrate chuyển aisdlc/risk-policy.yaml từ schema 1 sang schema 2: in diff, chỉ ghi file khi có --write.
func policyMigrate(ctx *Context, write bool) (int, error) {
	root, err := ctx.RepoRoot()
	if err != nil {
		return 0, err
	}
	if NewWebCache(ctx, root).PolicyWebMode() {
		return 0, abort(2, i18n.T("Chế độ web: chuyển chính sách trên trang chính sách của web."))
	}
	f := filepath.Join(root, RepoPolicyFile)
	b, err := os.ReadFile(f)
	if err != nil {
		return 0, abort(2, i18n.Tf("Không đọc được %s: %s", RepoPolicyFile, err.Error()))
	}
	from, err := parsePolicy(string(b), RepoPolicyFile)
	if err != nil {
		return 0, err
	}
	if from.Schema == 2 {
		ctx.Println(i18n.Tf("%s đã là schema 2, không cần chuyển.", RepoPolicyFile))
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
	ctx.Println(i18n.Tf("Chuyển %s sang schema 2: %s thay đổi", RepoPolicyFile, strconv.Itoa(len(changes))))
	for _, c := range changes {
		switch {
		case c.Before == "":
			ctx.Println("  " + c.Kind + " " + c.Key + ": " + c.After)
		case c.After == "":
			ctx.Println(i18n.Tf("  %s %s: %s (bỏ)", c.Kind, c.Key, c.Before))
		default:
			ctx.Println("  " + c.Kind + " " + c.Key + ": " + c.Before + " → " + c.After)
		}
	}
	if !write {
		ctx.Println(i18n.Tf("Chưa ghi gì. Chạy lại với --write để ghi %s, rồi commit qua MR.", RepoPolicyFile))
		return ExitOK, nil
	}
	if err := os.WriteFile(f, []byte(out), 0o644); err != nil {
		return 0, errors.New(i18n.Tf("Không ghi được %s: %s", RepoPolicyFile, err.Error()))
	}
	ctx.Println(i18n.Tf("Đã ghi %s (schema 2). Xem lại các mô tả rồi commit qua MR.", RepoPolicyFile))
	return ExitOK, nil
}
