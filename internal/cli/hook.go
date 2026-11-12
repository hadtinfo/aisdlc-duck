package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/policy"
)

// Hook của Claude Code (HookCommand của Java): đọc JSON từ stdin, gọi policy, in quyết định. Luôn exit 0 như bản
// Java (Claude Code chỉ đọc JSON trên stdout khi exit 0). Ngoài repo AISDLC: không in gì. Trong repo AISDLC mà lỗi
// (kể cả panic): PreToolUse của tool ghi thì deny (đóng), sự kiện khác thì im lặng. Hook không bao giờ gọi mạng
// (Context.Offline): chế độ web chỉ đọc cache đã ký; event chỉ ghi vào outbox rồi mở `web flush` nền (hook_web.go).

var (
	// guardedTools: tool ghi, xét đủ luật.
	guardedTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true, "Bash": true}
	// readerTools: tool đọc, chỉ xét luật "không chạm ~/.config/aisdlc".
	readerTools = map[string]bool{"Read": true, "Grep": true, "Glob": true}
	pathFields  = []string{"file_path", "notebook_path", "path", "pattern", "glob"}
	// envSessionID: id ghi được vào CLAUDE_ENV_FILE mà không cần trích dẫn.
	envSessionID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	// headSession, headCwd: như clear_grant của plugin/bin/aisdlc, đọc ở phần trước "tool_input" khi JSON hỏng.
	headSession = regexp.MustCompile(`"session_id"\s*:\s*"([^"]+)"`)
	headCwd     = regexp.MustCompile(`"cwd"\s*:\s*"([^"]+)"`)
)

// hookInput là JSON đã đọc (json nil nếu lỗi, kèm errMsg); cwd lấy từ JSON, thiếu thì thư mục của tiến trình.
// subagent: input có agent_id khác rỗng (Claude Code chỉ đặt khi hook chạy cho tool của một subagent).
type hookInput struct {
	json     map[string]any
	errMsg   string
	raw      string
	cwd      string
	subagent bool
}

// hook là một lần gọi hook (mỗi lần gọi là một tiến trình): giữ kết quả phân giải tính năng để không làm lại.
type hook struct {
	ctx      *Context
	resolved Resolution
	resRoot  string
}

func newHookCmd(ctx *Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: i18n.T("Dùng cho hook của plugin Claude Code (đọc JSON từ stdin)."),
		Args:  cobra.NoArgs,
		RunE: Wrap(ctx, func(cmd *cobra.Command, _ []string) (int, error) {
			fmt.Fprint(ctx.err, cmd.UsageString())
			return ExitUsage, nil
		}),
	}
	event := func(use, short string, aliases []string, run func(h *hook) int) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, Aliases: aliases, Args: cobra.NoArgs,
			RunE: Wrap(ctx, func(*cobra.Command, []string) (int, error) {
				return runHookEvent(ctx, use == "pre-tool-use", run), nil
			})}
	}
	cmd.AddCommand(
		event("pre-tool-use", i18n.T("Hook PreToolUse: chặn ghi sai quy trình."), []string{"pre-tool-use-read"},
			(*hook).preToolUse),
		event("stop", i18n.T("Hook Stop: không cho agent dừng giữa bước."), nil, (*hook).stop),
		event("session-start", i18n.T("Hook SessionStart: nạp quy tắc và trạng thái vào ngữ cảnh."), nil,
			(*hook).sessionStart),
		event("post-tool-use", i18n.T("Hook PostToolUse: giao chỉ dẫn từ web cho agent, gửi event sửa file lên web."), nil,
			(*hook).postToolUse),
		event("user-prompt-submit", i18n.T("Hook UserPromptSubmit: làm mới điều khiển từ web ở nền, giao chỉ dẫn chưa giao."),
			nil, (*hook).userPromptSubmit),
	)
	return cmd
}

// runHookEvent chạy một sự kiện hook và chặn panic lọt khỏi phần recover riêng của sự kiện: PreToolUse vẫn đóng
// (in "Lỗi: …" và exit 2, Claude Code chặn tool), sự kiện khác im lặng exit 0 (không chặn được dừng hay mở
// session thì thôi).
func runHookEvent(ctx *Context, preToolUse bool, run func(h *hook) int) (code int) {
	defer func() {
		if p := recover(); p != nil {
			if preToolUse {
				fmt.Fprintf(ctx.err, i18n.T("Lỗi: %v\n"), p)
				code = ExitUsage
				return
			}
			code = ExitOK
		}
	}()
	return run(&hook{ctx: ctx})
}

func (h *hook) read() *hookInput {
	h.ctx.Offline() // hook không bao giờ gọi mạng
	in := &hookInput{cwd: h.ctx.Cwd()}
	b, err := io.ReadAll(h.ctx.In())
	in.raw = string(b)
	if err != nil {
		in.errMsg = err.Error()
	} else {
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			in.errMsg = err.Error()
		} else if m, ok := v.(map[string]any); ok {
			in.json = m
		} else {
			in.errMsg = i18n.T("không phải object JSON")
		}
	}
	if in.json != nil {
		if c := text(in.json, "cwd"); c != "" && strings.TrimSpace(c) != "" {
			in.cwd = h.absCwd(c)
		}
		h.ctx.WithSessionID(text(in.json, "session_id"))
		in.subagent = strings.TrimSpace(text(in.json, "agent_id")) != ""
	} else if m := headCwd.FindStringSubmatch(in.head()); m != nil {
		in.cwd = h.absCwd(m[1])
	}
	// ngôn ngữ theo repo mà hook đang xét (cwd của phiên agent), không theo thư mục tiến trình; lỗi thì giữ locale đã
	// chọn lúc khởi động để hook vẫn fail-open.
	if l, err := localeAt(h.ctx, in.cwd); err == nil {
		i18n.SetDefault(l)
	}
	return in
}

func (h *hook) absCwd(c string) string {
	if !filepath.IsAbs(c) {
		c = filepath.Join(h.ctx.Cwd(), c)
	}
	return filepath.Clean(c)
}

// head là phần input trước "tool_input" (session_id, cwd do Claude Code đặt, agent không chèn được vào đây).
func (in *hookInput) head() string {
	head, _, _ := strings.Cut(in.raw, `"tool_input"`)
	return head
}

// grantSession là session có phiếu cần xóa: session_id của JSON, hoặc (JSON hỏng) đọc ở phần đầu input như
// clear_grant của plugin/bin/aisdlc.
func (h *hook) grantSession(in *hookInput) string {
	if in.json != nil {
		return h.ctx.SessionID()
	}
	if m := headSession.FindStringSubmatch(in.head()); m != nil {
		return truncSession(m[1])
	}
	return ""
}

// preToolUse: mọi tool. Mọi PreToolUse của session không phải lần hỏi lệnh chỉ dev vừa cấp phiếu đều xóa phiếu chưa
// dùng của session (hợp luật của HookCommand và clear_grant của plugin): dev bấm No rồi agent thử lại thì gặp hộp
// xác nhận mới; lệnh duyệt chạy song song với tool khác thì không còn phiếu.
func (h *hook) preToolUse() int {
	in := h.read()
	root, found := FindRepo(in.cwd)
	issued := false
	defer func() {
		if !issued && found {
			_ = policy.ClearGrant(root, h.grantSession(in))
		}
	}()
	inRepo := found
	if !found && in.json == nil {
		// JSON hỏng: như Java, "trong repo AISDLC" cũng tính theo cwd của tiến trình (cwd trích bằng regex chỉ dùng
		// cho ClearGrant)
		_, inRepo = FindRepo(h.ctx.Cwd())
	}
	out, ok := h.decide(in, root, inRepo)
	if ok {
		issued = out.decision == "ask"
		h.printDecision(out)
	}
	return ExitOK
}

type preDecision struct{ decision, reason string }

// decide trả quyết định cần in (ok=false: im lặng, tức cho phép). Panic sau khi đã biết repo → deny (fail closed).
func (h *hook) decide(in *hookInput, root string, found bool) (d preDecision, ok bool) {
	tool := ""
	if in.json != nil {
		tool = text(in.json, "tool_name")
	}
	// thư mục cấu hình web của dev (token, khóa ghim): chặn ở mọi repo, kể cả ngoài repo AISDLC
	if (guardedTools[tool] || readerTools[tool]) && in.json != nil {
		if reason, denied := h.webConfigDenied(in); denied {
			return preDecision{"deny", reason}, true
		}
	}
	if !found {
		return preDecision{}, false
	}
	if in.json == nil {
		return preDecision{"deny", i18n.Tf("aisdlc lỗi: không đọc được JSON của hook (%s)", oneLine(in.errMsg))}, true
	}
	if tool != "" && !guardedTools[tool] {
		return preDecision{}, false
	}
	defer func() {
		if p := recover(); p != nil {
			d, ok = preDecision{"deny", i18n.Tf("aisdlc lỗi: %s", oneLine(fmt.Sprint(p)))}, true
		}
	}()
	d, ok, err := h.guarded(in, root, tool)
	if err != nil {
		return preDecision{"deny", i18n.Tf("aisdlc lỗi: %s", oneLine(err.Error()))}, true
	}
	return d, ok
}

// guarded xét tool ghi; lỗi trả về thành "aisdlc lỗi: <msg>".
func (h *hook) guarded(in *hookInput, root, tool string) (preDecision, bool, error) {
	if tool == "" {
		return preDecision{}, false, fmt.Errorf("%s", i18n.T("thiếu tool_name"))
	}
	ti, _ := in.json["tool_input"].(map[string]any)
	file := text(ti, "file_path")
	if file == "" {
		file = text(ti, "notebook_path")
	}
	command := text(ti, "command")
	if tool == "Bash" && policy.DevCommandMentions(command) {
		cfg, err := model.LoadRepoConfig(root)
		if err != nil {
			return preDecision{}, false, err
		}
		if model.ReadSessionApprove(h.ctx.Std(), h.ctx.Git(root), cfg).Enabled {
			return h.devOnly(root, in, command)
		}
	}
	st, f, err := h.state(root)
	if err != nil {
		return preDecision{}, false, err
	}
	call := policy.ToolCall{ToolName: tool, FilePath: file, Command: command, Cwd: in.cwd}
	dec := policy.PreToolUse(call, st)
	if !dec.Allow && dec.Code == "PAUSED" && st.Active != "" && h.awaitFlush(root, st.Active) {
		// web flush đang chạy (vd. do UserPromptSubmit mở sau khi dev bấm Tiếp tục) vừa ghi cache: xét lại
		if st, f, err = h.state(root); err != nil {
			return preDecision{}, false, err
		}
		dec = policy.PreToolUse(call, st)
	}
	if dec.Allow && f != nil && f.ControlBusy {
		return preDecision{"deny", controlBusyMsg()}, true, nil
	}
	if !dec.Allow {
		switch dec.Code {
		case "PAUSED":
			h.pausedFlush(root)
		case "OUT_OF_SCOPE":
			h.scopeDenied(root, in.cwd, file, st)
		}
		return preDecision{"deny", dec.Reason}, true, nil
	}
	return preDecision{}, false, nil
}

// webConfigDenied: luật ~/.config/aisdlc. Lỗi gì cũng coi như không chạm (luật khác vẫn xét).
func (h *hook) webConfigDenied(in *hookInput) (reason string, denied bool) {
	defer func() {
		if recover() != nil {
			reason, denied = "", false
		}
	}()
	dir := ""
	if home, ok := h.ctx.Home(); ok {
		dir = WebConfigDir(home)
	}
	ti, _ := in.json["tool_input"].(map[string]any)
	grep := text(in.json, "tool_name") == "Grep"
	var paths []string
	for _, f := range pathFields {
		if grep && f == "pattern" {
			continue // pattern của Grep là regex nội dung, không phải đường dẫn
		}
		if v, ok := ti[f].(string); ok {
			paths = append(paths, v)
		}
	}
	d := policy.WebConfigDecision(paths, text(ti, "command"), dir, in.cwd)
	if d.Allow {
		return "", false
	}
	return d.Reason, true
}

func (h *hook) stop() int {
	in := h.read()
	root, found := FindRepo(in.cwd)
	if !found || in.json == nil {
		return ExitOK
	}
	if !h.stopBlocked(in, root) {
		h.sessionEvent(root, "SESSION_STOP") // chặn dừng = agent còn chạy tiếp
	}
	return ExitOK
}

// stopBlocked in quyết định chặn dừng nếu có; lỗi thì im lặng (không chặn được việc dừng thì thôi).
func (h *hook) stopBlocked(in *hookInput, root string) (blocked bool) {
	defer func() { _ = recover() }()
	st, _, err := h.state(root)
	if err != nil {
		return false
	}
	if d := policy.Stop(truthy(in.json["stop_hook_active"]), st); !d.Allow {
		blocked = true
		h.printJSON(struct {
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
		}{"block", d.Reason})
	}
	return blocked
}

func (h *hook) sessionStart() int {
	in := h.read()
	// trước mọi kiểm tra repo: session mở ngoài repo AISDLC (hoặc cd vào sau) vẫn cần biến để `aisdlc use` gắn kết
	// đúng session mà hook đọc
	h.exportSession()
	root, found := FindRepo(in.cwd)
	if !found || in.json == nil {
		return ExitOK
	}
	h.sessionContext(in, root)
	h.sessionEvent(root, "SESSION_START")
	return ExitOK
}

// sessionContext in quy tắc và trạng thái (additionalContext); lỗi thì im lặng.
func (h *hook) sessionContext(in *hookInput, root string) {
	defer func() { _ = recover() }()
	st, _, err := h.state(root)
	if err != nil || !stateReadable(st) {
		return
	}
	text := policy.SessionContext(st)
	// chỉ dẫn chưa giao: nhận qua sổ (mỗi chỉ dẫn một lần); lỗi (vd. control.lock bị giữ) thì quy tắc vẫn nạp
	if !in.subagent {
		if instr := h.claimInstructions(root); instr != "" {
			text += "\n\n" + instr
		}
	}
	h.printJSON(struct {
		Out sessionStartOut `json:"hookSpecificOutput"`
	}{sessionStartOut{"SessionStart", text}})
}

type sessionStartOut struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// stateReadable: cổng và bước đọc được (bản Java ném lỗi khi dựng ngữ cảnh và hook im lặng).
func stateReadable(st policy.PolicyState) bool {
	if st.Active == "" {
		return true
	}
	if st.Gates == nil || st.Steps == nil {
		return false
	}
	for _, g := range gateSequence(st) {
		if _, err := st.Gates.State(g); err != nil {
			return false
		}
	}
	_, _, err := st.Steps.Current()
	return err == nil
}

// exportSession ghi `export AISDLC_SESSION=<id>` vào CLAUDE_ENV_FILE để mọi lệnh Bash của session mang biến này
// (không trùng dòng). Im lặng khi thiếu biến/id; id có ký tự ngoài [A-Za-z0-9_-] thì không ghi. File của Claude
// Code: nối thêm dòng, không ghi đè (không phải file trạng thái của AISDLC).
func (h *hook) exportSession() {
	defer func() { _ = recover() }()
	id := h.ctx.SessionID()
	file, ok := h.ctx.Env("CLAUDE_ENV_FILE")
	if id == "" || !ok || !envSessionID.MatchString(id) {
		return
	}
	line := "export AISDLC_SESSION=" + id
	if b, err := os.ReadFile(file); err == nil {
		for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
			if l == line {
				return
			}
		}
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line + "\n")
	_ = f.Close()
}

// resolve phân giải theo session của hook (không mạng, không hỏi), một lần mỗi lần gọi hook.
func (h *hook) resolve(root string) (Resolution, error) {
	if h.resolved == nil || h.resRoot != root {
		r, err := NewFeatureResolver(h.ctx).Resolve(root, "", h.ctx.SessionID())
		if err != nil {
			return nil, err
		}
		h.resolved, h.resRoot = r, root
	}
	return h.resolved, nil
}

// activeKey: tính năng đã phân giải mà thư mục không có trên nhánh hiện tại (đã đổi nhánh) coi như không có.
func activeKey(root string, r Resolution) model.FeatureKey {
	if x, ok := r.(Resolved); ok && exists(model.FeaturePaths{RepoRoot: root, Key: x.Key}.Meta()) {
		return x.Key
	}
	return ""
}

// state dựng PolicyState (và Feature nếu có tính năng đang làm).
func (h *hook) state(root string) (policy.PolicyState, *Feature, error) {
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		return policy.PolicyState{}, nil, err
	}
	r, err := h.resolve(root)
	if err != nil {
		return policy.PolicyState{}, nil, err
	}
	active := activeKey(root, r)
	if active == "" {
		var open []model.FeatureKey
		if a, ok := r.(Ambiguous); ok {
			open = a.Open
		}
		return policy.PolicyState{RepoRoot: root, Config: cfg, Ambiguous: open}, nil, nil
	}
	f, err := h.ctx.FeatureAt(root, active)
	if err != nil {
		return policy.PolicyState{}, nil, err
	}
	paths := f.Paths
	return policy.PolicyState{RepoRoot: root, Config: f.Config, Active: active, Gates: f.Gates, Steps: f.Steps(),
		Paths: &paths, Control: f.Control}, f, nil
}

type preToolUseOut struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// printDecision in deny hoặc ask (hộp xác nhận quyền Yes/No của Claude Code) dạng JSON một dòng.
func (h *hook) printDecision(d preDecision) {
	h.printJSON(struct {
		Out preToolUseOut `json:"hookSpecificOutput"`
	}{preToolUseOut{"PreToolUse", d.decision, d.reason}})
}

func (h *hook) printJSON(v any) {
	b, err := jsonMarshal(v)
	if err != nil {
		panic(err)
	}
	h.ctx.Println(string(b))
}

// text là giá trị chuỗi của field (rỗng nếu thiếu hoặc không phải chuỗi).
func text(m map[string]any, field string) string {
	if m == nil {
		return ""
	}
	s, _ := m[field].(string)
	return s
}

// truthy như asBoolean(false) của Jackson: true, hoặc chuỗi "true".
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.TrimSpace(x) == "true"
	}
	return false
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
