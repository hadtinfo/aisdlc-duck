package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
)

// loginTimeout: `web login` chờ /api/cli/pubkey tối đa 5 giây.
const loginTimeout = 5 * time.Second

// newWebCmd là lệnh web (WebCommand): status, push, flush (ẩn, hook dùng), login, approve-token, migrate.
func newWebCmd(ctx *Context) *cobra.Command {
	c := &cobra.Command{
		Use:   "web",
		Short: "Web nội bộ: status, push, login, approve-token, migrate.",
		Args:  cobra.NoArgs,
		RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
			fmt.Fprint(ctx.Err(), cmd.UsageString())
			return ExitUsage, nil
		}),
	}
	sub := func(name, short string, run func(*Context) (int, error)) *cobra.Command {
		return &cobra.Command{Use: name, Short: short, Args: cobra.NoArgs,
			RunE: Wrap(ctx, func(*cobra.Command, []string) (int, error) { return run(ctx) })}
	}
	c.AddCommand(
		sub("status", "Cho biết web đã bật chưa, số bản tin đang chờ, bị từ chối, lần gửi thành công cuối.", webStatus),
		sub("push", "Dựng snapshot mọi tính năng rồi gửi cả hàng đợi.", webPush),
		newWebFlushCmd(ctx),
		sub("login", "Chỉ dev: lưu URL web và token ingest, ghim khóa ký của server.", webLogin),
		sub("approve-token", "Chỉ dev: lưu token approve để duyệt từ terminal.", webApproveToken),
		sub("migrate", "Đưa các bản duyệt cục bộ (gates.json) còn khớp nội dung lên web, chờ chủ tính năng xác nhận.",
			webMigrate),
	)
	return c
}

func webStatus(ctx *Context) (int, error) {
	warnEnvToken(ctx)
	root, err := ctx.RepoRoot()
	if err != nil {
		return 0, err
	}
	web := "tắt"
	if c, ok := LoadWebConfig(ctx); ok {
		web = webclient.SafeURL(c.URL, c.Token) // không in userinfo (user:mật khẩu@) hay token
	}
	ob := webclient.NewOutbox(root)
	last, ok := webclient.LastOK(root)
	if !ok {
		last = "chưa"
	}
	ctx.Println("Web: " + web)
	ctx.Println("Đang chờ gửi: " + strconv.Itoa(ob.Size()))
	ctx.Println("Bị từ chối: " + strconv.Itoa(ob.RejectedCount()))
	ctx.Println("Gửi thành công lần cuối: " + last)
	return ExitOK, nil
}

func webPush(ctx *Context) (int, error) {
	root, err := ctx.RepoRoot()
	if err != nil {
		return 0, err
	}
	pub := ctx.Publisher(root)
	if !pub.Enabled() {
		return 0, abort(2, "Web chưa bật: chạy `aisdlc web login` (lưu URL web và token ingest tạo ở trang"+
			" /me vào ~/.config/aisdlc/web.json).")
	}
	keys, err := featureKeys(root)
	if err != nil {
		return 0, err
	}
	for _, k := range keys {
		stageFeature(ctx, root, k, "aisdlc web: bỏ qua ")
	}
	r := pub.Push(ctx.std)
	ctx.Println(fmt.Sprintf("Đã gửi %d, còn chờ %d", r.Sent, r.Pending))
	return ExitOK, nil
}

// stageFeature dựng tính năng k (như một lệnh: có thể làm mới từ web) rồi đặt snapshot vào outbox; lỗi dựng chỉ ra
// stderr với tiền tố skip.
func stageFeature(ctx *Context, root string, k model.FeatureKey, skip string) {
	f, err := guarded(func() (*Feature, error) { return ctx.FeatureAt(root, k) })
	if err != nil {
		ctx.Eprintln(skip + k.String() + ": " + errText(err))
		return
	}
	p := ctx.Publisher(root)
	p.Stage(k.String(), func() ([]byte, error) { return ctx.featureSnapshot(p, f) }, false)
}

func errText(err error) string {
	var a *Abort
	if asAbort(err, &a) {
		return a.Msg
	}
	return err.Error()
}

// newWebFlushCmd: tiến trình gửi nền do hook mở (WebCommand.flush). Gửi hết outbox (tối đa 5 giây), rồi làm mới chế
// độ, trạng thái cổng của tính năng đang làm (chế độ web), điều khiển từ web (cả chế độ local) và chính sách rủi ro:
// hook không gọi mạng nên cache được làm mới ở đây. `--feature KEY` (ẩn, hook truyền tính năng của phiên) làm mới
// thêm tính năng đó ngoài .aisdlc/active. Không phân tích cờ (DisableFlagParsing): đối số lạ hay sai đều bị bỏ qua.
// Không in gì (kể cả cảnh báo cấu hình và lỗi gửi), luôn exit 0.
func newWebFlushCmd(ctx *Context) *cobra.Command {
	return &cobra.Command{Use: "flush", Short: "Gửi outbox ở nền (hook dùng).", Hidden: true, Args: cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: Wrap(ctx, func(_ *cobra.Command, args []string) (int, error) {
			defer func() { _ = recover() }()
			ctx.out, ctx.err = io.Discard, io.Discard // tiến trình nền: im lặng
			root, ok := FindRepo(ctx.Cwd())
			if !ok {
				return ExitOK, nil
			}
			_ = ctx.newPublisher(root, io.Discard, false).Drain(ctx.Std())
			flushRefresh(ctx, root, flushFeatureArg(args))
			return ExitOK, nil
		})}
}

// flushFeatureArg là KEY của `--feature KEY` hoặc `--feature=KEY` trong args ("" nếu không có hay không hợp lệ).
func flushFeatureArg(args []string) model.FeatureKey {
	for i, a := range args {
		v, ok := "", false
		if a == "--feature" && i+1 < len(args) {
			v, ok = args[i+1], true
		} else if after, found := strings.CutPrefix(a, "--feature="); found {
			v, ok = after, true
		}
		if !ok {
			continue
		}
		if k, err := model.ParseFeatureKey(v); err == nil {
			return k
		}
		return ""
	}
	return ""
}

// flushRefresh làm mới chế độ, trạng thái cổng (chế độ web) và điều khiển từ web của tính năng đang làm
// (.aisdlc/active) và của extra (tính năng của phiên đã mở tiến trình này, có thể khác active), rồi chính sách từ
// web; im lặng.
func flushRefresh(ctx *Context, root string, extra model.FeatureKey) {
	defer func() { _ = recover() }()
	w := NewWebCache(ctx, root)
	if _, ok := w.Config(); !ok || !w.LoggedIn() {
		return
	}
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		return
	}
	w.RefreshMode(cfg)
	var keys []model.FeatureKey
	if k, ok, err := model.ReadActive(root); err == nil && ok {
		keys = append(keys, k)
	}
	if extra != "" && (len(keys) == 0 || keys[0] != extra) {
		keys = append(keys, extra)
	}
	for _, k := range keys {
		if !exists(model.FeaturePaths{RepoRoot: root, Key: k}.Meta()) {
			continue
		}
		if w.CachedMode(cfg) == gate.ModeWeb {
			w.Refresh(k)
		}
		NewWebControl(w).Refresh(k)
	}
	w.PolicyPullQuietly()
}

func webLogin(ctx *Context) (int, error) {
	home, ok := ctx.Home()
	if !ok {
		return 0, abort(2, "Không xác định được HOME.")
	}
	warnEnvToken(ctx)
	def := ""
	if c, ok := LoadWebConfig(ctx); ok {
		def = c.URL
	}
	prompt := "URL web: "
	if def != "" {
		prompt = "URL web [" + def + "]: "
	}
	u, err := ctx.AskDev(prompt)
	if err != nil {
		return 0, err
	}
	if u == "" {
		u = def
	}
	u = strings.TrimRight(u, "/")
	if u == "" {
		return 0, abort(2, "Thiếu URL web.")
	}
	token, err := ctx.AskDevSecret("Token ingest (tạo ở trang /me): ")
	if err != nil {
		return 0, err
	}
	if token == "" {
		return 0, abort(2, "Thiếu token.")
	}
	client := &webclient.Client{BaseURL: u, Token: token, Now: ctx.now}
	std, cancel := context.WithTimeout(ctx.std, loginTimeout)
	defer cancel()
	r, err := client.Do(std, http.MethodGet, "/api/cli/pubkey", nil)
	if err != nil {
		return 0, abort(2, err.Error()) // "Không kết nối được web <url>: ..." (client đã bỏ token)
	}
	if r.Status == http.StatusUnauthorized || r.Status == http.StatusForbidden {
		return 0, abort(2, "Token không dùng được cho CLI (cần token ingest).")
	}
	if !r.OK() {
		return 0, abort(2, "Web trả HTTP "+strconv.Itoa(r.Status)+" cho /api/cli/pubkey.")
	}
	var n map[string]any
	if err := json.Unmarshal(r.Body, &n); err != nil {
		return 0, abort(2, "Phản hồi /api/cli/pubkey không hợp lệ: "+err.Error())
	}
	key, err := snapshot.PublicKeyFromBase64(jsonText(n["publicKey"]))
	if err != nil {
		return 0, abort(2, "Phản hồi /api/cli/pubkey không hợp lệ: "+err.Error())
	}
	endorsement, _ := n["endorsement"].(string)
	if _, err := webclient.AcceptKey(home, key, endorsement); err != nil {
		if errors.Is(err, webclient.ErrKeyChanged) {
			return 0, abort(2, err.Error()+"; không lưu gì. Báo quản trị web.")
		}
		return 0, err
	}
	if err := webclient.SaveConfig(home, webclient.Config{URL: u, Token: token}); err != nil {
		return 0, err
	}
	ctx.Println("Đã lưu " + webclient.ConfigFile(home) + " và ghim khóa web " + snapshot.KeyID(key) + ".")
	return ExitOK, nil
}

func webApproveToken(ctx *Context) (int, error) {
	home, ok := ctx.Home()
	if !ok {
		return 0, abort(2, "Không xác định được HOME.")
	}
	token, err := ctx.AskDevSecret("Token approve (tạo ở trang /me): ")
	if err != nil {
		return 0, err
	}
	if token == "" {
		return 0, abort(2, "Thiếu token.")
	}
	if err := webclient.SaveApproveToken(home, token); err != nil {
		return 0, err
	}
	ctx.Println("Đã lưu " + webclient.ApproveTokenFile(home) + ".")
	return ExitOK, nil
}

func webMigrate(ctx *Context) (int, error) {
	root, err := ctx.RepoRoot()
	if err != nil {
		return 0, err
	}
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		return 0, err
	}
	web := NewWebCache(ctx, root)
	if web.Mode(cfg) != gate.ModeWeb {
		return 0, abort(2, "Repo chưa ở chế độ duyệt trên web: đặt `approvals: web` trong "+
			"aisdlc/config.yaml trước.")
	}
	keys, err := featureKeys(root)
	if err != nil {
		return 0, err
	}
	for _, k := range keys {
		stageFeature(ctx, root, k, "aisdlc web: bỏ qua snapshot ") // web cần snapshot (luồng) của tính năng
	}
	ctx.Publisher(root).Push(ctx.std)
	sent := 0
	for _, k := range keys {
		if !exists(model.FeaturePaths{RepoRoot: root, Key: k}.Gates()) {
			continue
		}
		f, err := ctx.FeatureAt(root, k)
		if err != nil {
			return 0, err
		}
		n, err := web.Migrate(f)
		sent += n
		if err != nil {
			return 0, err
		}
	}
	ctx.Println("Đã gửi " + strconv.Itoa(sent) + " bản duyệt. Chủ tính năng xác nhận trên trang B.")
	return ExitOK, nil
}

// featureKeys là mọi tính năng trong aisdlc/features (có feature.yaml), sắp theo tên.
func featureKeys(root string) ([]model.FeatureKey, error) {
	dir := filepath.Join(root, "aisdlc", "features")
	es, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var out []model.FeatureKey
	for _, n := range names {
		if st, err := os.Stat(filepath.Join(dir, n, "feature.yaml")); err != nil || !st.Mode().IsRegular() {
			continue
		}
		if k, err := model.ParseFeatureKey(n); err == nil { // tên thư mục không phải feature key: bỏ qua
			out = append(out, k)
		}
	}
	return out, nil
}
