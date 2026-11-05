package cli

import (
	"slices"
	"strconv"
	"strings"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/policy"
	"aisdlc/internal/review"
)

// Lệnh chỉ dev khi sessionApprove bật ở baseRef (spec §3.2): hook hỏi dev Yes/No trong Claude Code và ghi phiếu
// cho phép một lần; CLI không console tiêu thụ phiếu (Context.ConfirmDev).

var (
	// gateVerbs: động từ gắn với một cổng; phiếu mang hash artifact hiện tại của cổng.
	gateVerbs = map[string]bool{"approve": true, "reject": true, "reopen": true}
	// askModes: chế độ quyền mà Claude Code còn hỏi người.
	askModes = map[string]bool{"default": true, "acceptEdits": true, "plan": true}
	// webAbortVerbs: lệnh chỉ dev mà CLI dừng bằng WebPhase4Msg ở chế độ duyệt trên web (Context.WebNetwork).
	webAbortVerbs = map[string]bool{"approve": true, "reject": true, "reopen": true, "risk set": true}
	// valueOptions: tùy chọn nhận giá trị ở token kế tiếp. --message là tên dài của -m trong bản Go.
	valueOptions = map[string]bool{"--feature": true, "-f": true, "-m": true, "--message": true, "--reason": true}
)

const indirectDevMsg = "Lệnh chỉ dev (approve/reject/reopen/scope allow/step accept/risk set) phải được gọi thẳng: " +
	"gọi thẳng `aisdlc <lệnh>` để hiện hộp xác nhận (một lệnh đơn, không qua biến, `bash -c`, " +
	"`eval`, đường dẫn khác hay ghép với lệnh khác)."

// devOnly: chỉ nhận lệnh đơn `aisdlc <lệnh>` (dạng gián tiếp → deny); chế độ quyền không hỏi người → deny; còn lại
// ghi phiếu một lần (hạn policy.GrantTTL) rồi trả ask để Claude Code hiện hộp Yes/No cho dev.
func (h *hook) devOnly(root string, in *hookInput, command string) (preDecision, bool, error) {
	c, ok := policy.ParseDevCommand(command)
	if !ok {
		return preDecision{"deny", indirectDevMsg}, true, nil
	}
	mode, hasMode := in.json["permission_mode"].(string)
	if !hasMode || !askModes[mode] {
		shown := mode
		if !hasMode {
			shown = "không rõ"
		}
		return preDecision{"deny", "Claude Code đang ở chế độ quyền không hỏi người (permission_mode: " + shown +
			"), nên không hiện được hộp xác nhận: dev duyệt ở terminal riêng hoặc web."}, true, nil
	}
	session := h.ctx.SessionID()
	if session == "" {
		return preDecision{"deny", "Không có session_id trong input hook: dev duyệt ở terminal riêng hoặc web."},
			true, nil
	}
	key, explicit, err := explicitFeature(c.Args)
	if err != nil {
		return preDecision{}, false, err
	}
	if !explicit {
		r, err := h.resolve(root)
		if err != nil {
			return preDecision{}, false, err
		}
		if x, ok := r.(Resolved); ok {
			key = x.Key
		}
	}
	if key == "" {
		return preDecision{"deny", "Không xác định được tính năng của lệnh này trong session; thêm " +
			"`--feature <KEY>` vào lệnh."}, true, nil
	}
	f, err := h.ctx.FeatureAt(root, key)
	if err != nil {
		return preDecision{}, false, err
	}
	// chế độ web: CLI sẽ dừng lệnh này (giai đoạn 4), nên không hỏi dev và không ghi phiếu
	if f.Mode == gate.ModeWeb && webAbortVerbs[c.Verb] {
		return preDecision{"deny", WebPhase4Msg}, true, nil
	}
	var g gate.Gate
	hash := ""
	if gateVerbs[c.Verb] {
		var ok bool
		if g, ok = gateArg(c.Args); !ok {
			return preDecision{"deny", "Không đọc được tên cổng trong lệnh `aisdlc " + c.Key() + "`."}, true, nil
		}
		if !slices.Contains(gate.Sequence(f.Flow()), g) {
			return preDecision{"deny", "Cổng " + string(g) + " không thuộc luồng " + string(f.Flow()) + "."}, true, nil
		}
		if hash, err = f.Gates.CurrentHash(g); err != nil {
			return preDecision{}, false, err
		}
	}
	// lý do dựng trước khi ghi phiếu: lỗi ở đây thì không còn phiếu nào nằm lại
	reason := h.askReason(f, c, g, hash)
	if err := policy.IssueGrant(root, policy.Grant{SessionID: session, Feature: key.String(), Command: c.GrantKey(),
		Hash: hash, ExpiresAt: h.ctx.Now().Add(policy.GrantTTL)}); err != nil {
		return preDecision{}, false, err
	}
	return preDecision{"ask", reason}, true, nil
}

// explicitFeature: --feature KEY, --feature=KEY, -f KEY, -fKEY trong đối số (key sai → lỗi, như FeatureKey Java).
func explicitFeature(args []string) (model.FeatureKey, bool, error) {
	for i, a := range args {
		v, found := "", false
		switch {
		case (a == "--feature" || a == "-f") && i+1 < len(args):
			v, found = args[i+1], true
		case strings.HasPrefix(a, "--feature="):
			v, found = strings.TrimPrefix(a, "--feature="), true
		case strings.HasPrefix(a, "-f") && len(a) > 2 && !strings.HasPrefix(a, "--"):
			v, found = a[2:], true
		}
		if found {
			k, err := model.ParseFeatureKey(strings.TrimFunc(v, func(r rune) bool { return r <= ' ' }))
			if err != nil {
				return "", false, err
			}
			return k, true, nil
		}
	}
	return "", false, nil
}

// gateArg: đối số vị trí đầu tiên (bỏ tùy chọn và giá trị của chúng) là tên cổng.
func gateArg(args []string) (gate.Gate, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if valueOptions[a] {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		g, err := GateOf(a)
		return g, err == nil
	}
	return "", false
}

// askReason là lý do hiện trong hộp xác nhận: tính năng, việc sắp làm, hash cổng và số cờ của review card.
func (h *hook) askReason(f *Feature, c policy.DevCommand, g gate.Gate, hash string) string {
	k := "[" + f.Key.String() + "] "
	at := ""
	if hash != "" {
		at = " @ " + hash[:min(8, len(hash))]
	}
	rest := positional(c.Args)
	switch c.Verb {
	case "approve":
		return k + "Duyệt cổng " + string(g) + at + h.flagCounts(f, g) + ". Bấm Yes nếu chính bạn muốn duyệt."
	case "reject":
		msg := ""
		if m, ok := optionValue(c.Args, "-m", "--message"); ok {
			msg = " — góp ý: " + m
		}
		return k + "Trả lại cổng " + string(g) + at + msg + ". Bấm Yes nếu chính bạn muốn trả lại."
	case "reopen":
		return k + "Mở lại cổng " + string(g) + at + ". Bấm Yes nếu chính bạn muốn mở lại."
	case "step accept":
		return k + "Chấp nhận bước " + rest + ". Bấm Yes nếu chính bạn muốn chấp nhận."
	case "scope allow":
		return k + "Cho phép sửa ngoài phạm vi: " + rest + ". Bấm Yes nếu chính bạn muốn cho phép."
	case "risk set":
		why := ""
		if m, ok := optionValue(c.Args, "--reason"); ok {
			why = " (lý do: " + m + ")"
		}
		return k + "Đặt mức rủi ro " + rest + why + ". Bấm Yes nếu chính bạn muốn đổi."
	}
	return k + "aisdlc " + c.Key() + ". Bấm Yes nếu chính bạn muốn chạy lệnh này."
}

// flagCounts: " — HIGH 1, WARN 2" theo review card; dựng thẻ lỗi (kể cả panic) thì rỗng, hộp xác nhận vẫn hiện.
func (h *hook) flagCounts(f *Feature, g gate.Gate) (s string) {
	defer func() {
		if recover() != nil {
			s = ""
		}
	}()
	card, err := CardFor(h.ctx, f, g)
	if err != nil {
		return ""
	}
	high, warn := 0, 0
	for _, fl := range card.Flags {
		switch fl.Level {
		case review.HIGH:
			high++
		case review.WARN:
			warn++
		}
	}
	return " — HIGH " + strconv.Itoa(high) + ", WARN " + strconv.Itoa(warn)
}

func positional(args []string) string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if valueOptions[a] {
			i++
		} else if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return strings.Join(out, " ")
}

// optionValue: giá trị sau tên tùy chọn đầu tiên khớp một trong names.
func optionValue(args []string, names ...string) (string, bool) {
	for i := 0; i+1 < len(args); i++ {
		if slices.Contains(names, args[i]) {
			return args[i+1], true
		}
	}
	return "", false
}

func gateSequence(st policy.PolicyState) []gate.Gate { return gate.Sequence(st.Gates.Flow()) }
