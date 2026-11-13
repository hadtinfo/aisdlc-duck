package cli

import (
	"slices"
	"strconv"
	"strings"

	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
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
	// valueOptions: tùy chọn nhận giá trị ở token kế tiếp. --message là tên dài của -m trong bản Go.
	valueOptions = map[string]bool{"--feature": true, "-f": true, "-m": true, "--message": true, "--reason": true}
)

// indirectDevMsg: lệnh chỉ dev gọi gián tiếp (qua biến, bash -c, eval…).
func indirectDevMsg() string {
	return i18n.T("Lệnh chỉ dev (approve/reject/reopen/rewind/scope allow/step accept/risk set) phải được gọi thẳng: " +
		"gọi thẳng `aisdlc <lệnh>` để hiện hộp xác nhận (một lệnh đơn, không qua biến, `bash -c`, " +
		"`eval`, đường dẫn khác hay ghép với lệnh khác).")
}

// devOnly: chỉ nhận lệnh đơn `aisdlc <lệnh>` (dạng gián tiếp → deny); chế độ quyền không hỏi người → deny; còn lại
// ghi phiếu một lần (hạn policy.GrantTTL) rồi trả ask để Claude Code hiện hộp Yes/No cho dev.
func (h *hook) devOnly(root string, in *hookInput, command string) (preDecision, bool, error) {
	c, ok := policy.ParseDevCommand(command)
	if !ok {
		return preDecision{"deny", indirectDevMsg()}, true, nil
	}
	mode, hasMode := in.json["permission_mode"].(string)
	if !hasMode || !askModes[mode] {
		shown := mode
		if !hasMode {
			shown = i18n.T("không rõ")
		}
		return preDecision{"deny", i18n.Tf("Claude Code đang ở chế độ quyền không hỏi người (permission_mode: %s), nên không hiện được hộp xác nhận: dev duyệt ở terminal riêng hoặc web.", shown)}, true, nil
	}
	session := h.ctx.SessionID()
	if session == "" {
		return preDecision{"deny", i18n.T("Không có session_id trong input hook: dev duyệt ở terminal riêng hoặc web.")},
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
		return preDecision{"deny", i18n.T("Không xác định được tính năng của lệnh này trong session; thêm `--feature <KEY>` vào lệnh.")}, true, nil
	}
	f, err := h.ctx.FeatureAt(root, key)
	if err != nil {
		return preDecision{}, false, err
	}
	var g gate.Gate
	hash := ""
	if gateVerbs[c.Verb] {
		var ok bool
		if g, ok = gateArg(c.Args); !ok {
			return preDecision{"deny", i18n.Tf("Không đọc được tên cổng trong lệnh `aisdlc %s`.", c.Key())}, true, nil
		}
		if !slices.Contains(gate.Sequence(f.Flow()), g) {
			return preDecision{"deny", i18n.Tf("Cổng %s không thuộc luồng %s.", string(g), string(f.Flow()))}, true, nil
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
		return i18n.Tf("%sDuyệt cổng %s%s%s. Bấm Yes nếu chính bạn muốn duyệt.", k, string(g), at, h.flagCounts(f, g))
	case "reject":
		msg := ""
		if m, ok := optionValue(c.Args, "-m", "--message"); ok {
			msg = i18n.Tf(" — góp ý: %s", m)
		}
		return i18n.Tf("%sTrả lại cổng %s%s%s. Bấm Yes nếu chính bạn muốn trả lại.", k, string(g), at, msg)
	case "reopen":
		return i18n.Tf("%sMở lại cổng %s%s. Bấm Yes nếu chính bạn muốn mở lại.", k, string(g), at)
	case "step accept":
		return i18n.Tf("%sChấp nhận bước %s. Bấm Yes nếu chính bạn muốn chấp nhận.", k, rest)
	case "scope allow":
		return i18n.Tf("%sCho phép sửa ngoài phạm vi: %s. Bấm Yes nếu chính bạn muốn cho phép.", k, rest)
	case "risk set":
		why := ""
		if m, ok := optionValue(c.Args, "--reason"); ok {
			why = i18n.Tf(" (lý do: %s)", m)
		}
		return i18n.Tf("%sĐặt mức rủi ro %s%s. Bấm Yes nếu chính bạn muốn đổi.", k, rest, why)
	}
	return i18n.Tf("%saisdlc %s. Bấm Yes nếu chính bạn muốn chạy lệnh này.", k, c.Key())
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
