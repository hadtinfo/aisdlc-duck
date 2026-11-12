package control_test

import (
	"regexp"
	"testing"
)

var viChars = regexp.MustCompile(`(?i)[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)

// Chữ đã lưu (lý do của lệnh, chỉ dẫn trả lại cổng) lưu tiếng Việt, hiện theo locale người xem; ghi chú giữ nguyên văn.
func TestControlViewStoredTextLocalized(t *testing.T) {
	f := newFx(t)
	ft := f.feat(t, "r/ct-locale")
	f.act(f.ownerS, ft, "ACCEPT_STEP", "S1", nil)
	id := f.control(t, ft).Actions[0].ID
	eq(t, f.events(ft.repo, "2", "", "ACTION_RESULT", js(map[string]any{"id": id, "status": "STALE", "reason": "Bước đã đổi kể từ lúc bạn xem; tải lại trang"})), 200)
	if _, err := f.e.Pool.Exec(t.Context(), "INSERT INTO instructions(repo_id, feature_key, by_login, text, source) VALUES ($1,$2,$3,$4,'feedback')",
		ft.repo, ft.key, f.owner, "Cổng BRD bị trả lại bởi "+f.owner+": ghi chú"); err != nil {
		t.Fatal(err)
	}
	view := func(lang string) map[string]any {
		h := map[string]string{"Cookie": "AISDLC_SESSION=" + f.ownerS.ID}
		if lang != "" {
			h["Accept-Language"] = lang
		}
		r := f.w.Do("GET", "/api/features/"+ft.repo+"/"+ft.key+"/control", "", h)
		if r.Status != 200 {
			t.Fatal(r.Status, r.Body)
		}
		return r.JSON(t)
	}
	en := view("en")
	reason := list(en["actions"])[0].(map[string]any)["reason"].(string)
	text := list(en["instructions"])[0].(map[string]any)["text"].(string)
	if viChars.MatchString(reason) || text != "Gate BRD was rejected by "+f.owner+": ghi chú" {
		t.Fatal(reason, "|", text)
	}
	vi := view("")
	eq(t, list(vi["actions"])[0].(map[string]any)["reason"], any("Bước đã đổi kể từ lúc bạn xem; tải lại trang"))
	eq(t, list(vi["instructions"])[0].(map[string]any)["text"], any("Cổng BRD bị trả lại bởi "+f.owner+": ghi chú"))
}
