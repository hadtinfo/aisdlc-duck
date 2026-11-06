package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
)

// Phần setup/init/use/status của CliFlowTest và StatusControlTest.

func readMeta(t *testing.T, r *testRepo, key string) model.FeatureMeta {
	t.Helper()
	m, err := model.ReadFeatureMeta(filepath.Join(r.root, "aisdlc/features", key, "feature.yaml"))
	must(t, err)
	return m
}

func TestInitFullFlowCreatesFeature(t *testing.T) { // phần init của fullFlowHappyPathWithoutMaven
	r := newTestRepo(t)
	eq(t, r.run("setup"), 0)
	eq(t, r.run("init", "PAY-1"), 0)
	m := readMeta(t, r, "PAY-1")
	eq(t, m.Flow, model.FlowFull)
	eq(t, len(m.BaseTree), 40)
	eq(t, m.CreatedAt.Equal(testNow), true)
	eq(t, m.Owner, "dev@example.com")
	eq(t, r.exists("aisdlc/features/PAY-1/design.yaml"), true)
	if st, err := os.Stat(filepath.Join(r.root, "aisdlc/features/PAY-1/db")); err != nil || !st.IsDir() {
		t.Fatalf("thiếu thư mục db: %v", err)
	}
	eq(t, strings.TrimSpace(r.read(".aisdlc/active")), "PAY-1")
	contains(t, r.out, "Đã tạo tính năng PAY-1 (luồng FULL): PAY-1",
		"  aisdlc/features/PAY-1/brd.md", "  aisdlc/features/PAY-1/db/",
		"Tiếp theo: soạn brd.md rồi chạy `aisdlc submit brd`.")
}

func TestSetupIsIdempotent(t *testing.T) {
	r := newTestRepo(t)
	r.write("CLAUDE.md", "# Repo cũ\n")
	r.write(".gitignore", "target/\n")
	eq(t, r.run("setup"), 0)
	contains(t, r.out, "SIGTERM", "healthPath")
	claude := r.read("CLAUDE.md")
	cfg := r.read("aisdlc/config.yaml")
	eq(t, r.run("setup"), 0)

	eq(t, r.read("CLAUDE.md"), claude)
	if !strings.HasPrefix(claude, "# Repo cũ\n") {
		t.Fatalf("CLAUDE.md: %s", claude)
	}
	eq(t, strings.Count(claude, "<!-- aisdlc -->"), 1)
	n := 0
	for _, l := range strings.Split(r.read(".gitignore"), "\n") {
		if l == ".aisdlc/" {
			n++
		}
	}
	eq(t, n, 1)
	eq(t, r.read("aisdlc/config.yaml"), cfg)
	contains(t, cfg, "language: go", "migrations:")
	eq(t, r.exists("src/test/java"), false)
	contains(t, r.read("aisdlc/gitlab-ci.aisdlc.yml"), "aisdlc-eval",
		"aisdlc eval --ci --base origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME")
	notContains(t, r.read("aisdlc/gitlab-ci.aisdlc.yml"), "java", "maven")
	contains(t, r.out, "Giữ nguyên aisdlc/config.yaml (đã có).", "Giữ nguyên CLAUDE.md (đã có mục AISDLC).",
		"Giữ nguyên .gitignore (đã có .aisdlc/).")
}

// --package của bản Java không còn (spec §3.1): cờ lạ là lỗi dùng lệnh, exit 2.
func TestSetupRejectsInvalidPackage(t *testing.T) {
	r := newTestRepo(t)
	eq(t, r.run("setup", "--package", "com..x"), 2)
	eq(t, r.exists("aisdlc/config.yaml"), false)
}

func TestInitWithoutJiraEnvWarns(t *testing.T) {
	r := newTestRepo(t)
	r.run("setup")
	eq(t, r.run("init", "PAY-1", "--lite", "--title", "Sửa lỗi"), 0)
	contains(t, r.out+r.err, "JIRA_URL", "bỏ qua kiểm tra")
	m := readMeta(t, r, "PAY-1")
	eq(t, m.Flow, model.FlowLite)
	eq(t, m.Title, "Sửa lỗi")
	contains(t, r.read("aisdlc/features/PAY-1/cases.yaml"), "budget")
	eq(t, r.exists("aisdlc/features/PAY-1/brd.md"), false)
	contains(t, r.out, "Tiếp theo: soạn cases.yaml (kèm scope, budget) rồi chạy `aisdlc submit cases`.")
}

func TestInitExistingFeatureOrBadKeyExits2(t *testing.T) {
	r := newTestRepo(t)
	r.run("setup")
	eq(t, r.run("init", "PAY-1"), 0)
	eq(t, r.run("init", "PAY-1"), 2)
	contains(t, r.err, "Tính năng PAY-1 đã tồn tại (aisdlc/features/PAY-1). Dùng `aisdlc use PAY-1`.")
	eq(t, r.run("init", "pay-1"), 2)
}

func TestInitWithoutSetupExits2(t *testing.T) {
	r := newTestRepo(t)
	eq(t, r.run("init", "PAY-1"), 2)
	contains(t, r.err, "aisdlc setup")
}

func TestInitChecksJiraTicketWithBearerToken(t *testing.T) {
	var auth, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		auth = req.Header.Get("Authorization")
		query = req.URL.RawQuery
		if strings.HasSuffix(req.URL.Path, "/rest/api/2/issue/PAY-1") {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"key":"PAY-1","fields":{"summary":"Hạn mức chuyển tiền"}}`))
			return
		}
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
	}))
	defer srv.Close()
	r := newTestRepo(t)
	r.env["JIRA_URL"] = srv.URL + "/"
	r.env["JIRA_TOKEN"] = "tok"
	r.run("setup")

	if c := r.run("init", "PAY-1"); c != 0 {
		t.Fatalf("exit %d: %s%s", c, r.out, r.err)
	}
	eq(t, auth, "Bearer tok")
	eq(t, query, "fields=summary")
	eq(t, readMeta(t, r, "PAY-1").Title, "Hạn mức chuyển tiền")
	contains(t, r.out, "Jira: PAY-1 tồn tại.")

	eq(t, r.run("init", "PAY-2"), 2)
	contains(t, r.err, "Không tìm thấy ticket")
	eq(t, r.exists("aisdlc/features/PAY-2"), false)
}

func TestInitJiraUnreachableWarnsAndContinues(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	r := newTestRepo(t)
	r.env["JIRA_URL"] = url
	r.env["JIRA_TOKEN"] = "tok"
	r.run("setup")
	eq(t, r.run("init", "PAY-1"), 0)
	contains(t, r.err, "Cảnh báo: không gọi được Jira (", "bỏ qua kiểm tra ticket Jira.")
	eq(t, readMeta(t, r, "PAY-1").Title, "PAY-1")
}

func TestUseSwitchesActiveFeature(t *testing.T) {
	r := newTestRepo(t)
	r.run("setup")
	r.run("init", "PAY-1")
	r.run("init", "PAY-2", "--lite")
	eq(t, strings.TrimSpace(r.read(".aisdlc/active")), "PAY-2")
	eq(t, r.run("use", "PAY-1"), 0)
	contains(t, r.out, "Đang làm tính năng PAY-1 (FULL).")
	eq(t, strings.TrimSpace(r.read(".aisdlc/active")), "PAY-1")
	eq(t, r.run("use", "PAY-9"), 2)
	contains(t, r.err, "Không có tính năng PAY-9 (thiếu aisdlc/features/PAY-9/feature.yaml).")
}

// Java chạy thêm submit brd, step start S1 và eval; các lệnh đó có ở task 5 và 6.
func TestActiveFeatureMissingOnThisBranchGivesReadableMessage(t *testing.T) {
	r := newTestRepo(t)
	r.run("setup")
	r.run("init", "PAY-1")
	must(t, os.RemoveAll(filepath.Join(r.root, "aisdlc/features/PAY-1")))
	for _, cmd := range [][]string{{"status"}, {"submit", "brd"}, {"step", "start", "S1"}, {"eval"}} {
		eq(t, r.run(cmd...), 2)
		contains(t, r.err, "PAY-1", "nhánh này", "aisdlc use <KEY>", "aisdlc init <KEY>")
	}
	if c := r.run("init", "PAY-2"); c != 0 {
		t.Fatalf("%s%s", r.out, r.err)
	}
	if c := r.run("status"); c != 0 {
		t.Fatal(r.err)
	}
}

func TestStatusWarnsWhenWebEnabledAndNoOriginRemote(t *testing.T) {
	r := newTestRepo(t)
	r.run("setup")
	r.run("init", "PAY-1")
	eq(t, r.run("status"), 0)
	notContains(t, r.err, "remote origin") // web tắt: không cảnh báo
	r.env["AISDLC_WEB_URL"] = "http://127.0.0.1:1"
	r.env["AISDLC_WEB_TOKEN"] = "x"
	eq(t, r.run("status"), 0)
	n := 0
	for _, l := range strings.Split(r.err, "\n") {
		if strings.Contains(l, "remote origin") {
			n++
			contains(t, l, "local/")
		}
	}
	eq(t, n, 1)
	eq(t, r.run("status", "--json"), 0)
	var v map[string]any
	must(t, json.Unmarshal([]byte(r.out), &v)) // stdout vẫn là JSON
	r.gitCmd("remote", "add", "origin", "git@gitlab.corp:pay/svc.git")
	eq(t, r.run("status"), 0)
	notContains(t, r.err, "remote origin")
}

// Phần help của helpListsCommands; task 5–7 thêm submit, approve, step, hook, diff khi có các lệnh đó.
func TestHelpListsCommands(t *testing.T) {
	r := newTestRepo(t)
	eq(t, r.run("--help"), 0)
	contains(t, r.out, "setup", "init", "use", "status", "submit", "approve", "reject", "reopen", "review", "step",
		"scope", "diff", "risk", "policy", "eval", "web", "hook")
}

func TestStatusTextAndJSONOfFreshFeature(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	eq(t, r.run("status"), 0)
	if !strings.HasPrefix(r.out, "[PAY-1]\nTính năng PAY-1 (FULL): PAY-1\n"+
		"Duyệt trong Claude Code: tắt (không đọc được aisdlc/config.yaml ở origin/main)\n") {
		t.Fatalf("got:\n%s", r.out)
	}
	contains(t, r.out, "Cổng:\n  BRD      NOT_SUBMITTED\n  DESIGN   LOCKED_BY_PREVIOUS\n",
		"Bước:\n", "Cổng kế tiếp cần xử lý: BRD\n")
	eq(t, r.run("status", "--json"), 0)
	var v struct {
		Feature, Title, Flow string
		SessionApprove       bool
		Gates                map[string]string
		Steps                map[string]string
		Checks               map[string]any
	}
	must(t, json.Unmarshal([]byte(r.out), &v))
	eq(t, v.Feature, "PAY-1")
	eq(t, v.Flow, "FULL")
	eq(t, v.Gates["BRD"], "NOT_SUBMITTED")
	eq(t, v.Gates["REVIEW"], "LOCKED_BY_PREVIOUS")
	if !strings.HasPrefix(r.out, `{"feature":"PAY-1","title":"PAY-1","flow":"FULL","sessionApprove":false,"gates":{"BRD":`) {
		t.Fatalf("thứ tự khóa JSON khác Java: %s", r.out)
	}
	notContains(t, r.out, "[PAY-1]")
}

func TestStatusShowsRiskLevel(t *testing.T) {
	r := newTestRepo(t)
	r.setupInit()
	r.write("aisdlc/features/PAY-1/risk.yaml", "level: high\npolicy: { version: 3, hash: x }\nmatched: []\n"+
		"agent: null\noverride: { from: low, to: high, by: Lan, reason: r, at: 2026-11-01T00:00:00Z }\nrequirements: []\n")
	eq(t, r.run("status"), 0)
	contains(t, r.out, "Rủi ro: high (chính sách v3), đổi tay bởi Lan")
	r.write("aisdlc/features/PAY-1/risk.yaml", "level: [\n")
	eq(t, r.run("status"), 0)
	contains(t, r.out, "Rủi ro: không đọc được aisdlc/features/PAY-1/risk.yaml")
}

// StatusControlTest (phần đọc cache; pausedOfflineMessage ở cli_control_test.go).

func newControlFx(t *testing.T) (*testRepo, func(c snapshot.FeatureControl)) {
	r := newTestRepo(t)
	r.setupInit()
	home := t.TempDir()
	r.env["HOME"] = home
	_, priv := pinKey(t, home)
	repoID := snapshot.RepoID(context.Background(), gitx.New(r.root), r.root)
	write := func(c snapshot.FeatureControl) {
		c.Repo = repoID
		b, err := c.ToJSON()
		must(t, err)
		r.writeSigned(".aisdlc/web/control/PAY-1.json", b, priv)
	}
	return r, write
}

func TestPausedWithoutWebConfigShowsRecoveryHint(t *testing.T) {
	r, write := newControlFx(t)
	// khóa ghim còn (cache xác minh được) nhưng không có web.json và không có biến môi trường web
	write(snapshot.FeatureControl{Feature: "PAY-1", IssuedAt: testNow, Pause: &snapshot.Pause{By: "an", At: testNow}})
	eq(t, r.run("status"), 0)
	contains(t, r.out, "Đang tạm dừng từ web (an,",
		"chưa cấu hình web trên máy này; xóa .aisdlc/web/control/PAY-1.json để bỏ tạm dừng cũ")
	notContains(t, r.out, "web không truy cập được")
}

func TestNotPausedPrintsNothingExtra(t *testing.T) {
	r, _ := newControlFx(t)
	eq(t, r.run("status"), 0)
	notContains(t, r.out, "tạm dừng", "Chỉ dẫn")
}

func TestStatusShowsPendingActionsAndInstructionsFromCache(t *testing.T) {
	r, write := newControlFx(t)
	exp := testNow.Add(24 * time.Hour)
	write(snapshot.FeatureControl{Feature: "PAY-1", IssuedAt: testNow,
		Instructions: []snapshot.Instruction{{ID: 3, By: "an", At: testNow, Text: "x"}, {ID: 4, By: "an", At: testNow, Text: "y"}},
		Actions: []snapshot.Action{
			{ID: 9, Kind: "ACCEPT_STEP", Step: "S1", EndTree: "t", By: "an", At: testNow, ExpiresAt: &exp},
			{ID: 10, Kind: "ALLOW_SCOPE", Step: "S1", Path: "a/b.go", By: "an", At: testNow, ExpiresAt: &exp},
		}})
	r.write(".aisdlc/web/control/PAY-1.local.json", `{"delivered":[4],"results":{"10":"APPLIED"}}`)
	eq(t, r.run("status"), 0)
	// S1 chưa làm: lệnh 9 STALE ngay khi dựng tính năng (Java parity), 10 đã xử lý từ trước → không còn lệnh chờ
	contains(t, r.out, "Chỉ dẫn chờ giao cho agent: 1\n")
	contains(t, r.err, "Lệnh từ web hết hiệu lực: Bước S1 chưa được làm\n")
	notContains(t, r.out, "tạm dừng", "Lệnh từ web chờ áp dụng")
	eq(t, r.read(".aisdlc/web/control/PAY-1.local.json"),
		`{"delivered":[4],"results":{"10":"APPLIED","9":"STALE"}}`)
	// lệnh chưa có trong sổ (vd. lúc dựng không lấy được khóa): printControl liệt kê
	f := r.feature()
	r.write(".aisdlc/web/control/PAY-1.local.json", `{"delivered":[4],"results":{"10":"APPLIED"}}`)
	o := &bytes.Buffer{}
	printControl(r.context(noConsole(), "", o, &bytes.Buffer{}), f)
	contains(t, o.String(), "Lệnh từ web chờ áp dụng: accept S1\n", "Chỉ dẫn chờ giao cho agent: 1\n")
	// cache ký bằng khóa khác: không tin, không in gì
	_, other := pinKey(t, t.TempDir())
	b, err := (snapshot.FeatureControl{Repo: "x", Feature: "PAY-1", Pause: &snapshot.Pause{By: "an"}}).ToJSON()
	must(t, err)
	r.writeSigned(".aisdlc/web/control/PAY-1.json", b, other)
	eq(t, r.run("status"), 0)
	notContains(t, r.out, "tạm dừng", "Lệnh từ web", "Chỉ dẫn")
}

// Chế độ web theo phong bì mode.json đã ký (WebGates.cachedMode): không cần config.yaml nói web.
func TestStatusWebModeFromSignedModeCache(t *testing.T) {
	r, _ := newControlFx(t)
	repoID := snapshot.RepoID(context.Background(), gitx.New(r.root), r.root)
	home := r.env["HOME"]
	_, priv := pinKey(t, home) // ghim lại khóa mới để ký mode.json
	r.writeSigned(".aisdlc/web/mode.json", []byte(`{"repo":"`+repoID+`","mode":"web"}`), priv)
	eq(t, r.run("status"), 0)
	contains(t, r.out, "Duyệt: trên web\n")
	// mode.json của repo khác: theo config.yaml (local)
	r.writeSigned(".aisdlc/web/mode.json", []byte(`{"repo":"khac","mode":"web"}`), priv)
	eq(t, r.run("status"), 0)
	notContains(t, r.out, "Duyệt: trên web")
	// config nói web mà máy chưa đăng nhập: nhắc
	must(t, os.Remove(PinnedKeyFile(home)))
	r.write("aisdlc/config.yaml", r.read("aisdlc/config.yaml")+"approvals: web\n")
	eq(t, r.run("status"), 0)
	contains(t, r.out, "Duyệt: trên web (chưa `aisdlc web login`)")
}
