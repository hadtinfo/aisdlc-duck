package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/risk"
	"aisdlc/internal/step"
)

func newStatusCmd(ctx *Context) *cobra.Command {
	var fo FeatureOption
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: i18n.T("Trạng thái cổng và bước của tính năng đang làm."),
		Args:  cobra.NoArgs,
	}
	fo.AddTo(c)
	c.Flags().BoolVar(&asJSON, "json", false, i18n.T("In JSON (máy đọc)"))
	c.RunE = Wrap(ctx, func(*cobra.Command, []string) (int, error) {
		return status(ctx, &fo, asJSON)
	})
	return c
}

// jsonField / jsonObject: object JSON giữ thứ tự khóa như ObjectNode của Jackson.
type jsonField struct {
	key string
	val any
}

type jsonObject []jsonField

func (o jsonObject) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := jsonMarshal(f.key)
		if err != nil {
			return nil, err
		}
		v, err := jsonMarshal(f.val)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func status(ctx *Context, fo *FeatureOption, asJSON bool) (int, error) {
	if asJSON {
		ctx.WithoutBanner()
	} else {
		ctx.Println("Locale: " + string(i18n.Default()))
	}
	_, explicit, err := fo.Key()
	if err != nil {
		return 0, err
	}
	if !explicit {
		root, err := ctx.RepoRoot()
		if err != nil {
			return 0, err
		}
		bound := false
		if s, ok := ctx.ResolveSession(); ok {
			if _, bound, err = model.ReadSession(root, s); err != nil {
				return 0, err
			}
		}
		if !bound {
			open, err := NewFeatureResolver(ctx).OpenFeatures(root)
			if err != nil {
				return 0, err
			}
			if len(open) > 1 {
				return listOpen(ctx, root, open, asJSON)
			}
		}
	}
	f, err := fo.Feature(ctx)
	if err != nil {
		return 0, err
	}
	seq := gate.Sequence(f.Flow())
	gates := make(jsonObject, 0, len(seq))
	for _, g := range seq {
		st, err := f.Gates.State(g)
		if err != nil {
			return 0, err
		}
		gates = append(gates, jsonField{string(g), string(st)})
	}
	var ids []string
	recs := map[string]*step.StepRecord{}
	for _, s := range f.Specs() {
		if _, ok := recs[s.ID]; !ok {
			ids = append(ids, s.ID)
		}
		recs[s.ID] = nil
	}
	all, err := f.Steps().All()
	if err != nil {
		return 0, err
	}
	for i := range all {
		r := all[i]
		if _, ok := recs[r.ID]; !ok {
			ids = append(ids, r.ID)
		}
		recs[r.ID] = &r
	}

	warnLocalRepoID(ctx, f)
	sa := model.ReadSessionApprove(ctx.std, f.Git(), f.Config)
	f.Web.PolicyPullQuietly()
	if asJSON {
		steps, checks := jsonObject{}, jsonObject{}
		for _, id := range ids {
			r := recs[id]
			if r == nil {
				steps = append(steps, jsonField{id, string(step.StepNotStarted)})
				continue
			}
			steps = append(steps, jsonField{id, string(r.Status)})
			if r.Check != nil {
				checks = append(checks, jsonField{id, r.Check})
			}
		}
		out := jsonObject{{"feature", f.Key.String()}, {"title", f.Meta.Title}, {"flow", string(f.Flow())},
			{"sessionApprove", sa.Enabled}, {"gates", gates}, {"steps", steps}, {"checks", checks}}
		b, err := jsonMarshal(out)
		if err != nil {
			return 0, err
		}
		ctx.Println(string(b))
		return ExitOK, nil
	}

	ctx.Println(i18n.Tf("Tính năng %s (%s): %s", f.Key.String(), string(f.Flow()), f.Meta.Title))
	if f.Mode == gate.ModeWeb {
		suffix := ""
		if !f.Web.LoggedIn() {
			suffix = i18n.T(" (chưa `aisdlc web login`)")
		}
		ctx.Println(i18n.Tf("Duyệt: trên web%s", suffix))
	}
	on := i18n.T("tắt")
	if sa.Enabled {
		on = i18n.T("bật")
	}
	ctx.Println(i18n.Tf("Duyệt trong Claude Code: %s (%s)", on, sa.Reason))
	printControl(ctx, f)
	printRisk(ctx, f)
	ctx.Println(i18n.T("Cổng:"))
	for _, g := range gates {
		a, ok, err := f.Gates.Approval(gate.Gate(g.key))
		if err != nil {
			return 0, err
		}
		extra := approvalDisplay(a, ok)
		if extra != "" {
			extra = "  " + extra
		}
		fmt.Fprintf(ctx.out, "  %-8s %s%s\n", g.key, g.val, extra)
	}
	ctx.Println(i18n.T("Bước:"))
	if len(ids) == 0 {
		ctx.Println(i18n.T("  (chưa có bước nào)"))
	}
	for _, id := range ids {
		r := recs[id]
		st, check := string(step.StepNotStarted), ""
		if r != nil {
			st = string(r.Status)
			if r.Check != nil {
				check = "  " + stepCheckSummary(*r.Check)
			}
		}
		fmt.Fprintf(ctx.out, "  %-4s %-12s%s\n", id, st, check)
	}
	next, ok, err := f.Gates.FirstNotOpen()
	if err != nil {
		return 0, err
	}
	if ok {
		ctx.Println(i18n.Tf("Cổng kế tiếp cần xử lý: %s", string(next)))
	}
	return ExitOK, nil
}

// listOpen: không session, không --feature, nhiều tính năng đang mở: liệt kê kèm session đang gắn thay vì chọn bừa.
func listOpen(ctx *Context, root string, open []model.FeatureKey, asJSON bool) (int, error) {
	bound, err := model.AllSessions(root)
	if err != nil {
		return 0, err
	}
	names := make([]string, 0, len(bound))
	for s := range bound {
		names = append(names, s)
	}
	sort.Strings(names)
	sessionsOf := func(k model.FeatureKey) []string {
		ss := []string{}
		for _, s := range names {
			if bound[s] == k {
				ss = append(ss, s)
			}
		}
		return ss
	}
	if asJSON {
		arr := make([]jsonObject, 0, len(open))
		for _, k := range open {
			arr = append(arr, jsonObject{{"feature", k.String()}, {"sessions", sessionsOf(k)}})
		}
		b, err := jsonMarshal(jsonObject{{"open", arr}})
		if err != nil {
			return 0, err
		}
		ctx.Println(string(b))
		return ExitOK, nil
	}
	ctx.Println(i18n.Tf("Có %s tính năng đang mở (không có session, dùng --feature <KEY> để xem một tính năng):", strconv.Itoa(len(open))))
	for _, k := range open {
		line := "  " + k.String()
		if ss := sessionsOf(k); len(ss) > 0 {
			line += "  session: " + strings.Join(ss, ", ")
		}
		ctx.Println(line)
	}
	return ExitOK, nil
}

// printRisk: mức rủi ro trong risk.yaml; khi dùng chính sách từ web thì nhắc nếu file chính sách trong repo khác
// bản trên web (theo cache đã ký).
func printRisk(ctx *Context, f *Feature) {
	r, ok, err := risk.ReadRiskFile(f.Paths.Risk())
	switch {
	case err != nil:
		ctx.Println(i18n.Tf("Rủi ro: không đọc được %s", f.Paths.Rel(f.Paths.Risk())))
	case ok:
		line := i18n.Tf("Rủi ro: %s (chính sách v%s)", r.Level, strconv.Itoa(r.Policy.Version))
		if r.Override != nil {
			line += i18n.Tf(", đổi tay bởi %s", r.Override.By)
		}
		ctx.Println(line)
	}
	if !policyFromWeb(f) {
		return
	}
	web, ok := f.Web.Policy()
	if !ok {
		ctx.Println(i18n.T("Chính sách rủi ro: chưa có bản từ web trên máy này; chạy `aisdlc policy pull`."))
		return
	}
	b, err := os.ReadFile(filepath.Join(f.Root, filepath.FromSlash(RepoPolicyFile)))
	if err != nil {
		return
	}
	a, err := risk.ParsePolicy(string(b))
	if err != nil {
		return
	}
	w, err := risk.ParsePolicy(web.YAML)
	if err != nil {
		return
	}
	w.Version = a.Version // so nội dung, bỏ qua số phiên bản (web đánh số riêng)
	if a.ToJSON() != w.ToJSON() {
		ctx.Println(i18n.Tf("Lưu ý: %s khác chính sách v%s trên web (web là nguồn). Tải YAML trên web rồi commit nếu muốn đồng bộ.", RepoPolicyFile, strconv.Itoa(web.Version)))
	}
}

// printControl: tạm dừng, lệnh chờ, chỉ dẫn chờ giao từ web (theo cache đã xác minh).
func printControl(ctx *Context, f *Feature) {
	if f.Control == nil {
		return
	}
	if p := f.Control.Pause; p != nil {
		at := "?"
		if !p.At.IsZero() {
			at = p.At.In(time.Local).Format("15:04 02/01/2006")
		}
		suffix := ""
		if _, ok := f.Web.Config(); !ok {
			suffix = i18n.Tf(" — chưa cấu hình web trên máy này; xóa .aisdlc/web/control/%s.json để bỏ tạm dừng cũ", f.Key.String())
		} else if f.ControlOffline {
			suffix = i18n.T(" — web không truy cập được, chưa nhận lệnh tiếp tục")
		}
		ctx.Println(i18n.Tf("Đang tạm dừng từ web (%s, %s)%s", p.By, at, suffix))
	}
	var pending []string
	for _, a := range f.PendingActions() {
		if a.Kind == "ALLOW_SCOPE" {
			pending = append(pending, i18n.Tf("cho phép %s ở %s", a.Path, a.Step))
		} else {
			pending = append(pending, "accept "+a.Step)
		}
	}
	if len(pending) > 0 {
		ctx.Println(i18n.Tf("Lệnh từ web chờ áp dụng: %s", strings.Join(pending, ", ")))
	}
	if n := len(f.UndeliveredInstructions()); n > 0 {
		ctx.Println(i18n.Tf("Chỉ dẫn chờ giao cho agent: %s", strconv.Itoa(n)))
	}
}

// warnLocalRepoID: web bật mà repo không có remote origin thì web định danh repo bằng tên thư mục (ra stderr, không
// làm hỏng --json).
func warnLocalRepoID(ctx *Context, f *Feature) {
	if _, ok := f.Web.Config(); !ok {
		return
	}
	if _, ok, err := f.Git().RemoteURL(ctx.std, "origin"); err != nil || ok {
		return
	}
	ctx.Eprintln(i18n.Tf("Cảnh báo: repo không có remote origin; web gom tính năng theo %s (tên thư mục). Thêm remote origin để định danh đúng repo.", f.Web.RepoID()))
}
