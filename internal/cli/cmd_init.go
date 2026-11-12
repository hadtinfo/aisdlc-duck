package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"aisdlc/internal/fsx"
	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
)

func newInitCmd(ctx *Context) *cobra.Command {
	var lite bool
	var title string
	c := &cobra.Command{
		Use:   "init KEY",
		Short: i18n.T("Tạo tính năng mới theo Jira key và đặt làm tính năng đang làm."),
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().BoolVar(&lite, "lite", false, i18n.T("Luồng nhẹ: bỏ BRD và thiết kế, chỉ có cases.yaml"))
	c.Flags().StringVar(&title, "title", "", i18n.T("Tiêu đề (mặc định lấy từ Jira)"))
	c.RunE = Wrap(ctx, func(cmd *cobra.Command, args []string) (int, error) {
		var t *string
		if cmd.Flags().Changed("title") {
			t = &title
		}
		return initFeature(ctx, args[0], lite, t)
	})
	return c
}

type templateFile struct{ path, template string }

func initFeature(ctx *Context, rawKey string, lite bool, title *string) (int, error) {
	root, err := ctx.RepoRoot()
	if err != nil {
		return 0, err
	}
	k, err := model.ParseFeatureKey(rawKey)
	if err != nil {
		return 0, err
	}
	paths := model.FeaturePaths{RepoRoot: root, Key: k}
	if exists(paths.Dir()) {
		return 0, abort(2, i18n.Tf("Tính năng %s đã tồn tại (%s). Dùng `aisdlc use %s`.", k.String(), paths.Rel(paths.Dir()), k.String()))
	}
	summary, err := jiraSummary(ctx, k)
	if err != nil {
		return 0, err
	}
	t := k.String()
	switch {
	case title != nil:
		t = *title
	case summary != "":
		t = summary
	}
	flow := model.FlowFull
	if lite {
		flow = model.FlowLite
	}
	baseTree, err := ctx.Git(root).SnapshotTree(ctx.std)
	if err != nil {
		return 0, err
	}

	var files []templateFile
	if flow == model.FlowFull {
		files = []templateFile{{paths.Brd(), "brd.md"}, {paths.DesignMd(), "design.md"},
			{paths.DesignYaml(), "design.yaml"}, {paths.Cases(), "cases.full.yaml"}}
	} else {
		files = []templateFile{{paths.Cases(), "cases.lite.yaml"}}
	}
	if err := os.MkdirAll(paths.Dir(), 0o755); err != nil {
		return 0, fmt.Errorf(i18n.T("Không tạo được %s: %v"), paths.Dir(), err)
	}
	if flow == model.FlowFull {
		if err := os.MkdirAll(paths.DbDrafts(), 0o755); err != nil {
			return 0, fmt.Errorf(i18n.T("Không tạo được %s: %v"), paths.DbDrafts(), err)
		}
	}
	for _, f := range files {
		content, err := template(i18n.Default(), f.template)
		if err != nil {
			return 0, err
		}
		if err := fsx.WriteAtomic(f.path, []byte(content), 0o644); err != nil {
			return 0, err
		}
	}
	meta := model.FeatureMeta{Key: k, Title: t, Flow: flow, BaseTree: baseTree, CreatedAt: ctx.Now().UTC(),
		Owner: owner(ctx, root), RiskSchema: riskSchemaMarker}
	if err := meta.Write(paths.Meta()); err != nil {
		return 0, err
	}
	resolver := NewFeatureResolver(ctx)
	if err := resolver.Bind(root, k); err != nil {
		return 0, err
	}

	ctx.Println(i18n.Tf("Đã tạo tính năng %s (luồng %s): %s", k.String(), string(flow), t))
	ctx.AfterCommandKey(root, k, "init")
	for _, f := range files {
		ctx.Println("  " + paths.Rel(f.path))
	}
	if err := resolver.WarnSameBranch(root); err != nil {
		return 0, err
	}
	if flow == model.FlowFull {
		ctx.Println("  " + paths.Rel(paths.DbDrafts()) + "/")
		ctx.Println(i18n.T("Tiếp theo: soạn brd.md rồi chạy `aisdlc submit brd`."))
	} else {
		ctx.Println(i18n.T("Tiếp theo: soạn cases.yaml (kèm scope, budget) rồi chạy `aisdlc submit cases`."))
	}
	return ExitOK, nil
}

// owner là git config user.email; trống hoặc lỗi thì "".
func owner(ctx *Context, root string) string {
	email, err := ctx.Git(root).UserEmail(ctx.std)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(email)
}

// jiraSummary kiểm tra ticket nếu có JIRA_URL và JIRA_TOKEN. 404 thì dừng; lỗi khác chỉ cảnh báo. Trả tiêu đề
// ticket ("" nếu không có).
func jiraSummary(ctx *Context, k model.FeatureKey) (string, error) {
	url, okU := ctx.Env("JIRA_URL")
	token, okT := ctx.Env("JIRA_TOKEN")
	if !okU || !okT {
		ctx.Eprintln(i18n.T("Cảnh báo: chưa đặt JIRA_URL và JIRA_TOKEN, bỏ qua kiểm tra ticket Jira."))
		return "", nil
	}
	warn := func(err error) (string, error) {
		ctx.Eprintln(i18n.Tf("Cảnh báo: không gọi được Jira (%s), bỏ qua kiểm tra ticket Jira.", err.Error()))
		return "", nil
	}
	base := strings.TrimRight(url, "/")
	req, err := http.NewRequestWithContext(ctx.std, http.MethodGet,
		base+"/rest/api/2/issue/"+k.String()+"?fields=summary", nil)
	if err != nil {
		return warn(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext},
		// không theo redirect: theo sẽ gửi lại Bearer sang địa chỉ khác; 3xx thành cảnh báo "Jira trả HTTP 3xx"
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return warn(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", abort(2, i18n.Tf("Không tìm thấy ticket %s trên Jira.", k.String()))
	}
	if resp.StatusCode != http.StatusOK {
		ctx.Eprintln(fmt.Sprintf(i18n.T("Cảnh báo: Jira trả HTTP %d cho %s, bỏ qua kiểm tra ticket Jira."),
			resp.StatusCode, k))
		return "", nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return warn(err)
	}
	var issue any
	if err := json.Unmarshal(body, &issue); err != nil {
		return warn(err)
	}
	ctx.Println(i18n.Tf("Jira: %s tồn tại.", k.String()))
	fields, _ := issue.(map[string]any)
	f, _ := fields["fields"].(map[string]any)
	if s, ok := f["summary"].(string); ok && strings.TrimSpace(s) != "" {
		return s, nil
	}
	return "", nil
}
