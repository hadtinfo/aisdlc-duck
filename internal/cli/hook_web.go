package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aisdlc/internal/model"
	"aisdlc/internal/policy"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/step"
	"aisdlc/internal/webclient"
)

// Phần web của hook (HookCommand của Java): event FILE_EDIT, SESSION_START, SESSION_STOP, SCOPE_DENIED vào outbox
// và mở `aisdlc web flush` nền. Hook không bao giờ gọi mạng và không bao giờ chờ web; web tắt thì không đọc gì thêm.
// Mọi lỗi ở đây im lặng: không đụng stdout (Claude Code đọc nó), không đổi mã thoát.

const (
	// flushEvery: hook mở `web flush` vì tạm dừng tối đa một lần mỗi chừng này (mtime của
	// .aisdlc/web/control/spawned).
	flushEvery = 5 * time.Second
	// flushWait: PreToolUse bị chặn vì tạm dừng mà có `web flush` đang chạy: chờ nó ghi cache điều khiển tối đa
	// chừng này.
	flushWait = 2 * time.Second
	// flushPoll: khoảng xem lại cache trong lúc chờ.
	flushPoll = 50 * time.Millisecond
)

// controlBusyMsg: PreToolUse của tool ghi khi không lấy được control.lock (lệnh chờ chưa xét được).
const controlBusyMsg = "aisdlc: không khóa được sổ điều khiển web (.aisdlc/web/control/control.lock) trong 1 giây" +
	" — một tiến trình aisdlc khác đang giữ (treo hoặc bị dừng?). Thử lại sau giây lát."

// emit ghi event vào outbox rồi mở tiến trình gửi nền (Publisher.Emit). Lỗi chỉ ra stderr.
func (h *hook) emit(root string, e webclient.Event) {
	defer func() { _ = recover() }()
	e.At, e.SessionID = h.ctx.now(), h.ctx.SessionID()
	if e.Sender == "" {
		e.Sender = webclient.Sender(h.ctx.Git(root))
	}
	h.setFlushFeature(root)
	h.ctx.Publisher(root).Emit(e)
}

// setFlushFeature: tiến trình `web flush` nền mở từ hook này làm mới cả tính năng của phiên (phân giải theo
// session_id của hook), không chỉ .aisdlc/active — nhiều phiên song song mỗi phiên một tính năng.
func (h *hook) setFlushFeature(root string) {
	if r, err := h.resolve(root); err == nil {
		h.ctx.flushFeature = activeKey(root, r)
	}
}

func (h *hook) webEnabled(root string) bool { return h.ctx.Publisher(root).Enabled() }

// postToolUse: giao chỉ dẫn từ web chưa giao cho agent chính (additionalContext); rồi, khi web bật, tool ghi vừa
// chạy → event FILE_EDIT (đường dẫn tương đối repo, bước đang chạy, ngoài phạm vi hay không); Bash chỉ khi có bước
// đang chạy (detail = 120 ký tự đầu của lệnh).
func (h *hook) postToolUse() int {
	defer func() { _ = recover() }() // im lặng: hook này không bao giờ làm hỏng phiên làm việc
	in := h.read()
	root, found := FindRepo(in.cwd)
	if !found || in.json == nil {
		return ExitOK
	}
	// tool của subagent: chỉ dẫn vào ngữ cảnh subagent là mất với agent chính → để lần sau của agent chính
	if !in.subagent {
		h.printContext("PostToolUse", h.claimInstructions(root))
	}
	if !h.webEnabled(root) { // web tắt: không đọc trạng thái gì
		return ExitOK
	}
	tool := text(in.json, "tool_name")
	if !guardedTools[tool] {
		return ExitOK
	}
	ti, _ := in.json["tool_input"].(map[string]any)
	bash := tool == "Bash"
	rel := ""
	if !bash {
		file := text(ti, "file_path")
		if file == "" {
			file = text(ti, "notebook_path")
		}
		if file == "" {
			return ExitOK
		}
		if rel = relativeTo(root, in.cwd, file); rel == "" {
			return ExitOK // ngoài repo
		}
	}
	st, _, err := h.state(root)
	if err != nil {
		return ExitOK
	}
	cur := runningStep(st)
	e := webclient.Event{Type: "FILE_EDIT", Feature: string(st.Active), Step: cur, Path: rel}
	if bash {
		if cur == "" {
			return ExitOK
		}
		e.Detail = prefixUTF16(text(ti, "command"), 120)
	} else if cur != "" {
		// cùng quy tắc với step finish: file trạng thái tính năng và unmanaged không tính là ngoài phạm vi
		globs, err := st.Steps.AllowedGlobs(cur)
		if err != nil {
			return ExitOK
		}
		e.OutOfScope = !step.IgnoredPath(*st.Paths, st.Config, rel) && !step.ScopeMatches(globs, rel)
	}
	h.emit(root, e)
	return ExitOK
}

// runningStep là bước đang IN_PROGRESS của tính năng đang làm ("" nếu không có).
func runningStep(st policy.PolicyState) string {
	if st.Active == "" || st.Steps == nil {
		return ""
	}
	r, ok, err := st.Steps.Current()
	if err != nil || !ok || r.Status != step.StepInProgress {
		return ""
	}
	return r.ID
}

// sessionEvent: event phiên (SESSION_START/SESSION_STOP) gắn tính năng đang làm (nếu có), không bước.
func (h *hook) sessionEvent(root, typ string) {
	defer func() { _ = recover() }()
	if !h.webEnabled(root) {
		return
	}
	feature := ""
	if r, err := h.resolve(root); err == nil {
		feature = string(activeKey(root, r))
	}
	h.emit(root, webclient.Event{Type: typ, Feature: feature})
}

// scopeDenied: PreToolUse bị chặn vì ngoài phạm vi → event SCOPE_DENIED (đường dẫn tương đối, bước đang chạy).
func (h *hook) scopeDenied(root, cwd, file string, st policy.PolicyState) {
	defer func() { _ = recover() }()
	if file == "" || st.Steps == nil || !h.webEnabled(root) {
		return
	}
	rel := relativeTo(root, cwd, file)
	if rel == "" {
		return
	}
	h.emit(root, webclient.Event{Type: "SCOPE_DENIED", Feature: string(st.Active), Step: runningStep(st), Path: rel,
		OutOfScope: true})
}

// pausedFlush: bị chặn vì tạm dừng → tool bị chặn không sinh PostToolUse nên tự mở `web flush` (làm mới điều
// khiển), tối đa một lần mỗi 5 giây.
func (h *hook) pausedFlush(root string) {
	defer func() { _ = recover() }()
	if !h.webEnabled(root) {
		return
	}
	mark := spawnedMark(root)
	if h.recentlySpawned(mark) {
		return
	}
	h.spawn(root, mark)
}

// userPromptSubmit (dev vừa gõ prompt, thường ngay sau khi bấm Tiếp tục / gửi chỉ dẫn trên web): chỉ khi repo có
// .aisdlc/web/control/. Mở `web flush` nền (làm mới cờ tạm dừng và chỉ dẫn; PreToolUse kế tiếp chờ nó tối đa 2 giây)
// trừ khi lần mở trước (< 5 giây) chưa ghi xong cache điều khiển, rồi giao chỉ dẫn chưa giao qua additionalContext.
// Không gọi mạng, luôn exit 0.
func (h *hook) userPromptSubmit() int {
	defer func() { _ = recover() }() // im lặng: hook này không bao giờ chặn prompt của dev
	in := h.read()
	root, found := FindRepo(in.cwd)
	if !found || in.json == nil {
		return ExitOK
	}
	if st, err := os.Stat(controlDir(root)); err != nil || !st.IsDir() {
		return ExitOK // repo chưa dùng điều khiển web
	}
	if r, err := h.resolve(root); err == nil {
		if key := activeKey(root, r); key != "" {
			h.promptFlush(root, key)
		}
	}
	if !in.subagent {
		h.printContext("UserPromptSubmit", h.claimInstructions(root))
	}
	return ExitOK
}

// promptFlush mở `web flush` trừ khi lần mở trước (< 5 giây) chưa ghi xong cache điều khiển: mỗi lúc tối đa một
// tiến trình đang chạy. Không dùng luật 5 giây cứng của pausedFlush: dev bấm Tiếp tục rồi gõ prompt ngay sau một lần
// bị chặn thì vẫn phải tải lại.
func (h *hook) promptFlush(root string, key model.FeatureKey) {
	defer func() { _ = recover() }()
	if !h.webEnabled(root) {
		return
	}
	mark := spawnedMark(root)
	if h.recentlySpawned(mark) && !writtenSince(snapshot.ControlCacheFile(root, key), mtime(mark)) {
		return
	}
	h.spawn(root, mark)
}

// claimInstructions: chỉ dẫn từ web chưa giao của tính năng đang làm. Nhận qua sổ cục bộ (dưới khóa, mỗi chỉ dẫn một
// lần trên máy này), mỗi chỉ dẫn một event INSTRUCTION_DELIVERED, cập nhật cờ inbox; trả khối additionalContext
// ("" nếu không có hoặc lỗi, vd. control.lock bị giữ).
func (h *hook) claimInstructions(root string) (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	// đường nhanh: chưa có thư mục điều khiển thì không có cache, không có cờ inbox để dọn (khỏi resolve tính năng)
	if !exists(controlDir(root)) {
		return ""
	}
	key := model.FeatureKey("")
	if r, err := h.resolve(root); err == nil {
		key = activeKey(root, r)
	}
	if key == "" || !exists(snapshot.ControlCacheFile(root, key)) {
		_ = os.Remove(ControlInbox(root)) // cờ cũ
		return ""
	}
	control := NewWebControl(NewWebCache(h.ctx, root))
	got, err := NewControlLedger(root, key, h.ctx.lockBudget()).Claim(control.Undelivered(key))
	if err != nil {
		return ""
	}
	control.UpdateInbox(key)
	if len(got) == 0 {
		return ""
	}
	for _, i := range got {
		h.emit(root, webclient.Event{Type: "INSTRUCTION_DELIVERED", Feature: key.String(),
			Detail: strconv.FormatInt(i.ID, 10)})
	}
	return policy.InstructionContext(got, time.Local)
}

// printContext in khối additionalContext của sự kiện hook (không in gì khi rỗng).
func (h *hook) printContext(event, text string) {
	if text == "" {
		return
	}
	h.printJSON(struct {
		Out sessionStartOut `json:"hookSpecificOutput"`
	}{sessionStartOut{event, text}})
}

// awaitFlush: có `web flush` mở < 5 giây trước mà chưa ghi lại cache điều khiển thì chờ tối đa 2 giây tới khi cache
// được ghi. True nếu cache vừa được ghi trong lúc chờ (người gọi xét lại); hết hạn hay không có tiến trình đang
// chạy → false (chặn như thường).
func (h *hook) awaitFlush(root string, key model.FeatureKey) (written bool) {
	defer func() {
		if recover() != nil {
			written = false
		}
	}()
	mark := spawnedMark(root)
	if !h.recentlySpawned(mark) {
		return false
	}
	cache := snapshot.ControlCacheFile(root, key)
	if writtenSince(cache, mtime(mark)) {
		return false // tiến trình đó đã xong: trạng thái vừa đọc là mới
	}
	before := mtime(cache)
	for end := time.Now().Add(flushWait); time.Now().Before(end); {
		time.Sleep(flushPoll)
		if st, err := os.Stat(cache); err == nil && st.ModTime().After(before) {
			return true
		}
	}
	return false
}

func spawnedMark(root string) string { return filepath.Join(controlDir(root), "spawned") }

func (h *hook) recentlySpawned(mark string) bool {
	st, err := os.Stat(mark)
	return err == nil && st.ModTime().After(h.ctx.now().Add(-flushEvery))
}

func mtime(p string) time.Time {
	st, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

// writtenSince: file có và mtime không trước t.
func writtenSince(p string, t time.Time) bool {
	st, err := os.Stat(p)
	return err == nil && !st.ModTime().Before(t)
}

// spawn đặt mốc spawned (mtime = bây giờ theo đồng hồ của Context) rồi mở `web flush` nền.
func (h *hook) spawn(root, mark string) {
	if err := os.MkdirAll(filepath.Dir(mark), 0o755); err != nil {
		return
	}
	if _, err := os.Stat(mark); err != nil {
		if f, err := os.OpenFile(mark, os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_ = f.Close()
		}
	}
	now := h.ctx.now()
	_ = os.Chtimes(mark, now, now)
	h.setFlushFeature(root)
	_ = h.ctx.SpawnFlush(root)
}

// relativeTo là đường dẫn tương đối repo (dấu /) của file (tương đối thì theo cwd), "" nếu nằm ngoài repo hoặc là
// chính gốc repo. So sánh theo chữ (không theo symlink), như HookCommand.relative của Java.
func relativeTo(root, cwd, file string) string {
	if strings.ContainsRune(file, 0) {
		return ""
	}
	p := file
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	p = filepath.Clean(p)
	r := filepath.Clean(root)
	rel, err := filepath.Rel(r, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// prefixUTF16 là n đơn vị UTF-16 đầu của s, như String.substring(0, n) của Java (không cắt giữa một ký tự).
func prefixUTF16(s string, n int) string {
	units := 0
	for i, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if units+w > n {
			return s[:i]
		}
		units += w
	}
	return s
}
